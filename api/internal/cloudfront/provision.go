package cloudfront

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Tags marking resources managed by GlacierVault, so Ensure stays idempotent
// across restarts and re-deploys.
const (
	tagManaged = "GlacierVaultManaged"
	tagStack   = "GlacierVaultStack"
)

// Cache TTLs: pack objects are content-addressed and immutable, so they can
// be cached aggressively. A repeat restore of the same packs is then served
// from the edge instead of re-fetching from S3.
const (
	cacheDefaultTTLSeconds = 30 * 24 * 3600 // 30 days
	cacheMaxTTLSeconds     = 365 * 24 * 3600
)

// EnsureConfig carries everything Ensure needs to provision the distribution.
type EnsureConfig struct {
	Region     string // AWS region of the cold bucket
	ColdBucket string
	StackName  string // GlacierVault stack name, used for resource naming/tags
	AccessKey  string // admin credentials (only used during setup)
	SecretKey  string
	Log        func(string) // progress logging; may be nil
}

// Info describes the provisioned distribution.
type Info struct {
	DistributionID string
	ARN            string
	Domain         string // e.g. d111111abcdef8.cloudfront.net
	KeyPairID      string // CloudFront key-pair ID for signed URLs
	KeyGroupID     string
	OACID          string
	CachePolicyID  string
}

// Ensure provisions (or reuses) the CloudFront distribution fronting the cold
// bucket, plus its OAC, key group, and cache policy, and grants the
// distribution read access to the bucket. It returns the distribution info
// and the freshly generated private key PEM (the caller persists it,
// encrypted — the private key never leaves the container otherwise).
//
// The distribution requires signed URLs for all viewer requests, so the
// bucket stays private: the bucket policy only allows the CloudFront service
// principal, and CloudFront only serves requests carrying a signature made
// with the private key returned here.
//
// Ensure is idempotent: if a GlacierVault-managed distribution already exists
// for the stack, it is reused and no new key pair is generated.
func Ensure(ctx context.Context, cfg EnsureConfig) (*Info, string, error) {
	logf := cfg.Log
	if logf == nil {
		logf = func(string) {}
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, "", fmt.Errorf("load AWS config: %w", err)
	}
	cf := cloudfront.NewFromConfig(awsCfg)
	s3c := s3.NewFromConfig(awsCfg)

	// Reuse an existing managed distribution if there is one.
	if existing, err := findManagedDistribution(ctx, cf, cfg.StackName); err != nil {
		return nil, "", fmt.Errorf("lookup existing distribution: %w", err)
	} else if existing != nil {
		logf("Reusing existing CloudFront distribution " + existing.Domain)
		// NOTE: reuse keeps the original key pair; the caller must already
		// have its private key stored.
		return existing, "", nil
	}

	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, "", err
	}

	base := "glaciervault-" + sanitize(cfg.StackName)

	logf("Creating CloudFront origin access control...")
	oacID, err := createOAC(ctx, cf, base+"-oac")
	if err != nil {
		return nil, "", err
	}

	logf("Creating CloudFront public key...")
	pubKeyID, err := createPublicKey(ctx, cf, base+"-key", kp.PublicKeyPEM)
	if err != nil {
		return nil, "", err
	}

	logf("Creating CloudFront key group...")
	keyGroupID, err := createKeyGroup(ctx, cf, base+"-keygroup", pubKeyID)
	if err != nil {
		return nil, "", err
	}

	logf("Creating CloudFront cache policy...")
	cachePolicyID, err := createCachePolicy(ctx, cf, base+"-cache")
	if err != nil {
		return nil, "", err
	}

	logf("Creating CloudFront distribution (this takes a few minutes)...")
	info, err := createDistribution(ctx, cf, cfg, base, oacID, keyGroupID, cachePolicyID)
	if err != nil {
		return nil, "", err
	}
	info.KeyPairID = pubKeyID
	info.KeyGroupID = keyGroupID
	info.OACID = oacID
	info.CachePolicyID = cachePolicyID

	logf("Granting the distribution read access to the cold bucket...")
	if err := allowDistributionOnBucket(ctx, s3c, cfg.ColdBucket, info.ARN); err != nil {
		return nil, "", err
	}

	logf("Waiting for the distribution to deploy...")
	if err := waitDeployed(ctx, cf, info.DistributionID, logf); err != nil {
		// Non-fatal: the proxy starts working as soon as CloudFront finishes
		// deploying on its own.
		logf("[warn] distribution not deployed yet: " + err.Error())
	}

	return info, kp.PrivateKeyPEM, nil
}

// findManagedDistribution returns the GlacierVault-managed distribution for
// the stack, if one exists.
func findManagedDistribution(ctx context.Context, cf *cloudfront.Client, stackName string) (*Info, error) {
	var marker *string
	for {
		out, err := cf.ListDistributions(ctx, &cloudfront.ListDistributionsInput{Marker: marker})
		if err != nil {
			return nil, err
		}
		if out.DistributionList == nil {
			return nil, nil
		}
		for _, d := range out.DistributionList.Items {
			tags, err := cf.ListTagsForResource(ctx, &cloudfront.ListTagsForResourceInput{
				Resource: aws.String(aws.ToString(d.ARN)),
			})
			if err != nil {
				continue
			}
			m := map[string]string{}
			for _, t := range tags.Tags.Items {
				m[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			if m[tagManaged] == "true" && m[tagStack] == stackName {
				return &Info{
					DistributionID: aws.ToString(d.Id),
					ARN:            aws.ToString(d.ARN),
					Domain:         aws.ToString(d.DomainName),
				}, nil
			}
		}
		if !aws.ToBool(out.DistributionList.IsTruncated) {
			return nil, nil
		}
		marker = out.DistributionList.NextMarker
	}
}

func callerRef(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().Unix(), hex.EncodeToString(b))
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func createOAC(ctx context.Context, cf *cloudfront.Client, name string) (string, error) {
	out, err := cf.CreateOriginAccessControl(ctx, &cloudfront.CreateOriginAccessControlInput{
		OriginAccessControlConfig: &cftypes.OriginAccessControlConfig{
			Name:                          aws.String(name),
			Description:                   aws.String("GlacierVault cold bucket access"),
			SigningBehavior:               cftypes.OriginAccessControlSigningBehaviorsAlways,
			SigningProtocol:               cftypes.OriginAccessControlSigningProtocolsSigv4,
			OriginAccessControlOriginType: cftypes.OriginAccessControlOriginTypesS3,
		},
	})
	if err != nil {
		return "", fmt.Errorf("create OAC: %w", err)
	}
	return aws.ToString(out.OriginAccessControl.Id), nil
}

func createPublicKey(ctx context.Context, cf *cloudfront.Client, name, publicKeyPEM string) (string, error) {
	out, err := cf.CreatePublicKey(ctx, &cloudfront.CreatePublicKeyInput{
		PublicKeyConfig: &cftypes.PublicKeyConfig{
			CallerReference: aws.String(callerRef(name)),
			Name:            aws.String(name),
			EncodedKey:      aws.String(publicKeyPEM),
			Comment:         aws.String("GlacierVault signed-URL key"),
		},
	})
	if err != nil {
		return "", fmt.Errorf("create public key: %w", err)
	}
	return aws.ToString(out.PublicKey.Id), nil
}

func createKeyGroup(ctx context.Context, cf *cloudfront.Client, name, publicKeyID string) (string, error) {
	out, err := cf.CreateKeyGroup(ctx, &cloudfront.CreateKeyGroupInput{
		KeyGroupConfig: &cftypes.KeyGroupConfig{
			Name:    aws.String(name),
			Items:   []string{publicKeyID},
			Comment: aws.String("GlacierVault signed-URL key group"),
		},
	})
	if err != nil {
		return "", fmt.Errorf("create key group: %w", err)
	}
	return aws.ToString(out.KeyGroup.Id), nil
}

func createCachePolicy(ctx context.Context, cf *cloudfront.Client, name string) (string, error) {
	// Query strings are excluded from the cache key: signed URLs carry the
	// signature in the query string, and excluding it lets every signed URL
	// for the same object share one cache entry. CloudFront still validates
	// the signature on every request.
	out, err := cf.CreateCachePolicy(ctx, &cloudfront.CreateCachePolicyInput{
		CachePolicyConfig: &cftypes.CachePolicyConfig{
			Name:       aws.String(name),
			Comment:    aws.String("GlacierVault immutable pack cache"),
			MinTTL:     aws.Int64(0),
			DefaultTTL: aws.Int64(cacheDefaultTTLSeconds),
			MaxTTL:     aws.Int64(cacheMaxTTLSeconds),
			ParametersInCacheKeyAndForwardedToOrigin: &cftypes.ParametersInCacheKeyAndForwardedToOrigin{
				EnableAcceptEncodingGzip: aws.Bool(false),
				HeadersConfig: &cftypes.CachePolicyHeadersConfig{
					HeaderBehavior: cftypes.CachePolicyHeaderBehaviorNone,
				},
				CookiesConfig: &cftypes.CachePolicyCookiesConfig{
					CookieBehavior: cftypes.CachePolicyCookieBehaviorNone,
				},
				QueryStringsConfig: &cftypes.CachePolicyQueryStringsConfig{
					QueryStringBehavior: cftypes.CachePolicyQueryStringBehaviorNone,
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create cache policy: %w", err)
	}
	return aws.ToString(out.CachePolicy.Id), nil
}

func createDistribution(ctx context.Context, cf *cloudfront.Client, cfg EnsureConfig, base, oacID, keyGroupID, cachePolicyID string) (*Info, error) {
	originID := "cold-bucket"
	originDomain := fmt.Sprintf("%s.s3.%s.amazonaws.com", cfg.ColdBucket, cfg.Region)

	out, err := cf.CreateDistribution(ctx, &cloudfront.CreateDistributionInput{
		DistributionConfig: &cftypes.DistributionConfig{
			CallerReference: aws.String(callerRef(base)),
			Comment:         aws.String("GlacierVault free-egress restore path for " + cfg.StackName),
			Enabled:         aws.Bool(true),
			PriceClass:      cftypes.PriceClassPriceClass100, // US/EU edges: cheapest overage
			Origins: &cftypes.Origins{
				Quantity: aws.Int32(1),
				Items: []cftypes.Origin{{
					Id:                    aws.String(originID),
					DomainName:            aws.String(originDomain),
					OriginAccessControlId: aws.String(oacID),
					// Empty OAI element is required when using OAC.
					S3OriginConfig: &cftypes.S3OriginConfig{OriginAccessIdentity: aws.String("")},
				}},
			},
			DefaultCacheBehavior: &cftypes.DefaultCacheBehavior{
				TargetOriginId:       aws.String(originID),
				ViewerProtocolPolicy: cftypes.ViewerProtocolPolicyRedirectToHttps,
				Compress:             aws.Bool(false), // packs are encrypted; compression wastes CPU
				AllowedMethods: &cftypes.AllowedMethods{
					Quantity: aws.Int32(2),
					Items:    []cftypes.Method{cftypes.MethodGet, cftypes.MethodHead},
					CachedMethods: &cftypes.CachedMethods{
						Quantity: aws.Int32(2),
						Items:    []cftypes.Method{cftypes.MethodGet, cftypes.MethodHead},
					},
				},
				// Only signed URLs are served; this is what keeps the
				// distribution private.
				TrustedKeyGroups: &cftypes.TrustedKeyGroups{
					Enabled:  aws.Bool(true),
					Quantity: aws.Int32(1),
					Items:    []string{keyGroupID},
				},
				CachePolicyId: aws.String(cachePolicyID),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create distribution: %w", err)
	}
	d := out.Distribution
	info := &Info{
		DistributionID: aws.ToString(d.Id),
		ARN:            aws.ToString(d.ARN),
		Domain:         aws.ToString(d.DomainName),
	}

	// Tag for idempotent reuse.
	_, err = cf.TagResource(ctx, &cloudfront.TagResourceInput{
		Resource: aws.String(info.ARN),
		Tags: &cftypes.Tags{Items: []cftypes.Tag{
			{Key: aws.String(tagManaged), Value: aws.String("true")},
			{Key: aws.String(tagStack), Value: aws.String(cfg.StackName)},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("tag distribution: %w", err)
	}
	return info, nil
}

// allowDistributionOnBucket merges a statement into the cold bucket's policy
// granting the distribution's CloudFront service principal s3:GetObject.
// Existing statements are preserved.
func allowDistributionOnBucket(ctx context.Context, s3c *s3.Client, bucket, distARN string) error {
	policy := map[string]interface{}{
		"Version":   "2012-10-17",
		"Statement": []interface{}{},
	}
	if out, err := s3c.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)}); err == nil && out.Policy != nil {
		_ = json.Unmarshal([]byte(aws.ToString(out.Policy)), &policy)
	}
	statements, _ := policy["Statement"].([]interface{})

	want := map[string]interface{}{
		"Sid":       "GlacierVaultAllowCloudFrontOAC",
		"Effect":    "Allow",
		"Principal": map[string]interface{}{"Service": "cloudfront.amazonaws.com"},
		"Action":    "s3:GetObject",
		"Resource":  fmt.Sprintf("arn:aws:s3:::%s/*", bucket),
		"Condition": map[string]interface{}{
			"StringEquals": map[string]interface{}{"AWS:SourceArn": distARN},
		},
	}
	for _, s := range statements {
		if m, ok := s.(map[string]interface{}); ok && m["Sid"] == want["Sid"] {
			return nil // already present
		}
	}
	policy["Statement"] = append(statements, want)
	doc, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal bucket policy: %w", err)
	}
	if _, err := s3c.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(string(doc)),
	}); err != nil {
		return fmt.Errorf("put bucket policy: %w", err)
	}
	return nil
}

// waitDeployed polls until the distribution status is Deployed.
func waitDeployed(ctx context.Context, cf *cloudfront.Client, distID string, logf func(string)) error {
	deadline := time.Now().Add(20 * time.Minute)
	for {
		out, err := cf.GetDistribution(ctx, &cloudfront.GetDistributionInput{
			Id: aws.String(distID),
		})
		if err != nil {
			return err
		}
		status := aws.ToString(out.Distribution.Status)
		if status == "Deployed" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting (status %s)", status)
		}
		logf("CloudFront distribution status: " + status + " — waiting...")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
		}
	}
}
