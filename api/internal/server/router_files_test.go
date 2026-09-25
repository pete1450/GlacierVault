package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/glaciervault/api/internal/catalog"
	"github.com/go-chi/chi/v5"
)

func getSnapshotFiles(t *testing.T, s *Server, id, prefix string) []map[string]any {
	t.Helper()
	u := "/api/snapshots/" + id + "/files"
	if prefix != "" {
		u += "?prefix=" + url.QueryEscape(prefix)
	}
	req := httptest.NewRequest(http.MethodGet, u, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.handleSnapshotFiles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func filePaths(entries []map[string]any) []string {
	var p []string
	for _, e := range entries {
		p = append(p, e["path"].(string))
	}
	return p
}

// A snapshot holding backuptest/testfile.tst must show only "backuptest/"
// at the root — the file must not leak into the parent listing.
func TestHandleSnapshotFilesCollapsesToImmediateChildren(t *testing.T) {
	db := openMigratedDB(t)
	if _, err := db.Exec(`INSERT INTO snapshots (snapshot_id, hostname, backup_time)
		VALUES ('snap1', 'host', '2026-09-24 00:00:00')`); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	indexRows := []struct {
		path  string
		isDir int
	}{
		{"/backuptest", 1},
		{"/backuptest/testfile.tst", 0},
		{"/backuptest/sub", 1},
		{"/backuptest/sub/deep.txt", 0},
		{"/loose.txt", 0},
	}
	for _, r := range indexRows {
		if _, err := db.Exec(`INSERT INTO file_index (snapshot_id, path, size, is_dir) VALUES (1, ?, 6, ?)`,
			r.path, r.isDir); err != nil {
			t.Fatalf("insert file: %v", err)
		}
	}
	// Catalog engine is nil but IndexSnapshot short-circuits on the
	// pre-populated file_index, so no engine call happens.
	s := &Server{DB: db, Catalog: catalog.New(db, nil)}

	root := filePaths(getSnapshotFiles(t, s, "1", ""))
	if want := []string{"/backuptest", "/loose.txt"}; !reflect.DeepEqual(root, want) {
		t.Fatalf("root = %v, want %v", root, want)
	}

	sub := filePaths(getSnapshotFiles(t, s, "1", "/backuptest/"))
	if want := []string{"/backuptest/sub", "/backuptest/testfile.tst"}; !reflect.DeepEqual(sub, want) {
		t.Fatalf("sub = %v, want %v", sub, want)
	}

	deep := filePaths(getSnapshotFiles(t, s, "1", "/backuptest/sub/"))
	if want := []string{"/backuptest/sub/deep.txt"}; !reflect.DeepEqual(deep, want) {
		t.Fatalf("deep = %v, want %v", deep, want)
	}
}
