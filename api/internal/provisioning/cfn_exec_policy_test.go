package provisioning

import (
	"encoding/json"
	"strings"
	"testing"
)

type cfnExecPolicyDoc struct {
	Version   string `json:"Version"`
	Statement []struct {
		Sid      string   `json:"Sid"`
		Effect   string   `json:"Effect"`
		Action   []string `json:"Action"`
		Resource string   `json:"Resource"`
	} `json:"Statement"`
}

func parseCfnExecDoc(t *testing.T, accountID, region, stackName string) cfnExecPolicyDoc {
	t.Helper()
	var parsed cfnExecPolicyDoc
	if err := json.Unmarshal([]byte(CfnExecPolicyDocument(accountID, region, stackName)), &parsed); err != nil {
		t.Fatalf("policy document is not valid JSON: %v", err)
	}
	return parsed
}

func TestCfnExecPolicyDocument(t *testing.T) {
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")

	if parsed.Version != "2012-10-17" {
		t.Errorf("unexpected version %q", parsed.Version)
	}
	if len(parsed.Statement) != 6 {
		t.Fatalf("expected 6 statements, got %d", len(parsed.Statement))
	}

	wantSids := []string{"StackBuckets", "StackQueue", "StackBatchRole", "StackUser", "StackUserPolicy", "ReadOnlyDiscovery"}
	for i, want := range wantSids {
		if parsed.Statement[i].Sid != want {
			t.Errorf("statement %d: Sid = %q, want %q", i, parsed.Statement[i].Sid, want)
		}
		if parsed.Statement[i].Effect != "Allow" {
			t.Errorf("statement %s: Effect = %q, want Allow", want, parsed.Statement[i].Effect)
		}
		if len(parsed.Statement[i].Action) == 0 {
			t.Errorf("statement %s: no actions", want)
		}
	}
}

func TestCfnExecPolicyDocumentResources(t *testing.T) {
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")

	wantResources := map[string]string{
		"StackBuckets":      "arn:aws:s3:::rustic-cold-backups-scoped-*",
		"StackQueue":        "arn:aws:sqs:us-east-1:788142818300:rustic-cold-backups-scoped-*",
		"StackBatchRole":    "arn:aws:iam::788142818300:role/rustic-cold-backups-scoped-s3batchrole*",
		"StackUser":         "arn:aws:iam::788142818300:user/rustic-cold-backups-scoped-user*",
		"StackUserPolicy":   "arn:aws:iam::788142818300:policy/rustic-cold-backups-scoped-*",
		"ReadOnlyDiscovery": "*",
	}
	for _, st := range parsed.Statement {
		want, ok := wantResources[st.Sid]
		if !ok {
			t.Errorf("unexpected statement %q", st.Sid)
			continue
		}
		if st.Resource != want {
			t.Errorf("statement %s: Resource = %q, want %q", st.Sid, st.Resource, want)
		}
	}
}

func TestCfnExecPolicyDocumentNoWildcardsOrAdmin(t *testing.T) {
	doc := CfnExecPolicyDocument("788142818300", "us-east-1", "rustic-cold-backups-scoped")
	if strings.Contains(doc, "AdministratorAccess") {
		t.Errorf("policy document must not reference AdministratorAccess")
	}
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")
	for _, st := range parsed.Statement {
		for _, a := range st.Action {
			if strings.HasSuffix(a, ":*") {
				t.Errorf("statement %s: wildcard action %q", st.Sid, a)
			}
		}
		// The only Resource "*" allowed is the read-only discovery statement:
		// those list actions do not support resource-level permissions.
		if st.Resource == "*" && st.Sid != "ReadOnlyDiscovery" {
			t.Errorf("statement %s: unexpected Resource \"*\"", st.Sid)
		}
		if st.Resource == "*" {
			for _, a := range st.Action {
				if !strings.HasPrefix(a, "sqs:List") && !strings.HasPrefix(a, "s3:List") && !strings.HasPrefix(a, "iam:List") {
					t.Errorf("statement %s: non-list action %q on Resource \"*\"", st.Sid, a)
				}
			}
		}
	}
}

func TestCfnExecPolicyDocumentNoCloudFront(t *testing.T) {
	// CloudFront is provisioned by the app itself, not by the CDK stack,
	// so the execution role needs no CloudFront permissions at all.
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")
	for _, st := range parsed.Statement {
		for _, a := range st.Action {
			if strings.HasPrefix(a, "cloudfront:") {
				t.Errorf("statement %s: unexpected CloudFront action %q", st.Sid, a)
			}
		}
	}
}

func TestCfnExecPolicyDocumentSubstitutesInputs(t *testing.T) {
	// Different account/region/stack must flow into the resource ARNs,
	// so a misconfigured wizard input cannot silently scope the wrong stack.
	parsed := parseCfnExecDoc(t, "111122223333", "eu-west-1", "my-stack")
	for _, st := range parsed.Statement {
		if st.Sid == "ReadOnlyDiscovery" {
			continue
		}
		if !strings.Contains(st.Resource, "111122223333") && !strings.HasPrefix(st.Resource, "arn:aws:s3:::") {
			t.Errorf("statement %s: Resource %q missing account ID", st.Sid, st.Resource)
		}
		if !strings.Contains(st.Resource, "my-stack") {
			t.Errorf("statement %s: Resource %q missing stack name", st.Sid, st.Resource)
		}
	}
	if got := parsed.Statement[1].Resource; got != "arn:aws:sqs:eu-west-1:111122223333:my-stack-*" {
		t.Errorf("StackQueue Resource = %q", got)
	}
}

func TestCfnExecPolicyArn(t *testing.T) {
	if got := CfnExecPolicyArn("788142818300"); got != "arn:aws:iam::788142818300:policy/GlacierVaultCfnExecScoped" {
		t.Errorf("CfnExecPolicyArn = %q", got)
	}
	if CfnExecPolicyName != "GlacierVaultCfnExecScoped" {
		t.Errorf("CfnExecPolicyName = %q", CfnExecPolicyName)
	}
}

func TestBootstrapArgs(t *testing.T) {
	args := bootstrapArgs("788142818300", "us-east-1", "arn:aws:iam::788142818300:policy/GlacierVaultCfnExecScoped")
	want := []string{
		"bootstrap", "--force",
		"--cloudformation-execution-policies", "arn:aws:iam::788142818300:policy/GlacierVaultCfnExecScoped",
		"aws://788142818300/us-east-1",
	}
	if len(args) != len(want) {
		t.Fatalf("bootstrapArgs = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("bootstrapArgs[%d] = %q, want %q", i, args[i], want[i])
		}
	}
	// The flag must come before the positional aws:// target.
	flagIdx, targetIdx := -1, -1
	for i, a := range args {
		if a == "--cloudformation-execution-policies" {
			flagIdx = i
		}
		if strings.HasPrefix(a, "aws://") {
			targetIdx = i
		}
	}
	if flagIdx == -1 {
		t.Errorf("bootstrapArgs missing --cloudformation-execution-policies flag")
	}
	if targetIdx != -1 && flagIdx > targetIdx {
		t.Errorf("execution-policies flag must precede the aws:// target")
	}
}
