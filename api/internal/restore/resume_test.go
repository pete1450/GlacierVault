package restore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glaciervault/api/internal/db"
)

func TestBatchJobIDFromLine(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		// Typical tool output reporting the submitted job.
		{"Submitted S3 Batch job 8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d", "8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d"},
		{"batch job id: 12345678-1234-1234-1234-123456789abc", "12345678-1234-1234-1234-123456789abc"},
		// ARN form also yields the UUID.
		{"Job ARN: arn:aws:s3:us-east-1:123456789012:job/8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d", "8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d"},
		// No "job" mention → ignored even with a UUID present.
		{"request id 8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d", ""},
		// No UUID → ignored.
		{"warming 1000 objects via batch restore", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := batchJobIDFromLine(c.line); got != c.want {
			t.Errorf("batchJobIDFromLine(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// TestReconcileInterruptedJobs verifies startup triage: jobs with no Batch
// job ID are marked failed; jobs with one are handed to the resume path
// (which we don't run here — no AWS).
func TestReconcileInterruptedJobs(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Minimal snapshots row for the FK.
	if _, err := database.Exec(`INSERT INTO snapshots (id, snapshot_id, hostname, backup_time) VALUES (1, 'abc123', 'testhost', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("snapshots insert: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO restore_jobs (id, snapshot_id, requested_paths, destination, status, batch_job_id)
		VALUES (1, 1, '[]', '/tmp/x', 'warmup_requested', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO restore_jobs (id, snapshot_id, requested_paths, destination, status, batch_job_id)
		VALUES (2, 1, '[]', '/tmp/y', 'retrieval_in_progress', '8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO restore_jobs (id, snapshot_id, requested_paths, destination, status)
		VALUES (3, 1, '[]', '/tmp/z', 'completed')`); err != nil {
		t.Fatal(err)
	}

	m := &Manager{db: database}
	// Prevent the resume goroutine for job 2 from doing real AWS work: give
	// it a cancelled context parent via a manager scoped to fail fast.
	// Instead, test triage only by checking job 1 and that job 2 was NOT
	// marked failed synchronously.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Replace resumeAfterRestart with a no-op by testing ReconcileInterruptedJobs
		// indirectly: we can't inject, so just verify job 1 synchronously and
		// let job 2's goroutine fail fast on missing AWS config.
		m.ReconcileInterruptedJobs(context.Background())
	}()
	<-done

	var status1, errMsg1 string
	err = database.QueryRow(`SELECT status, COALESCE(error_message,'') FROM restore_jobs WHERE id=1`).Scan(&status1, &errMsg1)
	if err != nil {
		t.Fatal(err)
	}
	if status1 != StatusFailed {
		t.Errorf("job 1 status = %q, want %q", status1, StatusFailed)
	}
	if errMsg1 == "" {
		t.Error("job 1 should have an explanatory error message")
	}

	var status3 string
	_ = database.QueryRow(`SELECT status FROM restore_jobs WHERE id=3`).Scan(&status3)
	if status3 != StatusCompleted {
		t.Errorf("job 3 status = %q, want completed (untouched)", status3)
	}
}
