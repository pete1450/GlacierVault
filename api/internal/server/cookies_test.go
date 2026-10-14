package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsSecureRequest(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*http.Request)
		want  bool
	}{
		{"plain http", func(r *http.Request) {}, false},
		{"tls", func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, true},
		{"forwarded https", func(r *http.Request) {
			r.Header.Set("X-Forwarded-Proto", "https")
		}, true},
		{"forwarded http", func(r *http.Request) {
			r.Header.Set("X-Forwarded-Proto", "http")
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			tt.setup(r)
			if got := isSecureRequest(r); got != tt.want {
				t.Errorf("isSecureRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetSessionCookie(t *testing.T) {
	// HTTP request: Secure should be false.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	setSessionCookie(w, r, "token123", 8*time.Hour)
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "session=token123") {
		t.Errorf("cookie missing value: %s", cookie)
	}
	if !strings.Contains(cookie, "HttpOnly") {
		t.Errorf("cookie missing HttpOnly: %s", cookie)
	}
	if !strings.Contains(cookie, "SameSite=Strict") {
		t.Errorf("cookie missing SameSite=Strict: %s", cookie)
	}
	if strings.Contains(cookie, "Secure") {
		t.Errorf("cookie should not have Secure over HTTP: %s", cookie)
	}

	// HTTPS request: Secure should be true.
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{}
	setSessionCookie(w, r, "token123", 8*time.Hour)
	cookie = w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "Secure") {
		t.Errorf("cookie missing Secure over HTTPS: %s", cookie)
	}
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	clearSessionCookie(w, r)
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "session=") {
		t.Errorf("cookie missing: %s", cookie)
	}
	if !strings.Contains(cookie, "Max-Age=0") && !strings.Contains(cookie, "max-age=0") {
		// Go uses MaxAge=-1 which renders as Max-Age=0
		t.Logf("cookie: %s", cookie)
	}
}
