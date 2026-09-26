package restore

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glaciervault/api/internal/cloudfront"
	appCrypto "github.com/glaciervault/api/internal/crypto"
	"github.com/glaciervault/api/internal/engine"
	"github.com/glaciervault/api/internal/notify"
)

// Retrieval tuning. Glacier Deep Archive Bulk tier restores can take up to
// 48 hours. The overall restore deadline is now computed per restore by
// ComputeWarmupPlan (72h per warm-up batch plus download headroom);
// restoreTimeout is the fallback when the pack count cannot be determined.
const restoreTimeout = 72 * time.Hour

// GlacierJobTier selects the S3 restore tier used by warmup-s3-archives.
// BULK is the cheapest option (~8-10x cheaper than STANDARD) at the cost of
// up to a 48-hour wait. The tool expects the all-caps AWS tier name.
const glacierJobTier = "BULK"

// restoredCopyDays is how long the temporarily restored S3 Standard copy of
// each pack is kept. 1 is the minimum AWS allows; the download starts as soon
// as the packs thaw, so a small window is enough. Note this does NOT limit
// how long we wait for the thaw: warmup-s3-archives derives its SQS wait
// budget from this same value, so rustic invokes it through the
// glaciervault-warmup wrapper, which retries the tool when its wait times
// out. Retries are safe because the tool's RestoreStatus pre-check
// short-circuits on packs that thawed between attempts, and duplicate Batch
// restore requests are idempotent.
const restoredCopyDays = 1

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
	cf     *cloudfront.Manager // localhost S3→CloudFront proxy lifecycle
	notify *notify.Manager     // apprise notifications (nil-safe: skipped when nil)
}

func New(db *sql.DB, eng *engine.Engine, cf *cloudfront.Manager, n *notify.Manager) *Manager {
	return &Manager{db: db, engine: eng, cf: cf, notify: n}
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
	// Size the warm-up before anything else: count the data packs in the
	// index, derive batches / per-batch copy expiry / download headroom /
	// the overall deadline, and log the full calculation. The per-batch
	// expiry math is explained in warmup_plan.go and docs/workflow.md.
	plan := m.computeWarmupPlan(ctx, jobID)

	// Overall deadline: Bulk tier restores can take up to 48h per batch.
	// Restarting a timed-out restore is safe — the warmup tool is idempotent.
	ctx, cancel := context.WithTimeout(ctx, plan.Timeout)
	defer cancel()

	// Look up rustic snapshot ID.
	var rusticID string
	row := m.db.QueryRowContext(ctx, `SELECT snapshot_id FROM snapshots WHERE id = ?`, snapshotRowID)
	if err := row.Scan(&rusticID); err != nil {
		return fmt.Errorf("lookup snapshot: %w", err)
	}

	// Render the warmup tool config into a per-job working directory.
	// writeWarmupConfig also drops the warmup plan file that the
	// glaciervault-warmup wrapper reads to set each batch's copy expiry.
	workDir, err := m.writeWarmupConfig(ctx, jobID, plan)
	if err != nil {
		return fmt.Errorf("write warmup config: %w", err)
	}
	defer os.RemoveAll(workDir)

	// AWS credentials for the warmup tool, which resolves credentials on
	// its own (rustic does not pass its backend credentials through).
	env, err := m.awsEnv(ctx)
	if err != nil {
		return err
	}

	// Single rustic invocation: it computes the exact pack set the snapshot
	// needs, warms those packs via warmup-s3-archives (which submits S3
	// Batch restore jobs and blocks until Glacier has them available), then
	// downloads and restores. Only needed packs are warmed, which keeps
	// retrieval as cheap as possible.
	m.setStatus(ctx, jobID, StatusWarmupRequested, "")
	buf := engine.GetBuffer(jobID)
	buf.Write("[restore] warming needed packs via warmup-s3-archives " +
		"(Bulk tier, cheapest; Glacier can take up to 48h)...")

	if err := m.engine.RunRestore(ctx, buf, rusticID, destination, paths, engine.RestoreOptions{
		Warmup:     true,
		Env:        env,
		Dir:        workDir,
		ConfigPath: m.cfRestoreProfile(ctx, buf, workDir),
		LineHook:   m.watchForBatchJobID(ctx, jobID),
	}); err != nil {
		return fmt.Errorf("rustic restore: %w", err)
	}

	m.db.ExecContext(ctx, `UPDATE restore_jobs SET completed_at=? WHERE id=?`, time.Now().UTC(), jobID)
	m.setStatus(ctx, jobID, StatusCompleted, "")
	if m.notify != nil {
		m.notify.RestoreCompleted(jobID)
	}
	return nil
}

// writeWarmupConfig renders warmup-s3-archives-config.toml into a fresh
// per-job directory and returns the directory path. The warmup tool reads
// its config from the working directory it is invoked in, so the restore
// process runs with this directory as its working directory. It also writes
// warmup-plan.txt, which the glaciervault-warmup wrapper reads to size each
// batch's restored-copy expiry (see warmup_plan.go).
func (m *Manager) writeWarmupConfig(ctx context.Context, jobID int64, plan WarmupPlan) (string, error) {
	s, err := m.loadWarmupSettings(ctx)
	if err != nil {
		return "", err
	}

	cfg := fmt.Sprintf(`# Generated by GlacierVault for restore job %d. Do not edit.
[aws_resources]
account_id = %q
cold_bucket_name = %q
batch_manifests_bucket_name = %q
batch_reports_bucket_name = %q
batch_role_arn = %q
restore_queue_url = %q

[initiate_restore_object]
expiration_in_days = %d
glacier_job_tier = %q
`,
		jobID,
		s.accountID,
		s.coldBucket,
		s.batchManifestsBucket,
		s.batchReportsBucket,
		s.batchRoleArn,
		s.restoreQueueURL,
		restoredCopyDays,
		glacierJobTier,
	)

	dir, err := os.MkdirTemp("", fmt.Sprintf("glaciervault-restore-%d-", jobID))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "warmup-s3-archives-config.toml"), []byte(cfg), 0600); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	// The per-batch plan for glaciervault-warmup: total batches and the
	// download headroom. The wrapper derives each batch's
	// expiration_in_days from these (E_k = 2·(N−k) + DL + 1).
	planFile := fmt.Sprintf(`# Generated by GlacierVault for restore job %d. Read by glaciervault-warmup; do not edit.
batches=%d
download_days=%d
data_packs=%d
`, jobID, plan.Batches, plan.DownloadDays, plan.DataPacks)
	if err := os.WriteFile(filepath.Join(dir, "warmup-plan.txt"), []byte(planFile), 0600); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// computeWarmupPlan counts the data packs in the repository index and
// derives the batch/expiry/timeout plan, logging the full calculation to
// the job log and the server log before any Batch job is submitted. If the
// count fails, it logs loudly and falls back to the single-batch plan
// (1-day expiry) — the pre-existing behavior; the restore itself will
// almost certainly fail right after anyway, since it needs the same repo.
func (m *Manager) computeWarmupPlan(ctx context.Context, jobID int64) WarmupPlan {
	rate := m.downloadGBPerDay(ctx)
	packs, err := m.engine.CountDataPacks(ctx)
	if err != nil {
		log.Printf("restore job %d: count data packs: %v — falling back to single-batch warmup plan", jobID, err)
		engine.GetBuffer(jobID).Write(fmt.Sprintf(
			"[restore] WARNING: could not count data packs (%v); using single-batch plan (1-day copy expiry).", err))
		return ComputeWarmupPlan(1, rate)
	}
	plan := ComputeWarmupPlan(packs, rate)

	var expiries []string
	for k := 1; k <= plan.Batches; k++ {
		expiries = append(expiries, fmt.Sprintf("batch %d: %dd", k, plan.ExpiryDays(k)))
	}
	msg := fmt.Sprintf("[restore] warmup plan: %d data packs in index (upper bound) → %d batch(es) of ≤%d packs; "+
		"~%.1f GB max; download headroom %dd at %d GB/day; copy expiry per batch [%s]; overall timeout %v.",
		plan.DataPacks, plan.Batches, warmupBatchSize, plan.TotalGB,
		plan.DownloadDays, rate, strings.Join(expiries, ", "), plan.Timeout)
	engine.GetBuffer(jobID).Write(msg)
	log.Printf("restore job %d: %s", jobID, strings.TrimPrefix(msg, "[restore] "))
	return plan
}

// downloadGBPerDay reads the configured conservative download rate used to
// size the download headroom. Missing/invalid rows fall back to the default.
func (m *Manager) downloadGBPerDay(ctx context.Context) int {
	var rate int
	if err := m.db.QueryRowContext(ctx,
		`SELECT download_gb_per_day FROM restore_config WHERE id=1`,
	).Scan(&rate); err != nil || rate < 1 {
		return defaultDownloadRate
	}
	return rate
}

// RestoreTuning is the user-configurable warm-up sizing knob.
type RestoreTuning struct {
	DownloadGBPerDay int `json:"downloadGbPerDay"`
}

// GetRestoreTuning reads the stored warm-up tuning.
func (m *Manager) GetRestoreTuning(ctx context.Context) (RestoreTuning, error) {
	return RestoreTuning{DownloadGBPerDay: m.downloadGBPerDay(ctx)}, nil
}

// SaveRestoreTuning stores the warm-up tuning. The rate is clamped to a
// sane range: it only sizes headroom, and absurd values would silently
// under- or over-provision multi-batch restores.
func (m *Manager) SaveRestoreTuning(ctx context.Context, t RestoreTuning) error {
	if t.DownloadGBPerDay < 1 || t.DownloadGBPerDay > 100000 {
		return fmt.Errorf("downloadGbPerDay must be between 1 and 100000")
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO restore_config (id, download_gb_per_day) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET download_gb_per_day=excluded.download_gb_per_day`,
		t.DownloadGBPerDay)
	return err
}

// awsSettings holds the decrypted deployment credentials plus the buckets.
type awsSettings struct {
	region     string
	accessKey  string
	secretKey  string
	hotBucket  string
	coldBucket string
}

// cfRestoreProfile writes a per-restore rustic profile that points the cold
// backend at the localhost CloudFront proxy, so pack downloads use
// CloudFront's free data-transfer allowance instead of paid S3 egress. It
// returns "" (use the default profile, direct S3) when the free-egress path
// is not enabled or the proxy is unreachable — restores fail open to direct
// S3 rather than failing because the proxy is down.
func (m *Manager) cfRestoreProfile(ctx context.Context, buf *engine.RingBuffer, workDir string) string {
	cfg, err := cloudfront.Load(m.db)
	if err != nil || !cfg.Enabled {
		return ""
	}
	proxyAddr := m.cf.Addr()
	if !cloudfront.Reachable(proxyAddr) {
		buf.Write("[restore] CloudFront proxy unreachable — downloading directly from S3 (paid egress).")
		log.Printf("restore: cloudfront proxy %s unreachable, using direct S3", proxyAddr)
		return ""
	}
	s, err := m.loadAWSSettings(ctx)
	if err != nil {
		buf.Write("[restore] Could not load AWS settings for CloudFront profile — downloading directly from S3.")
		log.Printf("restore: load aws settings for cf profile: %v", err)
		return ""
	}
	profile := fmt.Sprintf(`[repository]
repository = "opendal:s3"
repo-hot = "opendal:s3"
password-file = "/config/repo.password"

[repository.options]
access_key_id = %q
secret_access_key = %q
region = %q

[repository.options-hot]
bucket = %q

[repository.options-cold]
bucket = %q
default_storage_class = "DEEP_ARCHIVE"
endpoint = "http://%s"
`, s.accessKey, s.secretKey, s.region, s.hotBucket, s.coldBucket, proxyAddr)
	path := filepath.Join(workDir, "rustic-cf.toml")
	if err := os.WriteFile(path, []byte(profile), 0600); err != nil {
		log.Printf("restore: write cf profile: %v", err)
		return ""
	}
	buf.Write("[restore] Downloads will use the CloudFront free-egress path.")
	return path
}

// warmupSettings holds everything warmup-s3-archives needs in its config file.
type warmupSettings struct {
	accountID            string
	coldBucket           string
	batchManifestsBucket string
	batchReportsBucket   string
	batchRoleArn         string
	restoreQueueURL      string
}

// loadAWSSettings reads and decrypts the deployment's AWS configuration.
func (m *Manager) loadAWSSettings(ctx context.Context) (*awsSettings, error) {
	var s awsSettings
	var encKey, encSecret string
	err := m.db.QueryRowContext(ctx,
		`SELECT region, encrypted_access_key, encrypted_secret_key, hot_bucket, cold_bucket FROM aws_config WHERE id=1`,
	).Scan(&s.region, &encKey, &encSecret, &s.hotBucket, &s.coldBucket)
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

// loadWarmupSettings reads the deployment values needed for
// warmup-s3-archives-config.toml. Deployments provisioned before these
// columns existed must re-run setup to populate them.
func (m *Manager) loadWarmupSettings(ctx context.Context) (*warmupSettings, error) {
	var s warmupSettings
	var acctID, manifests, reports, roleArn, queueURL sql.NullString
	err := m.db.QueryRowContext(ctx,
		`SELECT account_id, cold_bucket, batch_manifests_bucket, batch_reports_bucket, batch_role_arn, sqs_url
		 FROM aws_config WHERE id=1`,
	).Scan(&acctID, &s.coldBucket, &manifests, &reports, &roleArn, &queueURL)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	s.accountID, s.batchManifestsBucket, s.batchReportsBucket = acctID.String, manifests.String, reports.String
	s.batchRoleArn, s.restoreQueueURL = roleArn.String, queueURL.String
	// Deployments provisioned before the ARN fix stored the bare IAM role
	// name instead of the ARN. The CDK stack creates the role with the
	// default path, so arn:aws:iam::<account>:role/<name> is exact.
	if s.batchRoleArn != "" && !strings.HasPrefix(s.batchRoleArn, "arn:") {
		s.batchRoleArn = fmt.Sprintf("arn:aws:iam::%s:role/%s", s.accountID, s.batchRoleArn)
	}
	for name, v := range map[string]string{
		"account_id": s.accountID, "cold_bucket": s.coldBucket,
		"batch_manifests_bucket": s.batchManifestsBucket,
		"batch_reports_bucket":   s.batchReportsBucket,
		"batch_role_arn":         s.batchRoleArn, "restore queue url": s.restoreQueueURL,
	} {
		if v == "" {
			return nil, fmt.Errorf("aws_config.%s is empty — re-run setup to provision the warmup infrastructure", name)
		}
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
		// The Rust AWS SDK (warmup tool) reads AWS_REGION; AWS CLI-style
		// tooling reads AWS_DEFAULT_REGION. Set both.
		"AWS_REGION="+s.region,
		"AWS_DEFAULT_REGION="+s.region,
	), nil
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
