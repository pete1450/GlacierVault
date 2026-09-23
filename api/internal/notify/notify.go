// Package notify delivers user notifications through the apprise CLI.
//
// Apprise (https://github.com/caronc/apprise) speaks 80+ notification
// services — Discord, Slack, Telegram, email, ntfy, Gotify, webhooks,
// and more — from a single URL scheme. The user configures one or more
// apprise destination URLs plus per-event switches in Settings; events
// dispatch asynchronously so a slow or failing notification service can
// never block a backup or restore.
package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Config is the stored notification configuration.
type Config struct {
	Destinations           []string `json:"destinations"`
	NotifyBackupCompleted  bool     `json:"notifyBackupCompleted"`
	NotifyWarmupCompleted  bool     `json:"notifyWarmupCompleted"`
	NotifyRestoreCompleted bool     `json:"notifyRestoreCompleted"`
}

// urlPattern validates an apprise destination URL. Strict on purpose:
// URLs become separate argv entries, so a value starting with '-' could
// be parsed as an apprise flag instead of a destination.
var urlPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://\S+$`)

// ValidateURL reports whether s is an acceptable apprise destination URL.
func ValidateURL(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("empty URL")
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("invalid URL %q: must not start with '-'", s)
	}
	if !urlPattern.MatchString(s) {
		return fmt.Errorf("invalid URL %q: expected scheme://...", s)
	}
	return nil
}

// Manager reads notification config from the database and dispatches
// through the apprise CLI.
type Manager struct {
	db         *sql.DB
	appriseBin string
}

// New locates the apprise CLI. If it is missing, event methods log and
// skip instead of failing — notifications are best-effort by design.
func New(db *sql.DB) *Manager {
	bin, err := exec.LookPath("apprise")
	if err != nil {
		log.Printf("notify: apprise CLI not found — notifications disabled until it is installed")
		bin = ""
	}
	return &Manager{db: db, appriseBin: bin}
}

// GetConfig reads the stored configuration.
func (m *Manager) GetConfig(ctx context.Context) (Config, error) {
	var cfg Config
	var destinations string
	var backup, warmup, restore int
	err := m.db.QueryRowContext(ctx, `
		SELECT destinations, notify_backup_completed, notify_warmup_completed, notify_restore_completed
		FROM notification_config WHERE id=1`,
	).Scan(&destinations, &backup, &warmup, &restore)
	if err != nil {
		return cfg, fmt.Errorf("load notification config: %w", err)
	}
	for _, line := range strings.Split(destinations, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			cfg.Destinations = append(cfg.Destinations, line)
		}
	}
	cfg.NotifyBackupCompleted = backup == 1
	cfg.NotifyWarmupCompleted = warmup == 1
	cfg.NotifyRestoreCompleted = restore == 1
	return cfg, nil
}

// SaveConfig validates and persists the configuration.
func (m *Manager) SaveConfig(ctx context.Context, cfg Config) error {
	var cleaned []string
	for _, u := range cfg.Destinations {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if err := ValidateURL(u); err != nil {
			return err
		}
		cleaned = append(cleaned, u)
	}
	toInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	_, err := m.db.ExecContext(ctx, `
		UPDATE notification_config
		SET destinations=?, notify_backup_completed=?, notify_warmup_completed=?, notify_restore_completed=?
		WHERE id=1`,
		strings.Join(cleaned, "\n"),
		toInt(cfg.NotifyBackupCompleted),
		toInt(cfg.NotifyWarmupCompleted),
		toInt(cfg.NotifyRestoreCompleted),
	)
	if err != nil {
		return fmt.Errorf("save notification config: %w", err)
	}
	return nil
}

// SendTest synchronously sends a test notification to the given URLs so
// the user can verify their destinations before saving. It does not touch
// stored config.
func (m *Manager) SendTest(ctx context.Context, urls []string) error {
	var cleaned []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if err := ValidateURL(u); err != nil {
			return err
		}
		cleaned = append(cleaned, u)
	}
	if len(cleaned) == 0 {
		return errors.New("no notification destinations provided")
	}
	return m.dispatch(ctx, "GlacierVault test", "This is a test notification from GlacierVault.", cleaned)
}

// dispatch shells out to the apprise CLI. Each URL is a separate argv
// entry (never interpolated into a shell command).
func (m *Manager) dispatch(ctx context.Context, title, body string, urls []string) error {
	if m.appriseBin == "" {
		return errors.New("apprise CLI is not installed in this container")
	}
	if len(urls) == 0 {
		return errors.New("no notification destinations configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	args := append([]string{"-t", title, "-b", body}, urls...)
	out, err := exec.CommandContext(ctx, m.appriseBin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("apprise: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// notifyEvent reads fresh config in the background, applies the given
// switch selector, and dispatches. The goroutine means the caller never
// blocks on a notification service.
func (m *Manager) notifyEvent(sel func(Config) bool, title, body string) {
	go func() {
		ctx := context.Background()
		cfg, err := m.GetConfig(ctx)
		if err != nil {
			log.Printf("notify: %v", err)
			return
		}
		if !sel(cfg) || len(cfg.Destinations) == 0 {
			return
		}
		if err := m.dispatch(ctx, title, body, cfg.Destinations); err != nil {
			log.Printf("notify: %s: %v", title, err)
		}
	}()
}

// BackupCompleted notifies that a backup finished successfully.
func (m *Manager) BackupCompleted(backupName string) {
	m.notifyEvent(func(cfg Config) bool { return cfg.NotifyBackupCompleted },
		"GlacierVault backup complete",
		fmt.Sprintf("Backup %q finished successfully.", backupName))
}

// WarmupCompleted notifies that Glacier finished thawing a restore's packs
// (the S3 Batch restore job completed); the download phase starts next.
func (m *Manager) WarmupCompleted(jobID int64, batchJobID string) {
	m.notifyEvent(func(cfg Config) bool { return cfg.NotifyWarmupCompleted },
		"GlacierVault warmup complete",
		fmt.Sprintf("Restore job %d: Glacier finished thawing the packs (Batch job %s). Download starting.", jobID, batchJobID))
}

// RestoreCompleted notifies that a restore finished and files are on disk.
func (m *Manager) RestoreCompleted(jobID int64) {
	m.notifyEvent(func(cfg Config) bool { return cfg.NotifyRestoreCompleted },
		"GlacierVault restore complete",
		fmt.Sprintf("Restore job %d finished successfully.", jobID))
}
