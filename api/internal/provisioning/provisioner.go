package provisioning

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const cdkDir = "/opt/cdk"

// ResourceEstimate describes the AWS resources CDK will create.
type ResourceEstimate struct {
	S3Buckets int    `json:"s3Buckets"`
	SQSQueues int    `json:"sqsQueues"`
	IAMUsers  int    `json:"iamUsers"`
	IAMRoles  int    `json:"iamRoles"`
	Details   string `json:"details"`
}

// Estimate returns the static resource description for the CDK stack.
func Estimate() ResourceEstimate {
	return ResourceEstimate{
		S3Buckets: 4,
		SQSQueues: 1,
		IAMUsers:  1,
		IAMRoles:  1,
		Details:   "hot-bucket, cold-bucket (Glacier Deep Archive), batch-manifests-bucket, batch-reports-bucket, cold-events-queue, rustic-iam-user, s3-batch-role",
	}
}

// StackOutputs holds the values parsed from CloudFormation outputs after deploy.
type StackOutputs struct {
	HotBucket    string `json:"hotBucket"`
	ColdBucket   string `json:"coldBucket"`
	SQSUrl       string `json:"sqsUrl"`
	IAMUser      string `json:"iamUser"`
	BatchRoleArn string `json:"batchRoleArn"`
}

// Provisioner orchestrates CDK deploy.
type Provisioner struct {
	AccessKey string
	SecretKey string
	Region    string
	StackName string
	LogFn     func(line string) // called for each CDK output line
}

// Bootstrap runs `cdk bootstrap aws://<account>/<region>`.
func (p *Provisioner) Bootstrap(ctx context.Context) error {
	accountID, err := p.getAccountID(ctx)
	if err != nil {
		return fmt.Errorf("resolve account ID: %w", err)
	}
	return p.runCDK(ctx, "bootstrap", "--force", fmt.Sprintf("aws://%s/%s", accountID, p.Region))
}

// getAccountID calls STS GetCallerIdentity to resolve the AWS account ID.
func (p *Provisioner) getAccountID(ctx context.Context) (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(p.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(p.AccessKey, p.SecretKey, "")),
	)
	if err != nil {
		return "", err
	}
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return *out.Account, nil
}

// Deploy runs `cdk deploy` then reads resource physical IDs via CloudFormation.
func (p *Provisioner) Deploy(ctx context.Context) (*StackOutputs, error) {
	if err := p.runCDK(ctx, "deploy",
		"--require-approval", "never",
		p.StackName,
	); err != nil {
		return nil, err
	}
	return p.fetchStackResources(ctx)
}

// fetchStackResources calls CloudFormation DescribeStackResources to get physical IDs.
// The glacier-cold-storage-cdk stack defines no CfnOutputs, so we look up by logical ID.
func (p *Provisioner) fetchStackResources(ctx context.Context) (*StackOutputs, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(p.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(p.AccessKey, p.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	cfn := cloudformation.NewFromConfig(cfg)
	out, err := cfn.DescribeStackResources(ctx, &cloudformation.DescribeStackResourcesInput{
		StackName: aws.String(p.StackName),
	})
	if err != nil {
		return nil, fmt.Errorf("describe stack resources: %w", err)
	}

	so := &StackOutputs{}

	// CDK appends a hash suffix to logical IDs (e.g. "hotbucketBEE04F5D"), so match by prefix.
	for _, r := range out.StackResources {
		lid := aws.ToString(r.LogicalResourceId)
		pid := aws.ToString(r.PhysicalResourceId)
		switch {
		case strings.HasPrefix(lid, "hotbucket") && aws.ToString(r.ResourceType) == "AWS::S3::Bucket":
			so.HotBucket = pid
		case strings.HasPrefix(lid, "coldbucket") && aws.ToString(r.ResourceType) == "AWS::S3::Bucket":
			so.ColdBucket = pid
		case strings.HasPrefix(lid, "coldeventsqueue") && aws.ToString(r.ResourceType) == "AWS::SQS::Queue":
			so.SQSUrl = pid
		case strings.HasPrefix(lid, "user") && aws.ToString(r.ResourceType) == "AWS::IAM::User":
			so.IAMUser = pid
		case strings.HasPrefix(lid, "s3batchrole") && aws.ToString(r.ResourceType) == "AWS::IAM::Role":
			so.BatchRoleArn = pid
		}
	}

	if so.HotBucket == "" || so.ColdBucket == "" {
		return nil, fmt.Errorf("stack %s deployed but expected resources not found — check logical IDs", p.StackName)
	}
	return so, nil
}

func (p *Provisioner) runCDK(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "npx", append([]string{"cdk"}, args...)...)
	cmd.Dir = cdkDir
	cmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+p.AccessKey,
		"AWS_SECRET_ACCESS_KEY="+p.SecretKey,
		"AWS_DEFAULT_REGION="+p.Region,
	)

	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			if p.LogFn != nil {
				p.LogFn(scanner.Text())
			}
		}
	}()

	runErr := cmd.Run()
	pw.Close()
	<-done
	pr.Close()
	return runErr
}
