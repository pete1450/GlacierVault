package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/glaciervault/api/internal/scheduler"
)

// openMigratedDB creates a temp sqlite DB with foreign keys on and applies
// the 001 initial schema migration.
func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema, err := os.ReadFile("../db/migrations/001_initial_schema.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return db
}

func deleteBackupRequest(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/backups/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.handleDeleteBackup(rec, req)
	return rec
}

func TestHandleDeleteBackupDetachesReferences(t *testing.T) {
	db := openMigratedDB(t)
	if _, err := db.Exec(`INSERT INTO backup_definitions (id, name, source_paths, schedule, compression_level, enabled, encrypted_password)
		VALUES (1, 'docs', '["/mnt/docs"]', 'daily', 3, 1, 'x')`); err != nil {
		t.Fatalf("insert definition: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO backup_jobs (backup_def_id, status) VALUES (1, 'completed')`); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO snapshots (snapshot_id, backup_def_id, hostname, backup_time)
		VALUES ('snap1', 1, 'host', '2026-09-23 00:00:00')`); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	s := &Server{DB: db, Scheduler: scheduler.New(db, nil, nil, nil)}
	rec := deleteBackupRequest(t, s, "1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body %q)", rec.Code, rec.Body.String())
	}

	var defCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM backup_definitions WHERE id=1`).Scan(&defCount); err != nil {
		t.Fatalf("count defs: %v", err)
	}
	if defCount != 0 {
		t.Fatalf("backup definition was not deleted")
	}

	// History must survive, detached.
	var jobDef, snapDef sql.NullInt64
	if err := db.QueryRow(`SELECT backup_def_id FROM backup_jobs WHERE id=1`).Scan(&jobDef); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if err := db.QueryRow(`SELECT backup_def_id FROM snapshots WHERE id=1`).Scan(&snapDef); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if jobDef.Valid {
		t.Errorf("backup_jobs.backup_def_id not detached: %d", jobDef.Int64)
	}
	if snapDef.Valid {
		t.Errorf("snapshots.backup_def_id not detached: %d", snapDef.Int64)
	}
}

func TestHandleDeleteBackupNoReferences(t *testing.T) {
	db := openMigratedDB(t)
	if _, err := db.Exec(`INSERT INTO backup_definitions (id, name, source_paths, schedule, compression_level, enabled, encrypted_password)
		VALUES (7, 'photos', '["/mnt/photos"]', 'daily', 3, 1, 'x')`); err != nil {
		t.Fatalf("insert definition: %v", err)
	}
	s := &Server{DB: db, Scheduler: scheduler.New(db, nil, nil, nil)}
	rec := deleteBackupRequest(t, s, "7")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body %q)", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM backup_definitions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("definition not deleted: n=%d err=%v", n, err)
	}
}
