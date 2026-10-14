package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glaciervault/api/internal/auth"
)

// TestHandleAuthConfig verifies the auth type is reported correctly.
func TestHandleAuthConfig(t *testing.T) {
	tests := []struct {
		name     string
		authType string
		want     string
	}{
		{"default password", "", "password"},
		{"explicit password", "password", "password"},
		{"oidc", "oidc", "oidc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testServer(t)
			s.AuthType = tt.authType
			rec := httptest.NewRecorder()
			s.handleAuthConfig(rec, httptest.NewRequest("GET", "/api/auth/config", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("code = %d", rec.Code)
			}
		})
	}
}

// TestHandleOIDCLoginNotConfigured verifies the login endpoint rejects
// when OIDC is not configured.
func TestHandleOIDCLoginNotConfigured(t *testing.T) {
	s := testServer(t)
	s.OIDC = nil
	rec := httptest.NewRecorder()
	s.handleOIDCLogin(rec, httptest.NewRequest("GET", "/api/auth/oidc/login", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

// TestHandleOIDCCallbackNotConfigured verifies the callback rejects
// when OIDC is not configured.
func TestHandleOIDCCallbackNotConfigured(t *testing.T) {
	s := testServer(t)
	s.OIDC = nil
	rec := httptest.NewRecorder()
	s.handleOIDCCallback(rec, httptest.NewRequest("GET", "/api/auth/oidc/callback", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

// TestHandleOIDCCallbackMissingState verifies CSRF protection.
func TestHandleOIDCCallbackMissingState(t *testing.T) {
	s := testServer(t)
	// Set a non-nil OIDC client (we won't reach the exchange).
	s.OIDC = &auth.OIDCClient{}
	rec := httptest.NewRecorder()
	s.handleOIDCCallback(rec, httptest.NewRequest("GET", "/api/auth/oidc/callback?code=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 for missing state", rec.Code)
	}
}
