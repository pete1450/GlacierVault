package cloudfront

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// EnsureReady provisions (or reuses) the CloudFront distribution and
// guarantees a usable signing key. It wraps Ensure: when Ensure reuses an
// existing GlacierVault-managed distribution whose private signing key was
// never persisted — e.g. a previous run created the distribution but failed
// at the bucket-policy step — the key pair is rotated onto the existing
// distribution instead of failing and demanding manual deletion.
func EnsureReady(ctx context.Context, db *sql.DB, cfg EnsureConfig) (*Info, string, error) {
	info, keyPEM, err := Ensure(ctx, cfg)
	if err != nil {
		return nil, "", err
	}
	if keyPEM != "" {
		return info, keyPEM, nil // fresh provisioning; caller persists via Save
	}
	stored, err := Load(db)
	if err != nil {
		return nil, "", err
	}
	if stored.PrivateKeyEnc != "" {
		return info, "", nil // reuse with the key already stored; Save merges
	}
	logf := cfg.Log
	if logf == nil {
		logf = func(string) {}
	}
	logf("Existing distribution has no stored signing key — rotating a fresh key pair onto it...")
	return RotateKey(ctx, cfg, info)
}

// RotateKey recovers a managed distribution whose signing key was lost: it
// generates a fresh key pair, creates a new CloudFront public key and key
// group, and attaches the new key group to the distribution's default cache
// behavior, replacing the previous trust. It also (idempotently) ensures
// the cold bucket's policy grants the distribution read access, since the
// failed run may have stopped before that step. It returns the updated info
// and the new private key PEM; the caller persists both via Save.
//
// The orphaned public key / key group from the failed run are left in the
// account — nothing references them after the update, so they are harmless.
func RotateKey(ctx context.Context, cfg EnsureConfig, info *Info) (*Info, string, error) {
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

	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, "", err
	}

	// Unique names: a failed run may have left same-named keys behind, and
	// CloudFront requires key / key-group names to be unique per account.
	base := "glaciervault-" + sanitize(cfg.StackName)
	suffix := time.Now().Unix()

	logf("Creating replacement CloudFront public key...")
	pubKeyID, err := createPublicKey(ctx, cf, fmt.Sprintf("%s-key-%d", base, suffix), kp.PublicKeyPEM)
	if err != nil {
		return nil, "", err
	}

	logf("Creating replacement CloudFront key group...")
	keyGroupID, err := createKeyGroup(ctx, cf, fmt.Sprintf("%s-keygroup-%d", base, suffix), pubKeyID)
	if err != nil {
		return nil, "", err
	}

	logf("Attaching the new key group to the distribution...")
	gdc, err := cf.GetDistributionConfig(ctx, &cloudfront.GetDistributionConfigInput{
		Id: aws.String(info.DistributionID),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get distribution config: %w", err)
	}
	distCfg := gdc.DistributionConfig
	distCfg.DefaultCacheBehavior.TrustedKeyGroups = &cftypes.TrustedKeyGroups{
		Enabled:  aws.Bool(true),
		Quantity: aws.Int32(1),
		Items:    []string{keyGroupID},
	}
	if _, err := cf.UpdateDistribution(ctx, &cloudfront.UpdateDistributionInput{
		Id:                 aws.String(info.DistributionID),
		IfMatch:            gdc.ETag,
		DistributionConfig: distCfg,
	}); err != nil {
		return nil, "", fmt.Errorf("update distribution trust: %w", err)
	}

	logf("Granting the distribution read access to the cold bucket...")
	if err := allowDistributionOnBucket(ctx, s3c, cfg.ColdBucket, info.ARN); err != nil {
		return nil, "", err
	}

	updated := *info
	updated.KeyPairID = pubKeyID
	updated.KeyGroupID = keyGroupID
	return &updated, kp.PrivateKeyPEM, nil
}
