package catalog

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/glaciervault/api/internal/engine"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE snapshots (id INTEGER PRIMARY KEY AUTOINCREMENT, snapshot_id TEXT NOT NULL UNIQUE, file_count INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE file_index (id INTEGER PRIMARY KEY AUTOINCREMENT, snapshot_id INTEGER NOT NULL, path TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0, mtime DATETIME, is_dir INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// TestEnsureIndexingDedup verifies that concurrent first-opens of the same
// snapshot share one background job instead of indexing twice.
func TestEnsureIndexingDedup(t *testing.T) {
	c := New(testDB(t), engine.New("/nonexistent.toml"))
	p1 := c.EnsureIndexing(1, "abc123", 100)
	p2 := c.EnsureIndexing(1, "abc123", 100)
	if p1 != p2 {
		t.Fatal("expected the same progress handle for concurrent EnsureIndexing calls")
	}
	// The background job fails (no rustic binary here); wait for it and
	// verify the failure is reported and the entry is dropped for retry.
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, _, _, err, finished := p1.Snapshot()
		if finished {
			if err == nil {
				t.Fatal("expected an error with no rustic binary")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background job did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	p3 := c.EnsureIndexing(1, "abc123", 100)
	if p3 == p1 {
		t.Fatal("expected a fresh handle after the failed job was dropped")
	}
	// Wait for the retry's failure too, so no goroutine leaks into other tests.
	deadline = time.Now().Add(10 * time.Second)
	for {
		_, _, _, _, finished := p3.Snapshot()
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry job did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestIsIndexed verifies the indexed check used by the files handler.
func TestIsIndexed(t *testing.T) {
	db := testDB(t)
	c := New(db, engine.New("/nonexistent.toml"))
	indexed, err := c.IsIndexed(t.Context(), 7)
	if err != nil || indexed {
		t.Fatalf("expected not indexed, got indexed=%v err=%v", indexed, err)
	}
	if _, err := db.Exec(`INSERT INTO file_index (snapshot_id, path) VALUES (7, 'a/b')`); err != nil {
		t.Fatal(err)
	}
	indexed, err = c.IsIndexed(t.Context(), 7)
	if err != nil || !indexed {
		t.Fatalf("expected indexed, got indexed=%v err=%v", indexed, err)
	}
}
