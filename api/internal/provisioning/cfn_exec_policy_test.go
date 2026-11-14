package provisioning

import (
	"encoding/json"
	"strings"
	"testing"
)

type cfnExecPolicyDoc struct {
	Version   string `json:"Version"`
	Statement []struct {
		Sid       string                       `json:"Sid"`
		Effect    string                       `json:"Effect"`
		Action    []string                     `json:"Action"`
		Resource  string                       `json:"Resource"`
		Condition map[string]map[string]string `json:"Condition"`
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
	if len(parsed.Statement) != 11 {
		t.Fatalf("expected 11 statements, got %d", len(parsed.Statement))
	}

	wantSids := []string{"StackBuckets", "StackQueue", "StackBatchRole", "StackUser", "StackUserPolicy", "NotificationsHandlerRole", "NotificationsHandlerRolePass", "NotificationsHandlerRolePolicy", "NotificationsHandlerFunction", "BootstrapVersionParameter", "ReadOnlyDiscovery"}
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

	// Resource patterns use the first 20 characters of the stack name,
	// bare with no separator after it, so they match both truncated
	// physical names ("rustic-cold-backups-scope-...") and untruncated
	// ones ("rustic-cold-backups-scoped-...").
	wantResources := map[string]string{
		"StackBuckets":    "arn:aws:s3:::rustic-cold-backups-*",
		"StackQueue":      "arn:aws:sqs:us-east-1:788142818300:rustic-cold-backups-*",
		"StackBatchRole":  "arn:aws:iam::788142818300:role/rustic-cold-backups-*",
		"StackUser":       "arn:aws:iam::788142818300:user/rustic-cold-backups-*",
		"StackUserPolicy": "arn:aws:iam::788142818300:policy/rustic-cold-backups-*",
		// The handler role/function names are truncated in both the stack
		// part and the logical part, so the pattern carries a wildcard
		// between the prefix and the handler name.
		"NotificationsHandlerRole":       "arn:aws:iam::788142818300:role/rustic-cold-backups-*BucketNotificationsHandle*",
		"NotificationsHandlerRolePass":   "arn:aws:iam::788142818300:role/rustic-cold-backups-*BucketNotificationsHandle*",
		"NotificationsHandlerRolePolicy": "arn:aws:iam::788142818300:role/rustic-cold-backups-*BucketNotificationsHandle*",
		"NotificationsHandlerFunction":   "arn:aws:lambda:us-east-1:788142818300:function/rustic-cold-backups-*BucketNotificationsHandle*",
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
		// carries no stack name — everything else must carry the prefix
		// (the first 20 characters of the stack name, or the whole name
		// when it is shorter).
		if st.Sid == "BootstrapVersionParameter" {
			continue
		}
		if !strings.Contains(st.Resource, "my-stack") {
			t.Errorf("statement %s: Resource %q missing stack name prefix", st.Sid, st.Resource)
		}
	}
	if got := parsed.Statement[1].Resource; got != "arn:aws:sqs:eu-west-1:111122223333:my-stack*" {
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

func TestCfnExecResourcePrefix(t *testing.T) {
	// The first 20 characters: "rustic-cold-backups-" for both stack
	// names, so one policy covers every stack in the family.
	if got := cfnExecResourcePrefix("rustic-cold-backups-scoped"); got != "rustic-cold-backups-" {
		t.Errorf("cfnExecResourcePrefix = %q", got)
	}
	if got := cfnExecResourcePrefix("rustic-cold-backups-scoped2"); got != "rustic-cold-backups-" {
		t.Errorf("cfnExecResourcePrefix = %q", got)
	}
	if got := cfnExecResourcePrefix("my-stack"); got != "my-stack" {
		t.Errorf("cfnExecResourcePrefix(\"my-stack\") = %q", got)
	}
	if got := cfnExecResourcePrefix(""); got != "" {
		t.Errorf("cfnExecResourcePrefix(\"\") = %q", got)
	}
}

// globMatch reports whether value matches an IAM-style pattern where "*"
// matches any (possibly empty) sequence.
func globMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == value
	}
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}
	rest := value[len(parts[0]):]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, p)
		if i < 0 {
			return false
		}
		rest = rest[i+len(p):]
	}
	if last := parts[len(parts)-1]; last != "" {
		return strings.HasSuffix(rest, last)
	}
	return true
}

func TestCfnExecPolicyMatchesObservedNames(t *testing.T) {
	// Regression test for the 2026-11-14 deploy failures: CDK truncates
	// physical names to fit service limits, so the batch buckets came out
	// as "rustic-cold-backups-scope-..." and the policy's
	// "rustic-cold-backups-scoped-*" pattern did not match them — and the
	// second stack name ("scoped2") truncated two characters, so the
	// drop-one-character prefix did not match either. The truncated names
	// below are copied from the two deploy logs; the untruncated ones are
	// the forms the other resources take.
	checkNames := func(t *testing.T, stackName string, cases map[string][]string) {
		t.Helper()
		parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", stackName)
		bySid := map[string]string{}
		for _, st := range parsed.Statement {
			bySid[st.Sid] = st.Resource
		}
		for sid, names := range cases {
			pattern, ok := bySid[sid]
			if !ok {
				t.Errorf("stack %s: missing statement %q", stackName, sid)
				continue
			}
			for _, name := range names {
				if !globMatch(pattern, name) {
					t.Errorf("stack %s, statement %s: pattern %q does not match %q", stackName, sid, pattern, name)
				}
			}
		}
	}
	checkNames(t, "rustic-cold-backups-scoped", map[string][]string{
		"StackBuckets": {
			// Truncated names, from the log (CreateBucket denied on these).
			"arn:aws:s3:::rustic-cold-backups-scope-batchmanifestsbucket86c3-xp8fyxtbdydf",
			"arn:aws:s3:::rustic-cold-backups-scope-batchreportsbucket05a6b2-tefsci874q3f",
			// Untruncated form, as the hot/cold buckets take it.
			"arn:aws:s3:::rustic-cold-backups-scoped-hotbucketbee04f5d-abcdefghijkl",
		},
		"StackQueue": {
			"arn:aws:sqs:us-east-1:788142818300:rustic-cold-backups-scoped-coldeventsqueuec027cf68-abcdefgh",
		},
		"StackBatchRole": {
			"arn:aws:iam::788142818300:role/rustic-cold-backups-scoped-s3batchrole-abcdefgh",
		},
		"StackUser": {
			// From the log (rollback failed deleting this user).
			"arn:aws:iam::788142818300:user/rustic-cold-backups-scoped-user2C2B57AE-xIVQSgyirLpY",
		},
		"StackUserPolicy": {
			"arn:aws:iam::788142818300:policy/rustic-cold-backups-scoped-userpolicy-abcdefgh",
		},
		"NotificationsHandlerRole": {
			// From the log (CreateRole denied on this role).
			"arn:aws:iam::788142818300:role/rustic-cold-backups-scope-BucketNotificationsHandle-XAnYzOrQPjAt",
			// Untruncated form, in case a shorter stack name leaves it whole.
			"arn:aws:iam::788142818300:role/rustic-cold-backups-scoped-BucketNotificationsHandler-abcdefgh",
		},
		"NotificationsHandlerFunction": {
			// Expected form of the function name (the deploy never got far
			// enough to create it, so this one is inferred, not observed).
			"arn:aws:lambda:us-east-1:788142818300:function/rustic-cold-backups-scope-BucketNotificationsHandle-XAnYzOrQPjAt",
		},
	})
	checkNames(t, "rustic-cold-backups-scoped2", map[string][]string{
		"StackBuckets": {
			// Truncated names, from the second log — the stack name lost
			// TWO characters here ("scoped2" -> "scope").
			"arn:aws:s3:::rustic-cold-backups-scope-batchreportsbucket05a6b2-rsju97xtjvx5",
			"arn:aws:s3:::rustic-cold-backups-scope-batchmanifestsbucket86c3-oift89ypp2xc",
			// Untruncated form.
			"arn:aws:s3:::rustic-cold-backups-scoped2-hotbucketbee04f5d-abcdefghijkl",
		},
		"StackUser": {
			"arn:aws:iam::788142818300:user/rustic-cold-backups-scoped2-user2C2B57AE-abcdefghijkl",
		},
		"NotificationsHandlerRole": {
			// From the second log (CreateRole denied on this role).
			"arn:aws:iam::788142818300:role/rustic-cold-backups-scope-BucketNotificationsHandle-IrUXrnNmMrC1",
		},
	})
	// And the patterns must not match unrelated resources.
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")
	bySid := map[string]string{}
	for _, st := range parsed.Statement {
		bySid[st.Sid] = st.Resource
	}
	if globMatch(bySid["StackBuckets"], "arn:aws:s3:::some-other-bucket") {
		t.Errorf("StackBuckets pattern matches an unrelated bucket")
	}
	if globMatch(bySid["StackUser"], "arn:aws:iam::788142818300:user/someone-else") {
		t.Errorf("StackUser pattern matches an unrelated user")
	}
}

func TestCfnExecPolicyRollbackPermissions(t *testing.T) {
	// Regression test for the 2026-11-14 rollback failure: deleting the
	// user needs iam:ListAccessKeys, deleting the roles needs
	// iam:DetachRolePolicy.
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")
	want := map[string][]string{
		"StackUser":                {"iam:ListAccessKeys", "iam:DeleteAccessKey"},
		"StackBatchRole":           {"iam:DetachRolePolicy", "iam:ListAttachedRolePolicies"},
		"NotificationsHandlerRole": {"iam:DetachRolePolicy", "iam:ListAttachedRolePolicies"},
	}
	for _, st := range parsed.Statement {
		actions, ok := want[st.Sid]
		if !ok {
			continue
		}
		have := map[string]bool{}
		for _, a := range st.Action {
			have[a] = true
		}
		for _, a := range actions {
			if !have[a] {
				t.Errorf("statement %s: missing rollback action %q", st.Sid, a)
			}
		}
	}
	for sid := range want {
		found := false
		for _, st := range parsed.Statement {
			if st.Sid == sid {
				found = true
			}
		}
		if !found {
			t.Errorf("missing statement %q", sid)
		}
	}
}

func TestCfnExecPolicyHandlerConditions(t *testing.T) {
	// PassRole and AttachRolePolicy carry conditions that would deny the
	// other role actions if they shared a statement (the condition keys
	// are absent from those requests' context), so each lives in its own
	// statement with its own condition.
	parsed := parseCfnExecDoc(t, "788142818300", "us-east-1", "rustic-cold-backups-scoped")
	bySid := map[string]struct {
		Actions   []string
		Condition map[string]map[string]string
	}{}
	for _, st := range parsed.Statement {
		bySid[st.Sid] = struct {
			Actions   []string
			Condition map[string]map[string]string
		}{st.Action, st.Condition}
	}
	role, ok := bySid["NotificationsHandlerRole"]
	if !ok {
		t.Fatal("missing NotificationsHandlerRole statement")
	}
	if len(role.Condition) != 0 {
		t.Errorf("NotificationsHandlerRole must carry no condition, got %v", role.Condition)
	}
	for _, a := range role.Actions {
		if a == "iam:PassRole" || a == "iam:AttachRolePolicy" {
			t.Errorf("NotificationsHandlerRole must not contain %q (it has its own conditioned statement)", a)
		}
	}
	pass, ok := bySid["NotificationsHandlerRolePass"]
	if !ok {
		t.Fatal("missing NotificationsHandlerRolePass statement")
	}
	if len(pass.Actions) != 1 || pass.Actions[0] != "iam:PassRole" {
		t.Errorf("NotificationsHandlerRolePass actions = %v, want only iam:PassRole", pass.Actions)
	}
	if pass.Condition["StringEquals"]["iam:PassedToService"] != "lambda.amazonaws.com" {
		t.Errorf("NotificationsHandlerRolePass condition = %v", pass.Condition)
	}
	attach, ok := bySid["NotificationsHandlerRolePolicy"]
	if !ok {
		t.Fatal("missing NotificationsHandlerRolePolicy statement")
	}
	if len(attach.Actions) != 1 || attach.Actions[0] != "iam:AttachRolePolicy" {
		t.Errorf("NotificationsHandlerRolePolicy actions = %v, want only iam:AttachRolePolicy", attach.Actions)
	}
	if attach.Condition["ArnEquals"]["iam:PolicyARN"] != "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole" {
		t.Errorf("NotificationsHandlerRolePolicy condition = %v", attach.Condition)
	}
	fn, ok := bySid["NotificationsHandlerFunction"]
	if !ok {
		t.Fatal("missing NotificationsHandlerFunction statement")
	}
	for _, want := range []string{"lambda:CreateFunction", "lambda:DeleteFunction", "lambda:InvokeFunction"} {
		found := false
		for _, a := range fn.Actions {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("NotificationsHandlerFunction: missing action %q", want)
		}
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
