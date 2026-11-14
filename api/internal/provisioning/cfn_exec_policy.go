package provisioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"

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

// cfnExecPrefixLen is the number of leading characters of the stack name
// used to scope the execution role's permissions. CDK generates physical
// names from the stack name and truncates them to fit service limits (S3:
// 63 chars, IAM: 64 chars); the truncation splits the available budget
// between the stack-name part and the logical-ID part, so the stack-name
// part keeps at most ~25 characters — observed across two deploys, the
// batch buckets and the notification handler role all kept exactly 25,
// whether the stack name lost one character ("scoped") or two ("scoped2").
// Using 20 leaves margin for a longer hash or a tighter limit, and still
// identifies this stack's resources uniquely.
const cfnExecPrefixLen = 20

// cfnExecResourcePrefix returns the resource-name prefix used to scope the
// execution role's permissions: the first cfnExecPrefixLen characters of
// the stack name (or the whole name if it is shorter). The pattern is used
// bare, with no separator after it ("<prefix>*" rather than "<prefix>-*"),
// so it matches both truncated names ("rustic-cold-backups-scope-...") and
// untruncated ones ("rustic-cold-backups-scoped-...").
func cfnExecResourcePrefix(stackName string) string {
	if len(stackName) <= cfnExecPrefixLen {
		return stackName
	}
	return stackName[:cfnExecPrefixLen]
}

// CfnExecPolicyDocument returns the IAM policy document scoping the
// CloudFormation execution role to exactly what the stack creates. The stack
// (upstream rustic-rs/rustic-aws, glacier-cold-storage-cdk) creates:
// 4 S3 buckets, 1 SQS queue + notification, 1 batch role, 1 user + policy,
// plus the Lambda function and execution role CDK uses to configure the S3
// bucket notification (BucketNotificationsHandler). No KMS keys, no nested
// stacks, and no other custom resources besides that notification handler —
// and CloudFront is provisioned by the app itself, not by this stack, so the
// role needs no CloudFront permissions at all.
//
// The only Resource: "*" is the read-only discovery statement: those five
// list actions do not support resource-level permissions.
func CfnExecPolicyDocument(accountID, region, stackName string) string {
	prefix := cfnExecResourcePrefix(stackName)
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
				"Resource": fmt.Sprintf("arn:aws:s3:::%s*", prefix),
			},
			{
				"Sid":    "StackQueue",
				"Effect": "Allow",
				"Action": []string{
					"sqs:CreateQueue", "sqs:DeleteQueue",
					"sqs:GetQueueAttributes", "sqs:SetQueueAttributes", "sqs:GetQueueUrl",
					"sqs:TagQueue", "sqs:UntagQueue", "sqs:ListQueueTags",
				},
				"Resource": fmt.Sprintf("arn:aws:sqs:%s:%s:%s*", region, accountID, prefix),
			},
			{
				"Sid":    "StackBatchRole",
				"Effect": "Allow",
				"Action": []string{
					"iam:CreateRole", "iam:DeleteRole", "iam:GetRole",
					"iam:TagRole", "iam:UntagRole", "iam:UpdateAssumeRolePolicy",
					"iam:PutRolePolicy", "iam:DeleteRolePolicy", "iam:GetRolePolicy",
					"iam:ListRolePolicies", "iam:ListAttachedRolePolicies",
					"iam:DetachRolePolicy",
				},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:role/%s*", accountID, prefix),
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
					// CloudFormation lists (and deletes) access keys before
					// deleting the user during rollback.
					"iam:ListAccessKeys", "iam:DeleteAccessKey",
				},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:user/%s*", accountID, prefix),
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
				"Resource": fmt.Sprintf("arn:aws:iam::%s:policy/%s*", accountID, prefix),
			},
			{
				// CDK implements S3 bucket notifications with a Lambda-backed
				// custom resource (BucketNotificationsHandler) and its
				// execution role. Both the stack-name part and the logical
				// part of the name are truncated ("BucketNotificationsHandle"),
				// so the pattern uses the truncated "Handle" form — it
				// matches the full "Handler" form too — with a wildcard
				// between the stack prefix and the handler name so both the
				// truncated ("scope-...") and untruncated ("scoped-...")
				// stack forms match.
				"Sid":    "NotificationsHandlerRole",
				"Effect": "Allow",
				"Action": []string{
					"iam:CreateRole", "iam:DeleteRole", "iam:GetRole",
					"iam:TagRole", "iam:UntagRole", "iam:UpdateAssumeRolePolicy",
					"iam:PutRolePolicy", "iam:DeleteRolePolicy", "iam:GetRolePolicy",
					"iam:ListRolePolicies", "iam:ListAttachedRolePolicies",
					// CloudFormation detaches attached policies before
					// deleting the role during rollback.
					"iam:DetachRolePolicy",
				},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:role/%s*BucketNotificationsHandle*", accountID, prefix),
			},
			{
				// PassRole is a separate statement: its PassedToService
				// condition would deny the other role actions if it were
				// on the same statement (the key is absent from their
				// request context). Used when CloudFormation creates the
				// handler function with this role.
				"Sid":      "NotificationsHandlerRolePass",
				"Effect":   "Allow",
				"Action":   []string{"iam:PassRole"},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:role/%s*BucketNotificationsHandle*", accountID, prefix),
				"Condition": map[string]interface{}{
					"StringEquals": map[string]string{
						"iam:PassedToService": "lambda.amazonaws.com",
					},
				},
			},
			{
				"Sid":      "NotificationsHandlerRolePolicy",
				"Effect":   "Allow",
				"Action":   []string{"iam:AttachRolePolicy"},
				"Resource": fmt.Sprintf("arn:aws:iam::%s:role/%s*BucketNotificationsHandle*", accountID, prefix),
				"Condition": map[string]interface{}{
					"ArnEquals": map[string]string{
						"iam:PolicyARN": "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole",
					},
				},
			},
			{
				"Sid":    "NotificationsHandlerFunction",
				"Effect": "Allow",
				"Action": []string{
					"lambda:CreateFunction", "lambda:DeleteFunction",
					"lambda:GetFunction", "lambda:GetFunctionConfiguration",
					"lambda:UpdateFunctionCode", "lambda:UpdateFunctionConfiguration",
					"lambda:TagResource", "lambda:UntagResource", "lambda:ListTags",
					// CloudFormation invokes the function as a custom
					// resource provider.
					"lambda:InvokeFunction",
				},
				// Lambda function ARNs use a COLON after "function"
				// (arn:aws:lambda:region:account:function:name), not a
				// slash like IAM roles do. A slash here never matches a
				// real function ARN, so CreateFunction/DeleteFunction were
				// denied even though the pattern looked right — that is
				// what failed the 2026-11-14 rustic-cold-scoped deploy.
				"Resource": fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s*BucketNotificationsHandle*", region, accountID, prefix),
			},
			{
				// CDK adds a BootstrapVersion parameter to every stack,
				// resolved from SSM by CloudFormation at deploy time using
				// the execution role. Without this, changeset creation
				// fails with AccessDenied on ssm:GetParameters.
				// The qualifier hnb659fds is CDK's default bootstrap
				// qualifier, matching the rest of the setup.
				"Sid":      "BootstrapVersionParameter",
				"Effect":   "Allow",
				"Action":   []string{"ssm:GetParameter", "ssm:GetParameters"},
				"Resource": fmt.Sprintf("arn:aws:ssm:%s:%s:parameter/cdk-bootstrap/hnb659fds/version", region, accountID),
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
// not exist yet, or updates its document if it differs from the expected
// one, and returns its ARN.
//
// Requires the setup credential to have iam:CreatePolicy and iam:GetPolicy
// on the policy ARN (see docs/setup.md step 1), plus iam:GetPolicyVersion,
// iam:ListPolicyVersions, iam:CreatePolicyVersion and iam:DeletePolicyVersion
// on the same ARN for the update path.
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
	doc := CfnExecPolicyDocument(accountID, region, stackName)
	_, err = client.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(CfnExecPolicyName),
		PolicyDocument: aws.String(doc),
		Description:    aws.String("Scoped CloudFormation execution policy for the GlacierVault stack (replaces AdministratorAccess on the CDK cfn-exec role)."),
	})
	if err == nil {
		return arn, nil
	}
	var exists *iamtypes.EntityAlreadyExistsException
	if !errors.As(err, &exists) {
		return "", fmt.Errorf("create policy: %w — the setup credential needs iam:CreatePolicy and iam:GetPolicy on %s (see docs/setup.md step 1)", err, arn)
	}
	// Policy already exists (re-running setup): bring its document up to
	// date if the expected policy has changed since it was created.
	if err := updateCfnExecPolicyDocument(ctx, client, arn, doc); err != nil {
		return "", err
	}
	return arn, nil
}

// updateCfnExecPolicyDocument replaces the policy's default version when its
// document differs from doc. It is a no-op when the documents already match.
// Managed policies hold at most 5 versions, so non-default versions are
// pruned first (only the default version is ever in effect).
func updateCfnExecPolicyDocument(ctx context.Context, client *iam.Client, arn, doc string) error {
	pol, err := client.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: aws.String(arn)})
	if err != nil {
		return fmt.Errorf("get policy: %w", err)
	}
	verOut, err := client.GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{
		PolicyArn: aws.String(arn),
		VersionId: pol.Policy.DefaultVersionId,
	})
	if err != nil {
		return fmt.Errorf("get policy version: %w", err)
	}
	current, err := url.QueryUnescape(aws.ToString(verOut.PolicyVersion.Document))
	if err != nil {
		return fmt.Errorf("decode policy document: %w", err)
	}
	equal, err := policyDocumentsEqual(current, doc)
	if err != nil {
		return fmt.Errorf("compare policy documents: %w", err)
	}
	if equal {
		return nil
	}
	listOut, err := client.ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{PolicyArn: aws.String(arn)})
	if err != nil {
		return fmt.Errorf("list policy versions: %w", err)
	}
	for _, v := range listOut.Versions {
		if aws.ToString(v.VersionId) == aws.ToString(pol.Policy.DefaultVersionId) {
			continue
		}
		if _, err := client.DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{
			PolicyArn: aws.String(arn),
			VersionId: v.VersionId,
		}); err != nil {
			return fmt.Errorf("delete old policy version %s: %w", aws.ToString(v.VersionId), err)
		}
	}
	_, err = client.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(arn),
		PolicyDocument: aws.String(doc),
		SetAsDefault:   true,
	})
	if err != nil {
		return fmt.Errorf("create policy version: %w — the setup credential needs iam:CreatePolicyVersion, iam:ListPolicyVersions, iam:GetPolicyVersion and iam:DeletePolicyVersion on %s (see docs/setup.md step 1)", err, arn)
	}
	return nil
}

// policyDocumentsEqual reports whether two IAM policy JSON documents describe
// the same policy. Key order and whitespace are ignored.
func policyDocumentsEqual(a, b string) (bool, error) {
	var pa, pb interface{}
	if err := json.Unmarshal([]byte(a), &pa); err != nil {
		return false, fmt.Errorf("parse first document: %w", err)
	}
	if err := json.Unmarshal([]byte(b), &pb); err != nil {
		return false, fmt.Errorf("parse second document: %w", err)
	}
	return reflect.DeepEqual(pa, pb), nil
}
