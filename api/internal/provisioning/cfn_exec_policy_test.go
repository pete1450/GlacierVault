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
	if len(parsed.Statement) != 7 {
		t.Fatalf("expected 7 statements, got %d", len(parsed.Statement))
	}

	wantSids := []string{"StackBuckets", "StackQueue", "StackBatchRole", "StackUser", "StackUserPolicy", "BootstrapVersionParameter", "ReadOnlyDiscovery"}
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
		"StackBuckets":    "arn:aws:s3:::rustic-cold-backups-scoped-*",
		"StackQueue":      "arn:aws:sqs:us-east-1:788142818300:rustic-cold-backups-scoped-*",
		"StackBatchRole":  "arn:aws:iam::788142818300:role/rustic-cold-backups-scoped-s3batchrole*",
		"StackUser":       "arn:aws:iam::788142818300:user/rustic-cold-backups-scoped-user*",
		"StackUserPolicy": "arn:aws:iam::788142818300:policy/rustic-cold-backups-scoped-*",
		// CDK resolves every stack's BootstrapVersion parameter from SSM at
		// deploy time using the execution role; without this, changeset
		// creation fails with AccessDenied on ssm:GetParameters.
		"BootstrapVersionParameter": "arn:aws:ssm:us-east-1:788142818300:parameter/cdk-bootstrap/hnb659fds/version",
		"ReadOnlyDiscovery":         "*",
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
		// The bootstrap version parameter is shared across stacks, so it
		// carries no stack name — everything else must carry it.
		if st.Sid == "BootstrapVersionParameter" {
			continue
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

func TestPolicyDocumentsEqual(t *testing.T) {
	// Same document with keys in a different order and different whitespace:
	// the stored version was marshalled from a Go map, so key order is
	// random — this must still count as equal, or every setup re-run would
	// mint a new policy version for nothing.
	a := `{"Version": "2012-10-17", "Statement": [{"Sid": "A", "Effect": "Allow", "Action": ["s3:GetObject"], "Resource": "arn:aws:s3:::b/*"}]}`
	b := "{\"Statement\": [{\"Resource\": \"arn:aws:s3:::b/*\", \"Action\": [\"s3:GetObject\"], \"Effect\": \"Allow\", \"Sid\": \"A\"}], \"Version\": \"2012-10-17\"}"
	equal, err := policyDocumentsEqual(a, b)
	if err != nil {
		t.Fatalf("policyDocumentsEqual returned error: %v", err)
	}
	if !equal {
		t.Errorf("documents with reordered keys should be equal")
	}

	// A real difference must not compare equal.
	c := `{"Version": "2012-10-17", "Statement": [{"Sid": "A", "Effect": "Allow", "Action": ["s3:PutObject"], "Resource": "arn:aws:s3:::b/*"}]}`
	equal, err = policyDocumentsEqual(a, c)
	if err != nil {
		t.Fatalf("policyDocumentsEqual returned error: %v", err)
	}
	if equal {
		t.Errorf("documents with different actions should not be equal")
	}

	// Invalid JSON must error, not silently compare unequal.
	if _, err := policyDocumentsEqual(a, "{not json"); err == nil {
		t.Errorf("expected error for invalid JSON, got nil")
	}
}

func TestCfnExecPolicyDocumentIncludesBootstrapVersion(t *testing.T) {
	// Regression test for the deploy failure: CloudFormation resolves the
	// BootstrapVersion SSM parameter at changeset creation using the
	// execution role, so the scoped policy must allow ssm:GetParameters
	// on the bootstrap version parameter.
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")
	found := false
	for _, st := range parsed.Statement {
		if st.Sid != "BootstrapVersionParameter" {
			continue
		}
		found = true
		wantActions := map[string]bool{"ssm:GetParameter": false, "ssm:GetParameters": false}
		for _, a := range st.Action {
			if _, ok := wantActions[a]; !ok {
				t.Errorf("BootstrapVersionParameter: unexpected action %q", a)
			}
			wantActions[a] = true
		}
		for a, seen := range wantActions {
			if !seen {
				t.Errorf("BootstrapVersionParameter: missing action %q", a)
			}
		}
	}
	if !found {
		t.Errorf("policy document is missing the BootstrapVersionParameter statement")
	}
}
