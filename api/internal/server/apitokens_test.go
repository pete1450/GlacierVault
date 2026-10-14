package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCreateAndListAPITokens(t *testing.T) {
	s := testServer(t)

	// Create a token.
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"test-token"}`)
	s.handleCreateAPIToken(rec, httptest.NewRequest("POST", "/api/auth/tokens", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: code = %d", rec.Code)
	}
	var createOut map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&createOut)
	tokenStr, ok := createOut["token"].(string)
	if !ok || tokenStr == "" {
		t.Fatal("no token returned")
	}
	if len(tokenStr) < 10 || tokenStr[:3] != "gv_" {
		t.Errorf("token should start with gv_, got %s", tokenStr[:10])
	}

	// List tokens.
	rec = httptest.NewRecorder()
	s.handleListAPITokens(rec, httptest.NewRequest("GET", "/api/auth/tokens", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code = %d", rec.Code)
	}
	var listOut []map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&listOut)
	if len(listOut) != 1 {
		t.Fatalf("got %d tokens, want 1", len(listOut))
	}
	if listOut[0]["name"] != "test-token" {
		t.Errorf("name = %v", listOut[0]["name"])
	}
	// Token plaintext should NOT be in list output.
	if _, hasToken := listOut[0]["token"]; hasToken {
		t.Error("list should not include token plaintext")
	}
}

func TestAPITokenAuth(t *testing.T) {
	s := testServer(t)

	// Create a token.
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"auth-test"}`)
	s.handleCreateAPIToken(rec, httptest.NewRequest("POST", "/api/auth/tokens", body))
	var createOut map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&createOut)
	tokenStr := createOut["token"].(string)

	// Use the token to access a protected endpoint.
	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/auth/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: code = %d, want 200", rec.Code)
	}

	// Invalid token should fail.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/auth/tokens", nil)
	req.Header.Set("Authorization", "Bearer gv_invalid123")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: code = %d, want 401", rec.Code)
	}
}

func TestHandleRevokeAPIToken(t *testing.T) {
	s := testServer(t)

	// Create a token.
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"revoke-me"}`)
	s.handleCreateAPIToken(rec, httptest.NewRequest("POST", "/api/auth/tokens", body))
	var createOut map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&createOut)
	tokenStr := createOut["token"].(string)
	id := int64(createOut["id"].(float64))

	// Revoke via the manager (handler needs chi context; tested via integration).
	if err := s.TokenManager.RevokeToken(id); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Token should no longer work.
	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/auth/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: code = %d, want 401", rec.Code)
	}
}

func TestHandleCreateAPITokenValidation(t *testing.T) {
	s := testServer(t)
	// Empty name should fail.
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"name":""}`)
	s.handleCreateAPIToken(rec, httptest.NewRequest("POST", "/api/auth/tokens", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name: code = %d, want 400", rec.Code)
	}
	// Invalid expiry should fail.
	rec = httptest.NewRecorder()
	body = strings.NewReader(`{"name":"test","expiresIn":"invalid"}`)
	s.handleCreateAPIToken(rec, httptest.NewRequest("POST", "/api/auth/tokens", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid expiry: code = %d, want 400", rec.Code)
	}
}
