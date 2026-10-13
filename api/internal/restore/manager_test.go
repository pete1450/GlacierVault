package restore

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testRestoreDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", t.TempDir()+"/test.db?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := `
	CREATE TABLE restore_config (id INTEGER PRIMARY KEY, download_gb_per_day INTEGER);
	CREATE TABLE restore_jobs (
		id INTEGER PRIMARY KEY, snapshot_id INTEGER, requested_paths TEXT,
		destination TEXT, status TEXT, created_at TEXT
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGetRestoreTuningDefault(t *testing.T) {
	db := testRestoreDB(t)
	m := New(db, nil, nil, nil)
	cfg, err := m.GetRestoreTuning(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg.DownloadGBPerDay <= 0 {
		t.Fatalf("bad default: %d", cfg.DownloadGBPerDay)
	}
}

func TestSaveRestoreTuning(t *testing.T) {
	db := testRestoreDB(t)
	m := New(db, nil, nil, nil)
	ctx := context.Background()

	if err := m.SaveRestoreTuning(ctx, RestoreTuning{DownloadGBPerDay: 500}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg, _ := m.GetRestoreTuning(ctx)
	if cfg.DownloadGBPerDay != 500 {
		t.Fatalf("got %d, want 500", cfg.DownloadGBPerDay)
	}

	// Out of range.
	if err := m.SaveRestoreTuning(ctx, RestoreTuning{DownloadGBPerDay: 0}); err == nil {
		t.Fatal("expected error for 0")
	}
	if err := m.SaveRestoreTuning(ctx, RestoreTuning{DownloadGBPerDay: 100001}); err == nil {
		t.Fatal("expected error for 100001")
	}
}
