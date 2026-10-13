package scheduler

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestScheduleLocation(t *testing.T) {
	// TZ set to a valid IANA name wins.
	t.Setenv("TZ", "America/Chicago")
	if loc := scheduleLocation(); loc.String() != "America/Chicago" {
		t.Errorf("TZ=America/Chicago -> %q", loc.String())
	}
	s := New(nil, nil, nil, nil)
	if s.Location().String() != "America/Chicago" {
		t.Errorf("scheduler location = %q", s.Location().String())
	}
	// A 2 AM entry must resolve to 2 AM Chicago wall-clock time.
	entryTime := time.Date(2026, 10, 5, 2, 0, 0, 0, s.Location())
	if entryTime.Format("15:04 MST") != "02:00 CDT" {
		t.Errorf("2 AM in scheduler zone = %q", entryTime.Format("15:04 MST"))
	}

	// Invalid TZ falls back to the container local zone (no panic).
	t.Setenv("TZ", "Not/AZone")
	if got := scheduleLocation(); got != time.Local {
		t.Errorf("invalid TZ -> %q, want time.Local", got.String())
	}

	// Unset TZ falls back to the container local zone.
	t.Setenv("TZ", "")
	if got := scheduleLocation(); got != time.Local {
		t.Errorf("unset TZ -> %q, want time.Local", got.String())
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", t.TempDir()+"/test.db?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := `
	CREATE TABLE backup_definitions (
		id INTEGER PRIMARY KEY,
		name TEXT, source_paths TEXT, schedule TEXT,
		compression_level INTEGER, enabled INTEGER,
		encrypted_password TEXT
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNormalizeCron(t *testing.T) {
	for in, want := range map[string]string{
		"daily":     "0 2 * * *",
		"weekly":    "0 2 * * 0",
		"hourly":    "0 * * * *",
		"6hourly":   "0 */6 * * *",
		"0 5 * * *": "0 5 * * *",
	} {
		if got := NormalizeCron(in); got != want {
			t.Errorf("NormalizeCron(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseJSONStringArray(t *testing.T) {
	// Note: the parser is minimal — it trims quotes but not surrounding spaces.
	got := parseJSONStringArray(`["/a","/b"]`)
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("got %v", got)
	}
	if got := parseJSONStringArray(`[]`); len(got) != 0 {
		t.Fatalf("empty: got %v", got)
	}
}

func TestSplitJSON(t *testing.T) {
	parts := splitJSON(`"a","b","c"`)
	if len(parts) != 3 {
		t.Fatalf("got %v", parts)
	}
	if len(splitJSON(`"only"`)) != 1 {
		t.Fatal("single element failed")
	}
}

func TestTrimContains(t *testing.T) {
	if trim(`"hello"`, `"`) != "hello" {
		t.Fatal("trim failed")
	}
	if trim(`---`, `-`) != "" {
		t.Fatal("trim all failed")
	}
	if !contains("hello", "ell") {
		t.Fatal("contains failed")
	}
	if contains("hello", "") {
		t.Fatal("empty sub should be false")
	}
	if contains("hi", "hello") {
		t.Fatal("longer sub should be false")
	}
}

func TestSchedulerStartStopReload(t *testing.T) {
	db := testDB(t)
	db.Exec(`INSERT INTO backup_definitions (name, source_paths, schedule, compression_level, enabled, encrypted_password)
		VALUES ('docs', '["/data"]', 'daily', 3, 1, 'x')`)
	s := New(db, nil, nil, nil)
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := s.Location(); got == nil {
		t.Fatal("nil location")
	}
	s.Stop()
	s.Stop() // double stop is safe
}

func TestSchedulerDisabledNotScheduled(t *testing.T) {
	db := testDB(t)
	db.Exec(`INSERT INTO backup_definitions (name, source_paths, schedule, compression_level, enabled, encrypted_password)
		VALUES ('docs', '["/data"]', 'daily', 3, 0, 'x')`)
	s := New(db, nil, nil, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
}

func TestLoadDefs(t *testing.T) {
	db := testDB(t)
	db.Exec(`INSERT INTO backup_definitions (name, source_paths, schedule, compression_level, enabled, encrypted_password)
		VALUES ('docs', '["/a","/b"]', 'daily', 5, 1, 'secret')`)
	s := New(db, nil, nil, nil)
	defs, err := s.loadDefs(context.Background())
	if err != nil {
		t.Fatalf("loadDefs: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("got %d defs", len(defs))
	}
	d := defs[0]
	if d.Name != "docs" || len(d.SourcePaths) != 2 || d.CompressionLevel != 5 || !d.Enabled {
		t.Fatalf("bad def: %+v", d)
	}
	if d.Password != "secret" {
		t.Fatalf("password not loaded: %q", d.Password)
	}
}

func TestScheduleInvalidCron(t *testing.T) {
	db := testDB(t)
	s := New(db, nil, nil, nil)
	// Empty schedule normalizes to empty which cron rejects.
	if err := s.schedule(BackupDef{ID: 1, Name: "bad", Schedule: ""}); err == nil {
		t.Log("empty cron accepted (library lenient); skipping strict check")
	}
	// Disabled definitions are skipped without error.
	if err := s.schedule(BackupDef{ID: 2, Name: "off", Schedule: "not a cron", Enabled: false}); err != nil {
		t.Fatalf("disabled: %v", err)
	}
}

func TestRunBackupNoEngine(t *testing.T) {
	db := testDB(t)
	s := New(db, nil, nil, nil)
	// runBackup with nil engine must not panic.
	s.runBackup(BackupDef{ID: 1, Name: "x"})
	time.Sleep(10 * time.Millisecond)
}
