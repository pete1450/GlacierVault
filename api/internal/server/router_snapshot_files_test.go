package server

import (
	"database/sql"
	"fmt"
	"testing"
)

// Reproduces the reported bug: 33 top-level folders + files, but the old
// listing query (ORDER BY path LIMIT 5000 before collapsing to children)
// only showed the folders whose files sort alphabetically first.
func insertTestSnapshot(t *testing.T, db *sql.DB, id int, rusticID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO snapshots (id, snapshot_id, hostname, backup_time) VALUES (?, ?, 'testhost', '2026-10-02 00:00:00')`,
		id, rusticID); err != nil {
		t.Fatal(err)
	}
}

func TestListSnapshotChildrenShowsAllTopLevel(t *testing.T) {
	db := openMigratedDB(t)
	insertTestSnapshot(t, db, 1, "rustic-snap-1")

	// 33 top-level dirs with 200 files each = 6600 files, plus dir rows
	// (the JSON-lines `rustic ls` format includes directory entries).
	const snapID = 1
	for d := 0; d < 33; d++ {
		dir := fmt.Sprintf("dir%02d", d)
		if _, err := db.Exec(`INSERT INTO file_index (snapshot_id, path, is_dir) VALUES (?, ?, 1)`, snapID, dir); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < 200; f++ {
			if _, err := db.Exec(`INSERT INTO file_index (snapshot_id, path, size, is_dir) VALUES (?, ?, 10, 0)`,
				snapID, fmt.Sprintf("%s/file%03d.jpg", dir, f)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A nested subfolder, and top-level files sorting first/middle/last.
	for _, q := range []string{
		`INSERT INTO file_index (snapshot_id, path, size) VALUES (1, 'afile.txt', 1)`,
		`INSERT INTO file_index (snapshot_id, path, size) VALUES (1, 'mfile.txt', 2)`,
		`INSERT INTO file_index (snapshot_id, path, size) VALUES (1, 'zfile.txt', 3)`,
		`INSERT INTO file_index (snapshot_id, path, is_dir) VALUES (1, 'dir32/sub', 1)`,
		`INSERT INTO file_index (snapshot_id, path, size) VALUES (1, 'dir32/sub/deep.txt', 4)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	children, _, err := listSnapshotChildren(t.Context(), db, snapID, "", 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 33 dirs + 3 files.
	if len(children) != 36 {
		t.Fatalf("expected 36 top-level children, got %d", len(children))
	}
	byPath := map[string]snapshotChild{}
	for _, c := range children {
		byPath[c.path] = c
	}
	// The alphabetically-last entries must be present (the old query hid them).
	for _, want := range []string{"dir32", "zfile.txt", "dir00", "afile.txt"} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("missing top-level child %q", want)
		}
	}
	if !byPath["dir32"].isDir {
		t.Error("dir32 should be a directory")
	}
	if byPath["zfile.txt"].isDir {
		t.Error("zfile.txt should be a file")
	}
	if byPath["zfile.txt"].size != 3 {
		t.Errorf("zfile.txt size = %d, want 3", byPath["zfile.txt"].size)
	}
	// Directories first, then alphabetical.
	if !children[0].isDir || children[33].isDir || children[33].path != "afile.txt" {
		t.Errorf("unexpected ordering: first=%+v 34th=%+v", children[0], children[33])
	}

	// Sub-prefix: dir32/ shows its 200 files + the sub dir, not the files inside sub.
	sub, _, err := listSnapshotChildren(t.Context(), db, snapID, "dir32/", 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 201 {
		t.Fatalf("expected 201 children of dir32/, got %d", len(sub))
	}
	foundSub := false
	for _, c := range sub {
		if c.path == "dir32/sub" {
			foundSub = true
			if !c.isDir {
				t.Error("dir32/sub should be a directory")
			}
		}
		if c.path == "dir32/sub/deep.txt" {
			t.Error("deep.txt should be collapsed under dir32/sub")
		}
	}
	if !foundSub {
		t.Error("dir32/sub missing from dir32/ listing")
	}
}

// Format 1 (`rustic ls` single-array output) has no directory rows: dirs must
// be synthesized from deeper paths.
func TestListSnapshotChildrenSynthesizesDirs(t *testing.T) {
	db := openMigratedDB(t)
	insertTestSnapshot(t, db, 2, "rustic-snap-2")
	for _, q := range []string{
		`INSERT INTO file_index (snapshot_id, path, size) VALUES (2, 'photos/2020/img1.jpg', 5)`,
		`INSERT INTO file_index (snapshot_id, path, size) VALUES (2, 'photos/2021/img2.jpg', 6)`,
		`INSERT INTO file_index (snapshot_id, path, size) VALUES (2, 'notes.txt', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	children, _, err := listSnapshotChildren(t.Context(), db, 2, "", 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %v", children)
	}
	if children[0].path != "photos" || !children[0].isDir {
		t.Errorf("expected synthesized dir photos first, got %+v", children[0])
	}
	if children[1].path != "notes.txt" || children[1].isDir {
		t.Errorf("expected file notes.txt second, got %+v", children[1])
	}
}

// One flat folder with 2500 files: pagination slices it into pages and the
// total stays constant across pages.
func TestListSnapshotChildrenPagination(t *testing.T) {
	db := openMigratedDB(t)
	insertTestSnapshot(t, db, 3, "rustic-snap-3")
	if _, err := db.Exec(`INSERT INTO file_index (snapshot_id, path, is_dir) VALUES (3, 'flat', 1)`); err != nil {
		t.Fatal(err)
	}
	for f := 0; f < 2500; f++ {
		if _, err := db.Exec(`INSERT INTO file_index (snapshot_id, path, size) VALUES (3, ?, 7)`,
			fmt.Sprintf("flat/file%04d.bin", f)); err != nil {
			t.Fatal(err)
		}
	}

	page1, total, err := listSnapshotChildren(t.Context(), db, 3, "flat/", 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 1000 || total != 2500 {
		t.Fatalf("page 1: got %d rows total %d, want 1000 rows total 2500", len(page1), total)
	}
	if page1[0].path != "flat/file0000.bin" || page1[999].path != "flat/file0999.bin" {
		t.Errorf("page 1 boundaries wrong: first=%q last=%q", page1[0].path, page1[999].path)
	}

	page3, total3, err := listSnapshotChildren(t.Context(), db, 3, "flat/", 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 500 || total3 != 2500 {
		t.Fatalf("page 3: got %d rows total %d, want 500 rows total 2500", len(page3), total3)
	}
	if page3[0].path != "flat/file2000.bin" || page3[499].path != "flat/file2499.bin" {
		t.Errorf("page 3 boundaries wrong: first=%q last=%q", page3[0].path, page3[499].path)
	}

	// Oversized limits are clamped to 5000.
	clamped, _, err := listSnapshotChildren(t.Context(), db, 3, "flat/", 100000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(clamped) != 2500 {
		t.Fatalf("clamped large limit: got %d rows, want 2500", len(clamped))
	}
}
