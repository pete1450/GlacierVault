package restore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3control"
	"github.com/aws/aws-sdk-go-v2/service/s3control/types"
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

// watchForBatchJobID returns the LineHook for a restore invocation: the
// first line reporting the S3 Batch job ID persists it immediately, so a
// later container restart can re-attach to the in-flight warmup instead of
// submitting a duplicate Batch job. The update is idempotent (WHERE
// batch_job_id IS NULL) and also advances the status honestly: the Batch
// job now exists server-side, so retrieval is in progress.
func (m *Manager) watchForBatchJobID(ctx context.Context, jobID int64) func(string) {
	return func(line string) {
		id := batchJobIDFromLine(line)
		if id == "" {
			return
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
			// The warmup tool blocks on SQS now; this watcher tracks the same
			// Batch job via DescribeJob purely to fire the warmup-complete
			// notification when Glacier reports the packs thawed.
			go m.watchWarmupForNotification(context.Background(), jobID, id)
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
		go m.resumeAfterRestart(context.Background(), id, batchJobID.String)
	}
	if err := rows.Err(); err != nil {
		log.Printf("restore reconcile: rows: %v", err)
	}
	if interrupted+resumed > 0 {
		log.Printf("restore reconcile: %d interrupted job(s) marked failed, %d warmup(s) resumed",
			interrupted, resumed)
	}
}

// batchControlClient builds an S3 Control client from the stored deployment
// credentials, plus the warmup settings (account ID for DescribeJob).
func (m *Manager) batchControlClient(ctx context.Context) (*s3control.Client, *warmupSettings, error) {
	s, err := m.loadAWSSettings(ctx)
	if err != nil {
		return nil, nil, err
	}
	ws, err := m.loadWarmupSettings(ctx)
	if err != nil {
		return nil, nil, err
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(s.region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s.accessKey, s.secretKey, "")),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("load AWS config: %w", err)
	}
	return s3control.NewFromConfig(awsCfg), ws, nil
}

// watchWarmupForNotification polls the S3 Batch job until it completes and
// then fires the warmup-complete notification exactly once. It covers the
// happy path (container alive); after a restart, resumeAfterRestart takes
// over and calls notifyWarmupOnce itself. The dedupe column makes double
// delivery impossible even if both somehow observe completion.
func (m *Manager) watchWarmupForNotification(ctx context.Context, jobID int64, batchJobID string) {
	ctl, ws, err := m.batchControlClient(ctx)
	if err != nil {
		log.Printf("restore job %d: warmup notify: %v", jobID, err)
		return
	}

	// Fresh deadline: Bulk restores can take up to 48h from submission.
	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		// Stop once the job left the warmup phases (download done, failed,
		// or cancelled) — nothing left to notify about.
		var status string
		if err := m.db.QueryRowContext(ctx, `SELECT status FROM restore_jobs WHERE id=?`, jobID).Scan(&status); err == nil {
			switch status {
			case StatusCompleted, StatusFailed:
				return
			}
		}
		out, err := ctl.DescribeJob(ctx, &s3control.DescribeJobInput{
			AccountId: aws.String(ws.accountID),
			JobId:     aws.String(batchJobID),
		})
		if err != nil {
			log.Printf("restore job %d: warmup notify DescribeJob %s: %v", jobID, batchJobID, err)
		} else {
			switch out.Job.Status {
			case types.JobStatusComplete:
				m.notifyWarmupOnce(jobID, batchJobID)
				return
			case types.JobStatusFailed, types.JobStatusCancelled:
				// The restore itself reports the failure; no warmup notification.
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
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

// resumeAfterRestart re-attaches to the S3 Batch restore job recorded in
// batch_job_id and, once Glacier reports the packs restored, runs the
// download phase (plain rustic restore, no warmup step needed).
func (m *Manager) resumeAfterRestart(ctx context.Context, jobID int64, batchJobID string) {
	buf := engine.GetBuffer(jobID)
	buf.Write(fmt.Sprintf("[restore] container restarted during warmup — re-attached to S3 Batch job %s", batchJobID))

	ctl, ws, err := m.batchControlClient(ctx)
	if err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume after restart: %v", err))
		return
	}

	// Fresh deadline for the resumed wait: Bulk restores can take up to 48h
	// from submission. Poll DescribeJob (stateless — safe across restarts).
	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	var consecutiveErrors int
	for {
		out, err := ctl.DescribeJob(ctx, &s3control.DescribeJobInput{
			AccountId: aws.String(ws.accountID),
			JobId:     aws.String(batchJobID),
		})
		if err != nil {
			consecutiveErrors++
			log.Printf("restore job %d: DescribeJob %s: %v (%d consecutive)", jobID, batchJobID, err, consecutiveErrors)
			if consecutiveErrors >= 24 {
				m.setStatus(ctx, jobID, StatusFailed,
					fmt.Sprintf("lost track of S3 Batch job %s: %v (check the job in the AWS console)", batchJobID, err))
				return
			}
		} else {
			consecutiveErrors = 0
			switch out.Job.Status {
			case types.JobStatusComplete:
				m.setStatus(ctx, jobID, StatusRetrievalComplete, "")
				m.notifyWarmupOnce(jobID, batchJobID)
				buf.Write("[restore] Glacier retrieval complete — downloading packs")
				m.downloadAfterWarmup(ctx, jobID, buf)
				return
			case types.JobStatusFailed, types.JobStatusCancelled:
				reason := string(out.Job.Status)
				if out.Job.StatusUpdateReason != nil {
					reason = *out.Job.StatusUpdateReason
				}
				m.setStatus(ctx, jobID, StatusFailed,
					fmt.Sprintf("S3 Batch restore job %s ended as %s — start a new restore to retry", batchJobID, reason))
				return
			default:
				// Active, Cancelling, etc. — keep waiting.
				buf.Write(fmt.Sprintf("[restore] Batch job %s: %s", batchJobID, out.Job.Status))
			}
		}
		select {
		case <-ctx.Done():
			m.setStatus(ctx, jobID, StatusFailed,
				"resumed warmup wait timed out — start a new restore to retry")
			return
		case <-ticker.C:
		}
	}
}

// downloadAfterWarmup runs the download phase of a restore whose packs are
// already thawed: plain rustic restore with no warm-up step, through the
// CloudFront proxy profile when enabled.
func (m *Manager) downloadAfterWarmup(ctx context.Context, jobID int64, buf *engine.RingBuffer) {
	var snapshotRowID int64
	var pathsJSON, destination string
	err := m.db.QueryRowContext(ctx,
		`SELECT snapshot_id, requested_paths, destination FROM restore_jobs WHERE id=?`, jobID,
	).Scan(&snapshotRowID, &pathsJSON, &destination)
	if err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume: load job: %v", err))
		return
	}
	var rusticID string
	if err := m.db.QueryRowContext(ctx,
		`SELECT snapshot_id FROM snapshots WHERE id=?`, snapshotRowID).Scan(&rusticID); err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume: lookup snapshot: %v", err))
		return
	}
	var paths []string
	if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume: parse requested paths: %v", err))
		return
	}
	env, err := m.awsEnv(ctx)
	if err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume: %v", err))
		return
	}
	workDir, err := m.writeWarmupConfig(ctx, jobID)
	if err != nil {
		m.setStatus(ctx, jobID, StatusFailed, fmt.Sprintf("resume: %v", err))
		return
	}
	defer os.RemoveAll(workDir)

	m.setStatus(ctx, jobID, StatusRestoring, "")
	if err := m.engine.RunRestore(ctx, buf, rusticID, destination, paths, engine.RestoreOptions{
		Warmup:     false,
		Env:        env,
		Dir:        workDir,
		ConfigPath: m.cfRestoreProfile(ctx, buf, workDir),
	}); err != nil {
		m.setStatus(ctx, jobID, StatusFailed,
			fmt.Sprintf("download after warmup: %v (if the 2-day restored copies expired, start a new restore to warm the packs again)", err))
		return
	}
	m.db.ExecContext(ctx, `UPDATE restore_jobs SET completed_at=? WHERE id=?`, time.Now().UTC(), jobID)
	m.setStatus(ctx, jobID, StatusCompleted, "")
	if m.notify != nil {
		m.notify.RestoreCompleted(jobID)
	}
}
