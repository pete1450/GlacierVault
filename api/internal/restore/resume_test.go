package restore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glaciervault/api/internal/db"
	"github.com/glaciervault/api/internal/engine"
)

func TestBatchJobIDFromLine(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		// Typical tool output reporting the submitted job.
		{"Submitted S3 Batch job 8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d", "8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d"},
		{"batch job id: 12345678-1234-1234-1234-123456789abc", "12345678-1234-1234-1234-123456789abc"},
		// The exact line warmup-s3-archives 1.3.0 emits at info level on
		// submission (this is the line the reattach hook depends on).
		{"Created S3 batch job. Job ID: 8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d", "8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d"},
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

func TestEnsureWarmupLogLevel(t *testing.T) {
	// Absent → added at info, the level the tool's job-ID line needs.
	env := ensureWarmupLogLevel([]string{"FOO=bar"})
	found := false
	for _, kv := range env {
		if kv == "RUST_LOG=info" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env=%v, want RUST_LOG=info added", env)
	}
	// Present → respected, not overridden or duplicated.
	env2 := ensureWarmupLogLevel([]string{"RUST_LOG=debug", "FOO=bar"})
	count := 0
	for _, kv := range env2 {
		if kv == "RUST_LOG=debug" {
			count++
		}
		if kv == "RUST_LOG=info" {
			t.Fatalf("env2=%v, must not add RUST_LOG=info when already set", env2)
		}
	}
	if count != 1 {
		t.Fatalf("env2=%v, want exactly one RUST_LOG entry", env2)
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

	m := &Manager{db: database, engine: engine.New("")}
	// Job 2's resume goroutine re-runs the restore, which fails fast here
	// (no rustic binary / no AWS config); wait for it to land terminal.
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.ReconcileInterruptedJobs(context.Background())
	}()
	<-done

	deadline := time.Now().Add(15 * time.Second)
	var status2 string
	for {
		_ = database.QueryRow(`SELECT status FROM restore_jobs WHERE id=2`).Scan(&status2)
		if status2 == StatusFailed || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status2 != StatusFailed {
		t.Errorf("job 2 status = %q, want %q (resume attempted and failed fast)", status2, StatusFailed)
	}

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

func TestWarmupLineHook_Sentinel(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`INSERT INTO snapshots (id, snapshot_id, hostname, backup_time) VALUES (1, 'abc123', 'testhost', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("snapshots insert: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO restore_jobs (id, snapshot_id, requested_paths, destination, status)
		VALUES (1, 1, '[]', '/tmp/x', 'retrieval_in_progress')`); err != nil {
		t.Fatal(err)
	}

	// Nil notify manager: the hook must not panic, just skip notification.
	m := &Manager{db: database, engine: engine.New("")}
	hook := m.warmupLineHook(context.Background(), 1)

	// Batch job ID capture still works through the combined hook.
	hook(`warmup-s3-archives: submitted S3 Batch job 8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d`)
	var batchID string
	_ = database.QueryRow(`SELECT batch_job_id FROM restore_jobs WHERE id=1`).Scan(&batchID)
	if batchID != "8d5a4b2c-1e3f-4a5b-8c6d-7e8f9a0b1c2d" {
		t.Errorf("batch_job_id = %q, want the submitted job", batchID)
	}

	// The wrapper's last-batch sentinel marks retrieval honestly complete.
	hook(warmupCompleteSentinel + " — all 1 batch(es) thawed; download starting")
	var status string
	_ = database.QueryRow(`SELECT status FROM restore_jobs WHERE id=1`).Scan(&status)
	if status != StatusRetrievalComplete {
		t.Errorf("status = %q, want %q", status, StatusRetrievalComplete)
	}

	// A non-sentinel, non-job line changes nothing.
	hook(`[INFO] reading index...: 49 done in 266.60ms`)
	_ = database.QueryRow(`SELECT status FROM restore_jobs WHERE id=1`).Scan(&status)
	if status != StatusRetrievalComplete {
		t.Errorf("status = %q after noise line, want %q", status, StatusRetrievalComplete)
	}
}
