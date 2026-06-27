package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/glaciervault/api/internal/catalog"
	"github.com/glaciervault/api/internal/engine"
)

// namedSchedules maps friendly names to cron expressions.
var namedSchedules = map[string]string{
	"daily":   "0 2 * * *",
	"weekly":  "0 2 * * 0",
	"hourly":  "0 * * * *",
	"6hourly": "0 */6 * * *",
}

// NormalizeCron converts named schedules to cron expressions.
func NormalizeCron(schedule string) string {
	if expr, ok := namedSchedules[schedule]; ok {
		return expr
	}
	return schedule
}

// BackupDef is a minimal representation used by the scheduler.
type BackupDef struct {
	ID          int64
	Name        string
	SourcePaths []string
	Tags        []string
	Schedule    string
	Password    string
	Enabled     bool
}

// Scheduler manages cron-driven backup jobs.
type Scheduler struct {
	mu      sync.Mutex
	cron    *cron.Cron
	db      *sql.DB
	engine  *engine.Engine
	catalog *catalog.Catalog
	entries map[int64]cron.EntryID
	runFn   func(def BackupDef) // injectable for testing
}

func New(db *sql.DB, eng *engine.Engine, cat *catalog.Catalog) *Scheduler {
	s := &Scheduler{
		cron:    cron.New(),
		db:      db,
		engine:  eng,
		catalog: cat,
		entries: make(map[int64]cron.EntryID),
	}
	s.runFn = s.runBackup
	return s
}

// Start loads backup definitions from DB and starts the cron daemon.
func (s *Scheduler) Start(ctx context.Context) error {
	defs, err := s.loadDefs(ctx)
	if err != nil {
		return err
	}
	for _, def := range defs {
		if err := s.schedule(def); err != nil {
			log.Printf("scheduler: skip %q: %v", def.Name, err)
		}
	}
	s.cron.Start()
	return nil
}

// Stop gracefully shuts down the cron daemon.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// Reload re-reads all backup definitions and updates running entries.
func (s *Scheduler) Reload(ctx context.Context) error {
	defs, err := s.loadDefs(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove all existing entries.
	for _, eid := range s.entries {
		s.cron.Remove(eid)
	}
	s.entries = make(map[int64]cron.EntryID)

	for _, def := range defs {
		if err := s.scheduleLocked(def); err != nil {
			log.Printf("scheduler: reload skip %q: %v", def.Name, err)
		}
	}
	return nil
}

func (s *Scheduler) schedule(def BackupDef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scheduleLocked(def)
}

func (s *Scheduler) scheduleLocked(def BackupDef) error {
	if !def.Enabled {
		return nil
	}
	expr := NormalizeCron(def.Schedule)
	captured := def
	eid, err := s.cron.AddFunc(expr, func() {
		s.runFn(captured)
	})
	if err != nil {
		return fmt.Errorf("invalid cron %q: %w", expr, err)
	}
	s.entries[def.ID] = eid
	return nil
}

func (s *Scheduler) runBackup(def BackupDef) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()

	// Create job record.
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_jobs (backup_def_id, started_at, status) VALUES (?, ?, 'running')`,
		def.ID, time.Now().UTC(),
	)
	if err != nil {
		log.Printf("scheduler: create job record: %v", err)
		return
	}
	jobID, _ := res.LastInsertId()

	buf := engine.GetBuffer(jobID)
	buf.Write(fmt.Sprintf("Starting backup for %q", def.Name))

	runErr := s.engine.RunBackup(ctx, buf, def.SourcePaths, def.Tags)

	status := "completed"
	errMsg := ""
	if runErr != nil {
		status = "failed"
		errMsg = runErr.Error()
		log.Printf("scheduler: backup %q failed: %v", def.Name, runErr)
	}

	logLines := ""
	for _, l := range buf.Lines() {
		logLines += l + "\n"
	}

	s.db.ExecContext(ctx, `
		UPDATE backup_jobs SET status=?, completed_at=?, error_message=?, log_output=?
		WHERE id=?`,
		status, time.Now().UTC(), errMsg, logLines, jobID,
	)

	if runErr == nil {
		if err := s.catalog.SyncAfterBackup(ctx, def.ID); err != nil {
			log.Printf("scheduler: catalog sync: %v", err)
		}
	}
}

func (s *Scheduler) loadDefs(ctx context.Context) ([]BackupDef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, source_paths, schedule, encrypted_password, enabled FROM backup_definitions WHERE enabled = 1`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []BackupDef
	for rows.Next() {
		var d BackupDef
		var sourcePaths string
		var enabled int
		if err := rows.Scan(&d.ID, &d.Name, &sourcePaths, &d.Schedule, &d.Password, &enabled); err != nil {
			return nil, err
		}
		d.Enabled = enabled == 1
		// Parse JSON array of paths.
		d.SourcePaths = parseJSONStringArray(sourcePaths)
		defs = append(defs, d)
	}
	return defs, rows.Err()
}

func parseJSONStringArray(s string) []string {
	// Minimal JSON string array parser to avoid extra imports.
	s = s[1 : len(s)-1] // strip [ ]
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range splitJSON(s) {
		part = trim(part, `"`)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitJSON(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trim(s, cutset string) string {
	for len(s) > 0 && contains(cutset, s[0:1]) {
		s = s[1:]
	}
	for len(s) > 0 && contains(cutset, s[len(s)-1:]) {
		s = s[:len(s)-1]
	}
	return s
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub || len(s) > 0 && (s[:len(sub)] == sub || contains(s[1:], sub)))
}
