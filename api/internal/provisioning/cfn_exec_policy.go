package provisioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// CfnExecPolicyName is the customer managed policy attached to the CDK
// CloudFormation execution (cfn-exec) role in place of CDK's default
// AdministratorAccess. The setup wizard creates it (see EnsureCfnExecPolicy)
// before running `cdk bootstrap`, and bootstrap attaches it via
// --cloudformation-execution-policies.
const CfnExecPolicyName = "GlacierVaultCfnExecScoped"

// CfnExecPolicyArn returns the ARN of the scoped cfn-exec policy.
func CfnExecPolicyArn(accountID string) string {
	return fmt.Sprintf("arn:aws:iam::%s:policy/%s", accountID, CfnExecPolicyName)
}

// CfnExecPolicyDocument returns the IAM policy document scoping the
// CloudFormation execution role to exactly what the stack creates. The stack
// (upstream rustic-rs/rustic-aws, glacier-cold-storage-cdk) creates only:
// 4 S3 buckets, 1 SQS queue + notification, 1 batch role, 1 user + policy.
// No Lambda, no KMS keys, no nested stacks, no custom resources — and
// CloudFront is provisioned by the app itself, not by this stack, so the role
// needs no CloudFront permissions at all.
//
// The only Resource: "*" is the read-only discovery statement: those five
// list actions do not support resource-level permissions.
func CfnExecPolicyDocument(accountID, region, stackName string) string {
	doc := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Sid":    "StackBuckets",
				"Effect": "Allow",
				"Action": []string{
					"s3:CreateBucket", "s3:DeleteBucket",
					"s3:GetBucketLocation", "s3:ListBucket",
					"s3:GetBucketPolicy", "s3:PutBucketPolicy", "s3:DeleteBucketPolicy",
					"s3:GetLifecycleConfiguration", "s3:PutLifecycleConfiguration",
					"s3:GetBucketVersioning", "s3:PutBucketVersioning",
					"s3:GetEncryptionConfiguration", "s3:PutEncryptionConfiguration",
					"s3:GetBucketPublicAccessBlock", "s3:PutBucketPublicAccessBlock",
					"s3:GetBucketOwnershipControls", "s3:PutBucketOwnershipControls",
					"s3:GetBucketNotification", "s3:PutBucketNotification",
					"s3:GetBucketTagging", "s3:PutBucketTagging",
					"s3:GetBucketAcl", "s3:PutBucketAcl",
				},
				"Resource": fmt.Sprintf("arn:aws:s3:::%s-*", stackName),
			},
			{
				"Sid":    "StackQueue",
				"Effect": "Allow",
				"Action": []string{
					"sqs:CreateQueue", "sqs:DeleteQueue",
					"sqs:GetQueueAttributes", "sqs:SetQueueAttributes", "sqs:GetQueueUrl",
					"sqs:TagQueue", "sqs:UntagQueue", "sqs:ListQueueTags",
				},
				"Resource": fmt.Sprintf("arn:aws:sqs:%s:%s:%s-*", region, accountID, stackName),
			},
			{
				"Sid":    "StackBatchRole",
				"Effect": "Allow",
				"Action": []string{
					"iam:CreateRole", "iam:DeleteRole", "iam:GetRole",
					"iam:TagRole", "iam:UntagRole", "iam:UpdateAssumeRolePolicy",
					"iam:PutRolePolicy", "iam:DeleteRolePolicy", "iam:GetRolePolicy",
					"iam:ListRolePolicies",
				},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:role/%s-s3batchrole*", accountID, stackName),
			},
			{
				"Sid":    "StackUser",
				"Effect": "Allow",
				"Action": []string{
					"iam:CreateUser", "iam:DeleteUser", "iam:GetUser",
					"iam:TagUser", "iam:UntagUser",
					"iam:PutUserPolicy", "iam:DeleteUserPolicy", "iam:GetUserPolicy",
					"iam:ListUserPolicies",
					"iam:AttachUserPolicy", "iam:DetachUserPolicy",
					"iam:ListAttachedUserPolicies",
				},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:user/%s-user*", accountID, stackName),
			},
			{
				"Sid":    "StackUserPolicy",
				"Effect": "Allow",
				"Action": []string{
					"iam:CreatePolicy", "iam:DeletePolicy", "iam:GetPolicy",
					"iam:ListPolicyVersions", "iam:GetPolicyVersion",
					"iam:CreatePolicyVersion", "iam:DeletePolicyVersion",
					"iam:TagPolicy", "iam:UntagPolicy",
				},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:policy/%s-*", accountID, stackName),
			},
			{
				"Sid":    "ReadOnlyDiscovery",
				"Effect": "Allow",
				"Action": []string{
					"sqs:ListQueues", "s3:ListAllMyBuckets",
					"iam:ListPolicies", "iam:ListRoles", "iam:ListUsers",
				},
				"Resource": "*",
			},
		},
	}
	raw, _ := json.Marshal(doc)
	return string(raw)
}

// EnsureCfnExecPolicy creates the scoped cfn-exec managed policy if it does
// not exist yet and returns its ARN. It is idempotent: if the policy already
// exists (re-running setup), its ARN is returned as-is.
//
// Requires the setup credential to have iam:CreatePolicy and iam:GetPolicy
// on the policy ARN (see docs/setup.md step 1).
func EnsureCfnExecPolicy(ctx context.Context, accessKey, secretKey, region, accountID, stackName string) (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	client := iam.NewFromConfig(cfg)
	arn := CfnExecPolicyArn(accountID)
	_, err = client.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(CfnExecPolicyName),
		PolicyDocument: aws.String(CfnExecPolicyDocument(accountID, region, stackName)),
		Description:    aws.String("Scoped CloudFormation execution policy for the GlacierVault stack (replaces AdministratorAccess on the CDK cfn-exec role)."),
	})
	if err == nil {
		return arn, nil
	}
	var exists *iamtypes.EntityAlreadyExistsException
	if errors.As(err, &exists) {
		out, getErr := client.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: aws.String(arn)})
		if getErr != nil {
			return "", fmt.Errorf("get existing policy: %w", getErr)
		}
		return aws.ToString(out.Policy.Arn), nil
	}
	return "", fmt.Errorf("create policy: %w — the setup credential needs iam:CreatePolicy and iam:GetPolicy on %s (see docs/setup.md step 1)", err, arn)
}
