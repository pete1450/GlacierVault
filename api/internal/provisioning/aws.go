package provisioning

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// ValidateCredentials calls STS GetCallerIdentity to verify the credentials.
// Returns (true, identityARN, nil) on success.
func ValidateCredentials(ctx context.Context, accessKey, secretKey, region string) (bool, string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return false, "", fmt.Errorf("load config: %w", err)
	}
	client := sts.NewFromConfig(cfg)
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return false, "", err
	}
	return true, aws.ToString(out.Arn), nil
}

// CreateIAMAccessKey creates an IAM access key for iamUser and returns (accessKeyID, secretAccessKey).
func CreateIAMAccessKey(ctx context.Context, adminAccessKey, adminSecretKey, region, iamUser string) (string, string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(adminAccessKey, adminSecretKey, "")),
	)
	if err != nil {
		return "", "", err
	}
	client := iam.NewFromConfig(cfg)
	out, err := client.CreateAccessKey(ctx, &iam.CreateAccessKeyInput{
		UserName: aws.String(iamUser),
	})
	if err != nil {
		return "", "", err
	}
	return aws.ToString(out.AccessKey.AccessKeyId), aws.ToString(out.AccessKey.SecretAccessKey), nil
}
