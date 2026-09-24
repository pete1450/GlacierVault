package provisioning

import (
	"encoding/json"
	"testing"
)

func TestSnapshotDeletePolicyDocument(t *testing.T) {
	doc := SnapshotDeletePolicyDocument("my-cold-bucket", "my-hot-bucket")

	var parsed struct {
		Version   string `json:"Version"`
		Statement []struct {
			Effect   string   `json:"Effect"`
			Action   []string `json:"Action"`
			Resource []string `json:"Resource"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("policy document is not valid JSON: %v", err)
	}
	if parsed.Version != "2012-10-17" {
		t.Errorf("unexpected version %q", parsed.Version)
	}
	if len(parsed.Statement) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(parsed.Statement))
	}
	st := parsed.Statement[0]
	if st.Effect != "Allow" {
		t.Errorf("unexpected effect %q", st.Effect)
	}
	// Only s3:DeleteObject — nothing broader.
	if len(st.Action) != 1 || st.Action[0] != "s3:DeleteObject" {
		t.Errorf("unexpected actions %v", st.Action)
	}
	want := map[string]bool{
		"arn:aws:s3:::my-cold-bucket/*": false,
		"arn:aws:s3:::my-hot-bucket/*":  false,
	}
	for _, r := range st.Resource {
		if _, ok := want[r]; !ok {
			t.Errorf("unexpected resource %q", r)
		}
		want[r] = true
	}
	for r, seen := range want {
		if !seen {
			t.Errorf("missing resource %q", r)
		}
	}
}
