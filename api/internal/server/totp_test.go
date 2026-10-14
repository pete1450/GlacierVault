package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestHandleTOTPStatus(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "test-password")
	// Initially disabled.
	rec := httptest.NewRecorder()
	s.handleTOTPStatus(rec, httptest.NewRequest("GET", "/api/auth/totp/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out map[string]bool
	json.NewDecoder(rec.Body).Decode(&out)
	if out["enabled"] {
		t.Error("should be disabled initially")
	}
}

func TestTOTPSetupAndVerify(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "test-password")

	// Setup: generate secret.
	rec := httptest.NewRecorder()
	s.handleTOTPSetup(rec, httptest.NewRequest("POST", "/api/auth/totp/setup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: code = %d", rec.Code)
	}
	var setupOut map[string]string
	json.NewDecoder(rec.Body).Decode(&setupOut)
	secret := setupOut["secret"]
	if secret == "" {
		t.Fatal("no secret returned")
	}
	if setupOut["url"] == "" {
		t.Fatal("no url returned")
	}

	// Verify with wrong code should fail.
	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"code":"000000"}`)
	s.handleTOTPVerifySetup(rec, httptest.NewRequest("POST", "/api/auth/totp/verify-setup", body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: code = %d, want 401", rec.Code)
	}

	// Verify with correct code.
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	rec = httptest.NewRecorder()
	body = strings.NewReader(`{"code":"` + code + `"}`)
	s.handleTOTPVerifySetup(rec, httptest.NewRequest("POST", "/api/auth/totp/verify-setup", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("correct code: code = %d", rec.Code)
	}
	var verifyOut map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&verifyOut)
	if verifyOut["status"] != "enabled" {
		t.Errorf("status = %v, want enabled", verifyOut["status"])
	}
	codes, ok := verifyOut["backupCodes"].([]interface{})
	if !ok || len(codes) != 10 {
		t.Errorf("backupCodes = %v, want 10 codes", verifyOut["backupCodes"])
	}

	// Status should now be enabled.
	rec = httptest.NewRecorder()
	s.handleTOTPStatus(rec, httptest.NewRequest("GET", "/api/auth/totp/status", nil))
	var statusOut map[string]bool
	json.NewDecoder(rec.Body).Decode(&statusOut)
	if !statusOut["enabled"] {
		t.Error("should be enabled after verify")
	}
}

func TestTOTPLoginFlow(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "test-password")

	// Enable TOTP.
	rec := httptest.NewRecorder()
	s.handleTOTPSetup(rec, httptest.NewRequest("POST", "/api/auth/totp/setup", nil))
	var setupOut map[string]string
	json.NewDecoder(rec.Body).Decode(&setupOut)
	code, _ := totp.GenerateCode(setupOut["secret"], time.Now())
	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"code":"` + code + `"}`)
	s.handleTOTPVerifySetup(rec, httptest.NewRequest("POST", "/api/auth/totp/verify-setup", body))

	// Login should return 202 with totp_required.
	rec = httptest.NewRecorder()
	body = strings.NewReader(`{"password":"test-password"}`)
	req := httptest.NewRequest("POST", "/api/auth/login", body)
	s.handleLogin(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("login with totp: code = %d, want 202", rec.Code)
	}
	var loginOut map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&loginOut)
	if loginOut["status"] != "totp_required" {
		t.Fatalf("status = %v, want totp_required", loginOut["status"])
	}
	pendingToken, ok := loginOut["pendingToken"].(string)
	if !ok || pendingToken == "" {
		t.Fatal("no pendingToken")
	}

	// Verify TOTP login with correct code.
	code, _ = totp.GenerateCode(setupOut["secret"], time.Now())
	rec = httptest.NewRecorder()
	body = strings.NewReader(`{"pendingToken":"` + pendingToken + `","code":"` + code + `"}`)
	s.handleTOTPVerifyLogin(rec, httptest.NewRequest("POST", "/api/auth/totp/verify-login", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("totp verify: code = %d", rec.Code)
	}
	// Should have session cookie.
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("no session cookie after totp verify")
	}

	// Wrong code should fail.
	rec = httptest.NewRecorder()
	body = strings.NewReader(`{"pendingToken":"` + pendingToken + `","code":"000000"}`)
	s.handleTOTPVerifyLogin(rec, httptest.NewRequest("POST", "/api/auth/totp/verify-login", body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: code = %d, want 401", rec.Code)
	}
}

func TestTOTPDisable(t *testing.T) {
	s := testServer(t)
	setPassword(t, s.DB, "test-password")

	// Enable TOTP.
	rec := httptest.NewRecorder()
	s.handleTOTPSetup(rec, httptest.NewRequest("POST", "/api/auth/totp/setup", nil))
	var setupOut map[string]string
	json.NewDecoder(rec.Body).Decode(&setupOut)
	code, _ := totp.GenerateCode(setupOut["secret"], time.Now())
	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"code":"` + code + `"}`)
	s.handleTOTPVerifySetup(rec, httptest.NewRequest("POST", "/api/auth/totp/verify-setup", body))

	// Disable with wrong password should fail.
	rec = httptest.NewRecorder()
	body = strings.NewReader(`{"password":"wrong"}`)
	s.handleTOTPDisable(rec, httptest.NewRequest("POST", "/api/auth/totp/disable", body))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: code = %d, want 401", rec.Code)
	}

	// Disable with correct password.
	rec = httptest.NewRecorder()
	body = strings.NewReader(`{"password":"test-password"}`)
	s.handleTOTPDisable(rec, httptest.NewRequest("POST", "/api/auth/totp/disable", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: code = %d", rec.Code)
	}

	// Status should be disabled.
	rec = httptest.NewRecorder()
	s.handleTOTPStatus(rec, httptest.NewRequest("GET", "/api/auth/totp/status", nil))
	var out map[string]bool
	json.NewDecoder(rec.Body).Decode(&out)
	if out["enabled"] {
		t.Error("should be disabled")
	}
}
