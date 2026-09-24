package provisioning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// SnapshotDeletePolicyName is the inline policy attached to the rustic IAM
// user so GlacierVault's snapshot-delete feature works.
const SnapshotDeletePolicyName = "GlacierVaultSnapshotDelete"

// SnapshotDeletePolicyDocument returns the IAM policy document granting the
// rustic user s3:DeleteObject on the hot and cold buckets.
//
// The upstream glacier-cold-storage-cdk stack intentionally omits delete
// rights on the cold bucket: append-only mode protects the archives if the
// rustic credentials leak. But rustic stores snapshot files under
// snapshots/<id> in the *cold* bucket, so `rustic forget` (snapshot delete)
// and `rustic prune` (removing unreferenced data packs) both fail without
// this grant. Attaching it is a conscious tradeoff: snapshot deletion works,
// at the cost of weakening the append-only protection for this credential.
func SnapshotDeletePolicyDocument(coldBucket, hotBucket string) string {
	doc := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Effect":   "Allow",
				"Action":   []string{"s3:DeleteObject"},
				"Resource": []string{
					fmt.Sprintf("arn:aws:s3:::%s/*", coldBucket),
					fmt.Sprintf("arn:aws:s3:::%s/*", hotBucket),
				},
			},
		},
	}
	raw, _ := json.Marshal(doc)
	return string(raw)
}

// EnsureSnapshotDeletePolicy attaches the snapshot-delete inline policy to
// iamUser. It is idempotent: PutUserPolicy overwrites any previous version.
// Requires admin credentials (iam:PutUserPolicy).
func EnsureSnapshotDeletePolicy(ctx context.Context, adminAccessKey, adminSecretKey, region, iamUser, coldBucket, hotBucket string) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(adminAccessKey, adminSecretKey, "")),
	)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	client := iam.NewFromConfig(cfg)
	_, err = client.PutUserPolicy(ctx, &iam.PutUserPolicyInput{
		UserName:       aws.String(iamUser),
		PolicyName:     aws.String(SnapshotDeletePolicyName),
		PolicyDocument: aws.String(SnapshotDeletePolicyDocument(coldBucket, hotBucket)),
	})
	if err != nil {
		return fmt.Errorf("put user policy: %w", err)
	}
	return nil
}
