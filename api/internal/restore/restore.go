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
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	appCrypto "github.com/glaciervault/api/internal/crypto"
	"github.com/glaciervault/api/internal/engine"
)

const warmupBin = "warmup-s3-archives"

// Retrieval wait tuning. Glacier Deep Archive standard retrieval SLA is
// 12-48 hours, so the timeout covers the worst case with margin.
const (
	restorePollInterval = 30 * time.Minute
	restoreTimeout      = 48 * time.Hour
	// Time allowed for warmup-submitted restore requests to become visible
	// via head-object before we conclude nothing was requested.
	restoreGracePeriod = 30 * time.Minute
	// Number of concurrent head-object calls per status check.
	headWorkers = 16
)

// Status values for restore_jobs.
const (
	StatusQueued              = "queued"
	StatusWarmupRequested     = "warmup_requested"
	StatusRetrievalInProgress = "retrieval_in_progress"
	StatusRetrievalComplete   = "retrieval_complete"
	StatusRestoring           = "restoring"
	StatusCompleted           = "completed"
	StatusFailed              = "failed"
)

// Manager handles the full Glacier restore lifecycle.
type Manager struct {
	db     *sql.DB
	engine *engine.Engine
}

func New(db *sql.DB, eng *engine.Engine) *Manager {
	return &Manager{db: db, engine: eng}
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
	m.db.ExecContext(ctx, `UPDATE restore_jobs SET warmup_status='completed' WHERE id=?`, jobID)

	// Step 2: Wait until every data pack with a pending restore request
	// reports ongoing-request="false" via head-object.
	m.setStatus(ctx, jobID, StatusRetrievalInProgress, "")
	m.db.ExecContext(ctx, `UPDATE restore_jobs SET retrieval_started_at=? WHERE id=?`, time.Now().UTC(), jobID)

	if err := m.waitForRestore(ctx, jobID); err != nil {
		return fmt.Errorf("wait for retrieval: %w", err)
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
// AWS credentials are injected into its environment: the container does not
// have them in its ambient environment.
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

// awsSettings holds the decrypted deployment credentials plus the cold bucket.
type awsSettings struct {
	region     string
	accessKey  string
	secretKey  string
	coldBucket string
}

// loadAWSSettings reads and decrypts the deployment's AWS configuration.
func (m *Manager) loadAWSSettings(ctx context.Context) (*awsSettings, error) {
	var s awsSettings
	var encKey, encSecret string
	err := m.db.QueryRowContext(ctx,
		`SELECT region, encrypted_access_key, encrypted_secret_key, cold_bucket FROM aws_config WHERE id=1`,
	).Scan(&s.region, &encKey, &encSecret, &s.coldBucket)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if s.coldBucket == "" {
		return nil, fmt.Errorf("no cold bucket configured — run setup first")
	}
	if s.accessKey, err = appCrypto.Decrypt(encKey); err != nil {
		return nil, fmt.Errorf("decrypt access key: %w", err)
	}
	if s.secretKey, err = appCrypto.Decrypt(encSecret); err != nil {
		return nil, fmt.Errorf("decrypt secret key: %w", err)
	}
	return &s, nil
}

// awsEnv returns the process environment plus the deployment's AWS
// credentials, for subprocesses (warmup tool) that need them.
func (m *Manager) awsEnv(ctx context.Context) ([]string, error) {
	s, err := m.loadAWSSettings(ctx)
	if err != nil {
		return nil, err
	}
	return append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+s.accessKey,
		"AWS_SECRET_ACCESS_KEY="+s.secretKey,
		"AWS_DEFAULT_REGION="+s.region,
	), nil
}

// s3Client builds an S3 client authenticated as the deployment's IAM user.
func (m *Manager) s3Client(ctx context.Context) (*s3.Client, string, error) {
	s, err := m.loadAWSSettings(ctx)
	if err != nil {
		return nil, "", err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(s.region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s.accessKey, s.secretKey, "")),
	)
	if err != nil {
		return nil, "", fmt.Errorf("load aws sdk config: %w", err)
	}
	return s3.NewFromConfig(cfg), s.coldBucket, nil
}

// waitForRestore polls head-object on every data pack in the cold bucket
// until all packs with a restore request report ongoing-request="false".
// This is exact: unlike the old "first SQS event wins" approach, a snapshot
// restore is not considered complete until every pack is actually available.
func (m *Manager) waitForRestore(ctx context.Context, jobID int64) error {
	client, bucket, err := m.s3Client(ctx)
	if err != nil {
		return err
	}
	buf := engine.GetBuffer(jobID)
	deadline := time.Now().Add(restoreTimeout)
	graceUntil := time.Now().Add(restoreGracePeriod)

	for {
		done, total, pending, err := m.restoreComplete(ctx, client, bucket)
		switch {
		case err != nil:
			buf.Write(fmt.Sprintf("[restore] status check failed: %v (retrying)", err))
		case done:
			buf.Write(fmt.Sprintf("[restore] all %d packs available", total))
			return nil
		case total == 0:
			buf.Write("[restore] waiting for restore requests to register...")
			if time.Now().After(graceUntil) {
				return fmt.Errorf("no restore requests detected %v after warmup — warmup may have failed", restoreGracePeriod)
			}
		default:
			buf.Write(fmt.Sprintf("[restore] %d of %d packs still restoring...", pending, total))
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("retrieval timeout: exceeded %v", restoreTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(restorePollInterval):
		}
	}
}

// restoreComplete lists data packs in the cold bucket and heads each one.
// Packs with no restore header were not requested by this warmup and are
// skipped; done is true only when at least one pack was requested and none
// are still restoring.
func (m *Manager) restoreComplete(ctx context.Context, client *s3.Client, bucket string) (done bool, total, pending int, err error) {
	// Rustic stores data packs under data/ in the cold repository.
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String("data/"),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return false, 0, 0, fmt.Errorf("list objects: %w", err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}

	type headResult struct {
		requested bool
		ongoing   bool
		err       error
	}
	jobs := make(chan string)
	results := make(chan headResult, len(keys))
	var wg sync.WaitGroup
	for w := 0; w < headWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				head, err := client.HeadObject(ctx, &s3.HeadObjectInput{
					Bucket: aws.String(bucket),
					Key:    aws.String(key),
				})
				if err != nil {
					results <- headResult{err: fmt.Errorf("head %s: %w", key, err)}
					continue
				}
				requested, ongoing := parseRestoreHeader(aws.ToString(head.Restore))
				results <- headResult{requested: requested, ongoing: ongoing}
			}
		}()
	}
	go func() {
		for _, k := range keys {
			jobs <- k
		}
		close(jobs)
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.err != nil {
			return false, 0, 0, r.err
		}
		if !r.requested {
			continue
		}
		total++
		if r.ongoing {
			pending++
		}
	}
	return total > 0 && pending == 0, total, pending, nil
}

// parseRestoreHeader interprets the S3 x-amz-restore header, e.g.
// `ongoing-request="true"` or `ongoing-request="false", expiry-date="..."`.
// An empty header means no restore was requested for the object.
func parseRestoreHeader(v string) (requested, ongoing bool) {
	if v == "" {
		return false, false
	}
	return true, strings.Contains(v, `ongoing-request="true"`)
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
