package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeaders(inner)

	// Plain HTTP request — no HSTS.
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	}
	for k, want := range headers {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}

	// HSTS must NOT be set on plain HTTP.
	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS set on plain HTTP: %q", hsts)
	}

	// CSP must be present and contain key directives.
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "base-uri 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q; got %q", want, csp)
		}
	}

	// Permissions-Policy should disable sensitive features.
	pp := rec.Header().Get("Permissions-Policy")
	if !strings.Contains(pp, "camera=()") {
		t.Errorf("Permissions-Policy too permissive: %q", pp)
	}
}

func TestSecurityHeadersHSTSOnHTTPS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeaders(inner)

	// Simulate HTTPS via X-Forwarded-Proto (behind a proxy).
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	hsts := rec.Header().Get("Strict-Transport-Security")
	if !strings.Contains(hsts, "max-age=31536000") {
		t.Errorf("HSTS not set on HTTPS: %q", hsts)
	}
	if !strings.Contains(hsts, "includeSubDomains") {
		t.Errorf("HSTS missing includeSubDomains: %q", hsts)
	}
}
