package restore

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	appCrypto "github.com/glaciervault/api/internal/crypto"
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
// AWS credentials are injected into its environment the same way as for the
// `aws` CLI: the container does not have them in its ambient environment.
func (m *Manager) executeWarmup(ctx context.Context, jobID int64, rusticSnapshotID string) error {
	env, err := m.awsEnv(ctx)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, warmupBin, "restore", rusticSnapshotID)
	cmd.Env = env

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

// awsEnv loads the deployed IAM credentials and region from the database and
// returns them as environment variables for AWS CLI subprocesses. The warmup
// tool reads credentials from rustic.toml, but the `aws` CLI needs them in the
// environment.
func (m *Manager) awsEnv(ctx context.Context) ([]string, error) {
	var region, encKey, encSecret string
	err := m.db.QueryRowContext(ctx,
		`SELECT region, encrypted_access_key, encrypted_secret_key FROM aws_config WHERE id=1`,
	).Scan(&region, &encKey, &encSecret)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	key, err := appCrypto.Decrypt(encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt access key: %w", err)
	}
	secret, err := appCrypto.Decrypt(encSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret key: %w", err)
	}
	return append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+key,
		"AWS_SECRET_ACCESS_KEY="+secret,
		"AWS_DEFAULT_REGION="+region,
	), nil
}

// sqsReceiveMessage is the parsed output of `aws sqs receive-message`.
type sqsReceiveMessage struct {
	Messages []struct {
		MessageID     string `json:"MessageId"`
		ReceiptHandle string `json:"ReceiptHandle"`
		Body          string `json:"Body"`
	} `json:"Messages"`
}

// s3Event is the S3 event notification delivered to the queue.
type s3Event struct {
	Records []struct {
		EventName string `json:"eventName"`
	} `json:"Records"`
}

// checkSQS polls SQS for ObjectRestore:Completed notifications. Processed
// messages are deleted so a later restore does not see stale events.
func (m *Manager) checkSQS(ctx context.Context, jobID int64) (bool, error) {
	if m.sqsURL == "" {
		return false, fmt.Errorf("no SQS queue configured")
	}
	env, err := m.awsEnv(ctx)
	if err != nil {
		return false, err
	}

	cmd := exec.CommandContext(ctx, "aws", "sqs", "receive-message",
		"--queue-url", m.sqsURL,
		"--max-number-of-messages", "10",
		"--wait-time-seconds", "20",
		"--output", "json",
	)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}

	var received sqsReceiveMessage
	if err := json.Unmarshal(out, &received); err != nil {
		return false, fmt.Errorf("parse sqs response: %w", err)
	}
	if len(received.Messages) == 0 {
		return false, nil
	}

	completed := false
	for _, msg := range received.Messages {
		var event s3Event
		// The body may be a bare S3 event or an SNS envelope wrapping one.
		body := msg.Body
		var envelope struct {
			Message string `json:"Message"`
		}
		if err := json.Unmarshal([]byte(body), &envelope); err == nil && envelope.Message != "" {
			body = envelope.Message
		}
		if err := json.Unmarshal([]byte(body), &event); err == nil {
			for _, rec := range event.Records {
				if strings.Contains(rec.EventName, "ObjectRestore:Completed") {
					completed = true
					break
				}
			}
		}
		// Delete the message regardless so it is not reprocessed.
		del := exec.CommandContext(ctx, "aws", "sqs", "delete-message",
			"--queue-url", m.sqsURL,
			"--receipt-handle", msg.ReceiptHandle,
		)
		del.Env = env
		if derr := del.Run(); derr != nil {
			buf := engine.GetBuffer(jobID)
			buf.Write(fmt.Sprintf("[sqs] warning: could not delete message: %v", derr))
		}
	}
	return completed, nil
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
