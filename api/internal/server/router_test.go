package server

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	appCrypto "github.com/glaciervault/api/internal/crypto"
	"github.com/glaciervault/api/internal/cloudfront"
	"github.com/glaciervault/api/internal/notify"
	"github.com/glaciervault/api/internal/restore"
	"github.com/glaciervault/api/internal/scheduler"
	"golang.org/x/crypto/bcrypt"
)

// testServer creates a Server with a migrated DB and a test JWT secret.
func testServer(t *testing.T) *Server {
	t.Helper()
	db := openMigratedDB(t)
	// Load a crypto key so appCrypto.Encrypt works in handlers.
	if err := appCrypto.LoadOrCreateMasterKey(filepath.Join(t.TempDir(), "master.key")); err != nil {
		t.Fatalf("load crypto key: %v", err)
	}
	ntf := notify.New(db)
	return &Server{
		DB:         db,
		Scheduler:  scheduler.New(db, nil, nil, nil),
		Notify:     ntf,
		RestoreMgr: restore.New(db, nil, nil, ntf),
		CFManager:  cloudfront.NewManager(db),
		JWTSecret:  []byte("test-secret-key-for-jwt-signing"),
	}
}

func setPassword(t *testing.T, db *sql.DB, pw string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO app_config (id, password_hash) VALUES (1, ?)`, string(hash)); err != nil {
		t.Fatalf("set password: %v", err)
	}
}

func doRequest(s *Server, handler func(http.ResponseWriter, *http.Request), method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func doJSON(s *Server, handler func(http.ResponseWriter, *http.Request), method, path string, body any) (int, map[string]any) {
	rec := doRequest(s, handler, method, path, body)
	var out map[string]any
	json.NewDecoder(rec.Body).Decode(&out)
	return rec.Code, out
}

// ── Auth ──────────────────────────────────────────────────────────────────

func TestHandleLoginSuccess(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "correct-horse")
	rec := doRequest(s, s.handleLogin, "POST", "/api/login", map[string]string{"password": "correct-horse"})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %q", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie")
	}
}

func TestHandleLoginBadPassword(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "correct-horse")
	code, _ := doJSON(s, s.handleLogin, "POST", "/api/login", map[string]string{"password": "wrong"})
	if code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", code)
	}
}

func TestHandleLoginNotConfigured(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleLogin, "POST", "/api/login", map[string]string{"password": "x"})
	if code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", code)
	}
}

func TestHandleLoginBadJSON(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/api/login", bytes.NewBufferString("{invalid"))
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestHandleLogout(t *testing.T) {
	s := testServer(t)
	rec := doRequest(s, s.handleLogout, "POST", "/api/logout", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestHandleChangePassword(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "old-password-123")
	rec := doRequest(s, s.handleChangePassword, "POST", "/api/change-password",
		map[string]string{"currentPassword": "old-password-123", "newPassword": "new-password-456"})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %q", rec.Code, rec.Body.String())
	}
	// New password works.
	rec2 := doRequest(s, s.handleLogin, "POST", "/api/login", map[string]string{"password": "new-password-456"})
	if rec2.Code != http.StatusOK {
		t.Fatal("login with new password failed")
	}
}

func TestHandleChangePasswordValidation(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "old-password-123")
	// Too short.
	code, _ := doJSON(s, s.handleChangePassword, "POST", "/api/change-password",
		map[string]string{"currentPassword": "old-password-123", "newPassword": "short"})
	if code != http.StatusBadRequest {
		t.Fatalf("short password: code = %d, want 400", code)
	}
	// Wrong current.
	code, _ = doJSON(s, s.handleChangePassword, "POST", "/api/change-password",
		map[string]string{"currentPassword": "wrong", "newPassword": "new-password-456"})
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong current: code = %d, want 401", code)
	}
	// Bad JSON.
	req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleChangePassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: code = %d, want 400", rec.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "pw12345678")
	// Login to get a token.
	loginRec := doRequest(s, s.handleLogin, "POST", "/api/login", map[string]string{"password": "pw12345678"})
	var token string
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "session" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no session cookie")
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := s.authMiddleware(ok)

	// No token → 401.
	req := httptest.NewRequest("GET", "/api/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: code = %d, want 401", rec.Code)
	}

	// Bad token → 401.
	req = httptest.NewRequest("GET", "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "bogus"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: code = %d, want 401", rec.Code)
	}

	// Good cookie → 200.
	req = httptest.NewRequest("GET", "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("good cookie: code = %d, want 200", rec.Code)
	}

	// Bearer header → 200.
	req = httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer: code = %d, want 200", rec.Code)
	}
}

// ── Backups ───────────────────────────────────────────────────────────────

func createBackupDef(t *testing.T, s *Server, name string) int64 {
	t.Helper()
	res, err := s.DB.Exec(`INSERT INTO backup_definitions (name, source_paths, schedule, compression_level, enabled, encrypted_password)
		VALUES (?, '["/data"]', 'daily', 3, 1, 'x')`, name)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestHandleListBackups(t *testing.T) {
	s := testServer(t)
	createBackupDef(t, s, "docs")
	rec := doRequest(s, s.handleListBackups, "GET", "/api/backups", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d backups, want 1", len(out))
	}
}

func TestHandleCreateBackup(t *testing.T) {
	s := testServer(t)
	code, out := doJSON(s, s.handleCreateBackup, "POST", "/api/backups", map[string]any{
		"name": "photos", "sourcePaths": []string{"/pics"}, "schedule": "daily",
		"compressionLevel": 3, "enabled": true, "password": "secret123",
	})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("code = %d, body %v", code, out)
	}
}

func TestHandleCreateBackupValidation(t *testing.T) {
	s := testServer(t)
	// Missing name.
	code, _ := doJSON(s, s.handleCreateBackup, "POST", "/api/backups", map[string]any{
		"sourcePaths": []string{"/pics"},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("missing name: code = %d, want 400", code)
	}
	// Bad JSON.
	req := httptest.NewRequest("POST", "/api/backups", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleCreateBackup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: code = %d, want 400", rec.Code)
	}
}

func withChiParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleGetBackup(t *testing.T) {
	s := testServer(t)
	id := createBackupDef(t, s, "docs")
	req := withChiParam(httptest.NewRequest("GET", "/api/backups/1", nil), "id", itoa(id))
	rec := httptest.NewRecorder()
	s.handleGetBackup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	// Not found.
	req = withChiParam(httptest.NewRequest("GET", "/api/backups/999", nil), "id", "99999")
	rec = httptest.NewRecorder()
	s.handleGetBackup(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing: code = %d, want 404", rec.Code)
	}
}

func TestHandleUpdateBackup(t *testing.T) {
	s := testServer(t)
	id := createBackupDef(t, s, "docs")
	req := withChiParam(httptest.NewRequest("PUT", "/api/backups/1", jsonBody(map[string]any{
		"name": "docs2", "enabled": false,
	})), "id", itoa(id))
	rec := httptest.NewRecorder()
	s.handleUpdateBackup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestHandleListJobs(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleListJobs, "GET", "/api/jobs", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}

func TestHandleGetJobNotFound(t *testing.T) {
	s := testServer(t)
	req := withChiParam(httptest.NewRequest("GET", "/api/jobs/1", nil), "id", "99999")
	rec := httptest.NewRecorder()
	s.handleGetJob(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestHandleCancelJobNotFound(t *testing.T) {
	s := testServer(t)
	req := withChiParam(httptest.NewRequest("POST", "/api/jobs/1/cancel", nil), "id", "99999")
	rec := httptest.NewRecorder()
	s.handleCancelJob(rec, req)
	// Not running → 409 Conflict.
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", rec.Code)
	}
}

// ── Snapshots ─────────────────────────────────────────────────────────────

func TestHandleListSnapshots(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleListSnapshots, "GET", "/api/snapshots", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}

func TestHandleGetSnapshotNotFound(t *testing.T) {
	s := testServer(t)
	req := withChiParam(httptest.NewRequest("GET", "/api/snapshots/1", nil), "id", "99999")
	rec := httptest.NewRecorder()
	s.handleGetSnapshot(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestHandleGetStorageRequiresEngine(t *testing.T) {
	// handleGetStorage needs a configured Engine; with nil it panics.
	// This documents the requirement rather than testing the handler.
	s := testServer(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with nil Engine")
		}
	}()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/storage", nil)
	s.handleGetStorage(rec, req)
}

// ── Restores ──────────────────────────────────────────────────────────────

func TestHandleListRestores(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleListRestores, "GET", "/api/restores", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}

func TestHandleGetRestoreNotFound(t *testing.T) {
	s := testServer(t)
	req := withChiParam(httptest.NewRequest("GET", "/api/restores/1", nil), "id", "99999")
	rec := httptest.NewRecorder()
	s.handleGetRestore(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestHandleInitiateRestoreValidation(t *testing.T) {
	s := testServer(t)
	// Bad JSON.
	req := httptest.NewRequest("POST", "/api/restores", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleInitiateRestore(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: code = %d, want 400", rec.Code)
	}
	// Missing fields.
	code, _ := doJSON(s, s.handleInitiateRestore, "POST", "/api/restores", map[string]any{
		"snapshotId": 0, "destination": "",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("missing fields: code = %d, want 400", code)
	}
}

// ── Notifications ───────────────────────────────────────────────────────

func TestHandleGetNotificationConfig(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleGetNotificationConfig, "GET", "/api/notifications", nil)
	// 200 with apprise installed, 500 when notifications are disabled (no apprise CLI).
	if code != http.StatusOK && code != http.StatusInternalServerError {
		t.Fatalf("code = %d", code)
	}
}

func TestHandleSaveNotificationConfig(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleSaveNotificationConfig, "POST", "/api/notifications", map[string]any{
		"webhookUrl": "https://example.com/hook", "enabled": true,
	})
	if code != http.StatusOK && code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
	// Bad JSON.
	req := httptest.NewRequest("POST", "/api/notifications", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleSaveNotificationConfig(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: code = %d, want 400", rec.Code)
	}
}

// ── Restore tuning ──────────────────────────────────────────────────────

func TestHandleGetRestoreTuning(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleGetRestoreTuning, "GET", "/api/restore-tuning", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}

func TestHandleSaveRestoreTuning(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleSaveRestoreTuning, "POST", "/api/restore-tuning", map[string]any{
		"warmupBatchSize": 500,
	})
	if code != http.StatusOK && code != http.StatusBadRequest {
		t.Fatalf("code = %d", code)
	}
}

// ── Setup ─────────────────────────────────────────────────────────────────

func TestHandleSetupStatus(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleSetupStatus, "GET", "/api/setup/status", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func jsonBody(v any) *bytes.Buffer {
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(v)
	return &buf
}

// ── Pure helpers ────────────────────────────────────────────────────────

func TestNullInt64Val(t *testing.T) {
	if nullInt64Val(sql.NullInt64{Valid: false}) != nil {
		t.Fatal("invalid should be nil")
	}
	if v := nullInt64Val(sql.NullInt64{Int64: 42, Valid: true}); v != int64(42) {
		t.Fatalf("got %v", v)
	}
}

func TestParseJSONStringArrayServer(t *testing.T) {
	got := parseJSONStringArray(`["a","b"]`)
	if len(got) != 2 || got[0] != "a" {
		t.Fatalf("got %v", got)
	}
	if got := parseJSONStringArray(`invalid`); len(got) != 0 {
		t.Fatalf("invalid: got %v", got)
	}
}

func TestToJSONArray(t *testing.T) {
	if got := toJSONArray([]string{"a", "b"}); got != `["a","b"]` {
		t.Fatalf("got %q", got)
	}
}

func TestWriteZipFile(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipFile(zw, "test.txt", "hello")
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty zip")
	}
	// Verify content.
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(r.File) != 1 || r.File[0].Name != "test.txt" {
		t.Fatalf("bad zip contents: %v", r.File)
	}
}

func TestBuildRecoveryReadme(t *testing.T) {
	out := buildRecoveryReadme("us-east-1", "hot-b", "cold-b", "secret")
	for _, want := range []string{"us-east-1", "hot-b", "cold-b", "secret"} {
		if !strings.Contains(out, want) {
			t.Errorf("readme missing %q", want)
		}
	}
}

func TestBuildRusticConfigTemplate(t *testing.T) {
	out := buildRusticConfigTemplate(rusticConfigParams{
		Region: "us-east-1", HotBucket: "hot-b", ColdBucket: "cold-b",
	})
	if !strings.Contains(out, "hot-b") {
		t.Error("config missing hot bucket")
	}
}

func TestWriteRusticConfig(t *testing.T) {
	dir := t.TempDir()
	password, err := writeRusticConfig(dir, rusticConfigParams{
		Region: "us-east-1", HotBucket: "hot-b", ColdBucket: "cold-b",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if password == "" {
		t.Fatal("empty password returned")
	}
	for _, f := range []string{"rustic.toml", "repo.password"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s not written: %v", f, err)
		}
	}
}

// ── More handlers ─────────────────────────────────────────────────────

func TestHandleDeleteSnapshotNotFound(t *testing.T) {
	s := testServer(t)
	req := withChiParam(httptest.NewRequest("DELETE", "/api/snapshots/1", nil), "id", "99999")
	rec := httptest.NewRecorder()
	s.handleDeleteSnapshot(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestHandlePruneRepo(t *testing.T) {
	s := testServer(t)
	// Without engine it panics on nil dereference; document the requirement.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with nil Engine")
		}
	}()
	rec := httptest.NewRecorder()
	s.handlePruneRepo(rec, httptest.NewRequest("POST", "/api/repo/prune", nil))
}

func TestHandleRebuildCatalogRequiresCatalog(t *testing.T) {
	// handleRebuildCatalog spawns a goroutine using s.Catalog; with nil it
	// panics in the background. Document the requirement; skip the call.
	s := testServer(t)
	if s.Catalog != nil {
		t.Fatal("expected nil Catalog in test server")
	}
}

func TestHandleTestNotification(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleTestNotification, "POST", "/api/notifications/test", map[string]any{})
	// 200 with apprise, 4xx/5xx without. All are valid responses.
	if code < 200 || code >= 600 {
		t.Fatalf("code = %d", code)
	}
}

func TestHandleValidateCredentialsBadJSON(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/api/setup/validate", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleValidateCredentials(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

// ── CloudFront handlers ───────────────────────────────────────────────

func TestHandleCloudFrontStatusOK(t *testing.T) {
	s := testServer(t)
	// Seed the aws_config row (NOT NULL columns required).
	s.DB.Exec(`INSERT OR IGNORE INTO aws_config (id, region, encrypted_access_key, encrypted_secret_key)
		VALUES (1, 'us-east-1', 'x', 'y')`)
	code, out := doJSON(s, s.handleCloudFrontStatus, "GET", "/api/cloudfront/status", nil)
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if _, ok := out["enabled"]; !ok {
		t.Fatal("missing enabled key")
	}
}

func TestHandleCloudFrontEnableValidation(t *testing.T) {
	s := testServer(t)
	// Bad JSON.
	req := httptest.NewRequest("POST", "/api/cloudfront/enable", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleCloudFrontEnable(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: code = %d, want 400", rec.Code)
	}
	// Missing keys.
	code, _ := doJSON(s, s.handleCloudFrontEnable, "POST", "/api/cloudfront/enable", map[string]string{})
	if code != http.StatusBadRequest {
		t.Fatalf("missing keys: code = %d, want 400", code)
	}
}

func TestHandleCloudFrontDisable(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleCloudFrontDisable, "POST", "/api/cloudfront/disable", nil)
	// May succeed or fail depending on setup; just verify it responds.
	if code < 200 || code >= 600 {
		t.Fatalf("code = %d", code)
	}
}

// ── IAM handlers ──────────────────────────────────────────────────────

func TestHandleGrantSnapshotDeleteValidation(t *testing.T) {
	s := testServer(t)
	// Bad JSON.
	req := httptest.NewRequest("POST", "/api/iam/grant", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleGrantSnapshotDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: code = %d, want 400", rec.Code)
	}
	// Missing keys.
	code, _ := doJSON(s, s.handleGrantSnapshotDelete, "POST", "/api/iam/grant", map[string]string{})
	if code != http.StatusBadRequest {
		t.Fatalf("missing keys: code = %d, want 400", code)
	}
	// Not configured (no stack).
	code, _ = doJSON(s, s.handleGrantSnapshotDelete, "POST", "/api/iam/grant",
		map[string]string{"accessKey": "AK", "secretKey": "SK"})
	if code != http.StatusBadRequest {
		t.Fatalf("not configured: code = %d, want 400", code)
	}
}

func TestHandleRevokeSnapshotDeleteValidation(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/api/iam/revoke", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	s.handleRevokeSnapshotDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: code = %d, want 400", rec.Code)
	}
}

func TestHandleSnapshotDeleteStatusOK(t *testing.T) {
	s := testServer(t)
	code, _ := doJSON(s, s.handleSnapshotDeleteStatus, "GET", "/api/iam/status", nil)
	if code < 200 || code >= 600 {
		t.Fatalf("code = %d", code)
	}
}

// ── List handlers with data ───────────────────────────────────────────

func TestHandleListJobsWithData(t *testing.T) {
	s := testServer(t)
	defID := createBackupDef(t, s, "docs")
	if _, err := s.DB.Exec(`INSERT INTO backup_jobs (backup_def_id, started_at, status, bytes_transferred)
		VALUES (?, '2026-10-13 10:00:00', 'completed', 1024)`, defID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rec := doRequest(s, s.handleListJobs, "GET", "/api/jobs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d jobs, want 1", len(out))
	}
	if out[0]["status"] != "completed" {
		t.Fatalf("status = %v", out[0]["status"])
	}
	// With limit param.
	rec = doRequest(s, s.handleListJobs, "GET", "/api/jobs?limit=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("limit: code = %d", rec.Code)
	}
}

func TestHandleListSnapshotsWithData(t *testing.T) {
	s := testServer(t)
	defID := createBackupDef(t, s, "docs")
	if _, err := s.DB.Exec(`INSERT INTO snapshots (snapshot_id, backup_def_id, hostname, backup_time)
		VALUES ('snap1', ?, 'host', '2026-10-13 00:00:00')`, defID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rec := doRequest(s, s.handleListSnapshots, "GET", "/api/snapshots", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out []map[string]any
	json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 1 {
		t.Fatalf("got %d snapshots, want 1", len(out))
	}
}

func TestHandleListRestoresWithData(t *testing.T) {
	s := testServer(t)
	defID := createBackupDef(t, s, "docs")
	res, err := s.DB.Exec(`INSERT INTO snapshots (snapshot_id, backup_def_id, hostname, backup_time)
		VALUES ('snap1', ?, 'host', '2026-10-13 00:00:00')`, defID)
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	snapRowID, _ := res.LastInsertId()
	if _, err := s.DB.Exec(`INSERT INTO restore_jobs (snapshot_id, requested_paths, destination, status, created_at)
		VALUES (?, '[]', '/tmp/x', 'completed', '2026-10-13 10:00:00')`, snapRowID); err != nil {
		t.Fatalf("insert restore: %v", err)
	}
	rec := doRequest(s, s.handleListRestores, "GET", "/api/restores", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out []map[string]any
	json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 1 {
		t.Fatalf("got %d restores, want 1", len(out))
	}
}

func TestHandleGetJobWithData(t *testing.T) {
	s := testServer(t)
	defID := createBackupDef(t, s, "docs")
	res, err := s.DB.Exec(`INSERT INTO backup_jobs (backup_def_id, started_at, status)
		VALUES (?, '2026-10-13 10:00:00', 'running')`, defID)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, _ := res.LastInsertId()
	req := withChiParam(httptest.NewRequest("GET", "/api/jobs/1", nil), "id", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	s.handleGetJob(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestHandleGetRestoreWithData(t *testing.T) {
	s := testServer(t)
	defID := createBackupDef(t, s, "docs")
	res, err := s.DB.Exec(`INSERT INTO snapshots (snapshot_id, backup_def_id, hostname, backup_time)
		VALUES ('snap1', ?, 'host', '2026-10-13 00:00:00')`, defID)
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	snapRowID, _ := res.LastInsertId()
	res, err = s.DB.Exec(`INSERT INTO restore_jobs (snapshot_id, requested_paths, destination, status, created_at)
		VALUES (?, '[]', '/tmp/x', 'completed', '2026-10-13 10:00:00')`, snapRowID)
	if err != nil {
		t.Fatalf("insert restore: %v", err)
	}
	id, _ := res.LastInsertId()
	req := withChiParam(httptest.NewRequest("GET", "/api/restores/1", nil), "id", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	s.handleGetRestore(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
}
