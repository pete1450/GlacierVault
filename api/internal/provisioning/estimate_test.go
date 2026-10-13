package provisioning

import (
	"testing"
)

func TestEstimate(t *testing.T) {
	e := Estimate()
	if e.S3Buckets != 4 {
		t.Errorf("S3Buckets = %d, want 4", e.S3Buckets)
	}
	if e.SQSQueues != 1 {
		t.Errorf("SQSQueues = %d, want 1", e.SQSQueues)
	}
	if e.IAMUsers != 1 {
		t.Errorf("IAMUsers = %d, want 1", e.IAMUsers)
	}
	if e.CloudFrontDist != 1 {
		t.Errorf("CloudFrontDist = %d, want 1", e.CloudFrontDist)
	}
	if e.Details == "" {
		t.Error("Details should not be empty")
	}
}
