package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/glaciervault/api/internal/engine"
)

// Non-terminal restore statuses. After a container restart no goroutine is
// alive for any of these rows, so every one of them needs triage.
var nonTerminalStatuses = []string{
	StatusQueued,
	StatusWarmupRequested,
	StatusRetrievalInProgress,
	StatusRetrievalComplete,
	StatusRestoring,
}

// batchJobIDPattern matches S3 Batch Operations job IDs (UUIDs). The capture
// only fires on lines that also mention "job", so unrelated UUIDs in tool
// output are ignored.
var batchJobIDPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// batchJobIDFromLine extracts the S3 Batch job ID from a line of
// warmup-s3-archives output, or "" when the line doesn't report one.
func batchJobIDFromLine(line string) string {
	if !strings.Contains(strings.ToLower(line), "job") {
		return ""
	}
	return batchJobIDPattern.FindString(line)
}

// warmupCompleteSentinel is printed by the glaciervault-warmup wrapper when its
// last batch exits 0 — i.e. every pack's restored copy is live (SQS-confirmed
// by warmup-s3-archives). An S3 Batch restore job reporting Complete only
// means restore *requests* were initiated, so the Batch job status is NOT a
// thaw signal; this line is.
const warmupCompleteSentinel = "glaciervault-warmup: warmup complete"

// warmupLineHook returns the LineHook for a restore invocation. It records
// the first S3 Batch job ID it sees (first-write-wins: later batches submit
// their own jobs, but only the first ID is kept for debugging), and watches
// for the wrapper's warmup-complete sentinel — the true end of the thaw.
// On the sentinel the status advances honestly to retrieval_complete and the
// warmup notification fires exactly once.
func (m *Manager) warmupLineHook(ctx context.Context, jobID int64) func(string) {
	var batchJobID string
	return func(line string) {
		if strings.Contains(line, warmupCompleteSentinel) {
			m.setStatus(ctx, jobID, StatusRetrievalComplete, "")
			m.notifyWarmupOnce(jobID, batchJobID)
			return
		}
		id := batchJobIDFromLine(line)
		if id == "" {
			return
		}
		if batchJobID == "" {
			batchJobID = id
		}
		res, err := m.db.ExecContext(ctx,
			`UPDATE restore_jobs SET batch_job_id=?, status=?, retrieval_started_at=?
			 WHERE id=? AND batch_job_id IS NULL`,
			id, StatusRetrievalInProgress, time.Now().UTC(), jobID)
		if err != nil {
			log.Printf("restore job %d: record batch job id: %v", jobID, err)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("restore job %d: warmup Batch job %s", jobID, id)
		}
	}
}

// ReconcileInterruptedJobs triages restore jobs left in non-terminal states
// by a container restart. Jobs that never got a Batch job ID are marked
// failed (nothing server-side was started; safe to retry). Jobs with a
// recorded Batch job ID resume: a background goroutine re-attaches to the
// in-flight S3 Batch job via DescribeJob and runs the download phase once
// Glacier reports the packs restored.
func (m *Manager) ReconcileInterruptedJobs(ctx context.Context) {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(nonTerminalStatuses)), ",")
	args := make([]interface{}, len(nonTerminalStatuses))
	for i, s := range nonTerminalStatuses {
		args[i] = s
	}
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, batch_job_id FROM restore_jobs WHERE status IN (`+placeholders+`)`, args...)
	if err != nil {
		log.Printf("restore reconcile: query: %v", err)
		return
	}
	defer rows.Close()

	var interrupted, resumed int
	for rows.Next() {
		var id int64
		var batchJobID sql.NullString
		if err := rows.Scan(&id, &batchJobID); err != nil {
			log.Printf("restore reconcile: scan: %v", err)
			continue
		}
		if !batchJobID.Valid || batchJobID.String == "" {
			m.setStatus(ctx, id, StatusFailed,
				"Interrupted by container restart before the S3 Batch restore job was submitted. "+
					"No data was changed — safe to start a new restore from the Snapshots page.")
			interrupted++
			continue
		}
		resumed++
		go m.resumeAfterRestart(context.Background(), id)
	}
	if err := rows.Err(); err != nil {
		log.Printf("restore reconcile: rows: %v", err)
	}
	if interrupted+resumed > 0 {
		log.Printf("restore reconcile: %d interrupted job(s) marked failed, %d warmup(s) resumed",
			interrupted, resumed)
	}
}

// notifyWarmupOnce fires the warmup-complete notification at most once per
// restore job. warmup_notified_at acts as an atomic claim so the happy-path
// watcher and the restart-resume path cannot double-deliver.
func (m *Manager) notifyWarmupOnce(jobID int64, batchJobID string) {
	if m.notify == nil {
		return
	}
	res, err := m.db.ExecContext(context.Background(),
		`UPDATE restore_jobs SET warmup_notified_at=? WHERE id=? AND warmup_notified_at IS NULL`,
		time.Now().UTC(), jobID)
	if err != nil {
		log.Printf("restore job %d: warmup notify claim: %v", jobID, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return // already notified
	}
	m.notify.WarmupCompleted(jobID, batchJobID)
}

// resumeAfterRestart re-runs a restore interrupted by a container restart by
// simply running the happy path again. The warmup is idempotent:
// already-thawed packs are skipped (their restored-copy expiry is just
// extended) and the wrapper re-submits Batch jobs for the rest, so every
// batch is (re-)submitted and the sentinel-based completion detection works
// exactly as on the first attempt.
//
// (The previous code polled the S3 Batch job status and started the
// download when the job reported Complete — but a Batch restore job reports
// Complete when restore *requests* are initiated, not when objects are
// thawed, so a restart during the thaw wait raced the download against
// Glacier and failed. It also lost unsubmitted later batches on multi-batch
// restores; re-running the warmup fixes that too.)
func (m *Manager) resumeAfterRestart(ctx context.Context, jobID int64) {
	buf := engine.GetBuffer(jobID)
	buf.Write("[restore] container restarted during restore — re-running warmup (already-thawed packs are skipped) then downloading")

	var snapshotRowID sql.NullInt64
	var pathsJSON, destination string
	if err := m.db.QueryRowContext(ctx,
		`SELECT snapshot_id, requested_paths, destination FROM restore_jobs WHERE id=?`, jobID,
	).Scan(&snapshotRowID, &pathsJSON, &destination); err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume: load job: %v", err))
		return
	}
	if !snapshotRowID.Valid {
		m.setStatus(ctx, jobID, StatusFailed, "resume: snapshot was deleted; cannot restore")
		return
	}
	var paths []string
	if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume: parse requested paths: %v", err))
		return
	}
	// Clear the recorded Batch job so this attempt records its own.
	if _, err := m.db.ExecContext(ctx, `UPDATE restore_jobs SET batch_job_id=NULL WHERE id=?`, jobID); err != nil {
		log.Printf("restore job %d: resume clear batch job id: %v", jobID, err)
	}
	if err := m.run(ctx, jobID, snapshotRowID.Int64, paths, destination); err != nil {
		log.Printf("restore job %d: resumed run failed: %v", jobID, err)
		m.setStatus(ctx, jobID, StatusFailed, err.Error())
	}
}
