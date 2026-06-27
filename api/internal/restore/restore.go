package restore

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/glaciervault/api/internal/engine"
)

const warmupBin = "warmup-s3-archives"

// Status values for restore_jobs.
const (
	StatusQueued             = "queued"
	StatusWarmupRequested    = "warmup_requested"
	StatusRetrievalInProgress = "retrieval_in_progress"
	StatusRetrievalComplete  = "retrieval_complete"
	StatusRestoring          = "restoring"
	StatusCompleted          = "completed"
	StatusFailed             = "failed"
)

// Manager handles the full Glacier restore lifecycle.
type Manager struct {
	db     *sql.DB
	engine *engine.Engine
	sqsURL string
}

func New(db *sql.DB, eng *engine.Engine, sqsURL string) *Manager {
	return &Manager{db: db, engine: eng, sqsURL: sqsURL}
}

// Initiate creates a restore job record and starts the workflow asynchronously.
func (m *Manager) Initiate(ctx context.Context, snapshotRowID int64, requestedPaths []string, destination string) (int64, error) {
	pathsJSON := toJSONArray(requestedPaths)

	res, err := m.db.ExecContext(ctx, `
		INSERT INTO restore_jobs (snapshot_id, requested_paths, destination, status, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		snapshotRowID, pathsJSON, destination, StatusQueued, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	jobID, _ := res.LastInsertId()

	// Kick off the workflow in the background.
	go func() {
		bgCtx := context.Background()
		if err := m.run(bgCtx, jobID, snapshotRowID, requestedPaths, destination); err != nil {
			log.Printf("restore job %d failed: %v", jobID, err)
			m.setStatus(bgCtx, jobID, StatusFailed, err.Error())
		}
	}()

	return jobID, nil
}

func (m *Manager) run(ctx context.Context, jobID, snapshotRowID int64, paths []string, destination string) error {
	// Look up rustic snapshot ID.
	var rusticID string
	row := m.db.QueryRowContext(ctx, `SELECT snapshot_id FROM snapshots WHERE id = ?`, snapshotRowID)
	if err := row.Scan(&rusticID); err != nil {
		return fmt.Errorf("lookup snapshot: %w", err)
	}

	// Step 1: Warmup (submit Glacier retrieval requests).
	m.setStatus(ctx, jobID, StatusWarmupRequested, "")
	if err := m.executeWarmup(ctx, jobID, rusticID); err != nil {
		return fmt.Errorf("warmup: %w", err)
	}

	// Step 2: Poll SQS until retrieval is complete.
	m.setStatus(ctx, jobID, StatusRetrievalInProgress, "")
	m.db.ExecContext(ctx, `UPDATE restore_jobs SET retrieval_started_at=? WHERE id=?`, time.Now().UTC(), jobID)

	if err := m.pollRetrieval(ctx, jobID); err != nil {
		return fmt.Errorf("poll retrieval: %w", err)
	}

	m.setStatus(ctx, jobID, StatusRetrievalComplete, "")

	// Step 3: Execute rustic restore.
	m.setStatus(ctx, jobID, StatusRestoring, "")
	m.db.ExecContext(ctx, `UPDATE restore_jobs SET restore_started_at=? WHERE id=?`, time.Now().UTC(), jobID)

	buf := engine.GetBuffer(jobID)
	if err := m.engine.RunRestore(ctx, buf, rusticID, destination, paths); err != nil {
		return fmt.Errorf("rustic restore: %w", err)
	}

	m.db.ExecContext(ctx, `UPDATE restore_jobs SET completed_at=? WHERE id=?`, time.Now().UTC(), jobID)
	m.setStatus(ctx, jobID, StatusCompleted, "")
	return nil
}

// executeWarmup invokes the warmup-s3-archives binary with the snapshot ID.
func (m *Manager) executeWarmup(ctx context.Context, jobID int64, rusticSnapshotID string) error {
	cmd := exec.CommandContext(ctx, warmupBin, "restore", rusticSnapshotID)
	cmd.Env = os.Environ()

	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	buf := engine.GetBuffer(jobID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			buf.Write("[warmup] " + scanner.Text())
		}
	}()

	runErr := cmd.Run()
	pw.Close()
	<-done
	pr.Close()
	return runErr
}

// pollRetrieval polls the SQS queue for ObjectRestore:Completed events every 60s.
// It times out after 24 hours (Glacier Deep Archive standard retrieval SLA).
func (m *Manager) pollRetrieval(ctx context.Context, jobID int64) error {
	deadline := time.Now().Add(24 * time.Hour)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	buf := engine.GetBuffer(jobID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("retrieval timeout: exceeded 24h")
			}
			completed, err := m.checkSQS(ctx, jobID)
			if err != nil {
				buf.Write(fmt.Sprintf("[sqs] error: %v", err))
				continue
			}
			if completed {
				buf.Write("[sqs] retrieval complete")
				return nil
			}
			buf.Write("[sqs] waiting for retrieval...")
		}
	}
}

// checkSQS polls SQS for ObjectRestore:Completed notifications.
func (m *Manager) checkSQS(ctx context.Context, jobID int64) (bool, error) {
	cmd := exec.CommandContext(ctx, "aws", "sqs", "receive-message",
		"--queue-url", m.sqsURL,
		"--max-number-of-messages", "10",
		"--wait-time-seconds", "20",
		"--output", "json",
	)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	// Look for ObjectRestore:Completed in the message body.
	return strings.Contains(string(out), "ObjectRestore:Completed"), nil
}

func (m *Manager) setStatus(ctx context.Context, jobID int64, status, errMsg string) {
	m.db.ExecContext(ctx, `UPDATE restore_jobs SET status=?, error_message=? WHERE id=?`,
		status, nilIfEmpty(errMsg), jobID)
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func toJSONArray(paths []string) string {
	if len(paths) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, p := range paths {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"`)
		b.WriteString(strings.ReplaceAll(p, `"`, `\"`))
		b.WriteString(`"`)
	}
	b.WriteString("]")
	return b.String()
}
