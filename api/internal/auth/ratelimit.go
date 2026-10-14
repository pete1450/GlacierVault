package auth

import (
	"database/sql"
	"net"
	"net/http"
	"time"
)

// Rate limiting configuration.
const (
	MaxFailedAttempts = 5
	AttemptWindow     = 15 * time.Minute
	LockoutDuration   = 15 * time.Minute
)

// RateLimiter tracks failed login attempts per IP.
type RateLimiter struct {
	db *sql.DB
}

// NewRateLimiter creates a rate limiter backed by the database.
func NewRateLimiter(db *sql.DB) *RateLimiter {
	return &RateLimiter{db: db}
}

// clientIP extracts the real client IP, respecting X-Forwarded-For.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// Take the first (original client) IP.
		if idx := len(fwd); idx > 0 {
			for i, c := range fwd {
				if c == ',' {
					return normalizeIP(fwd[:i])
				}
			}
			return normalizeIP(fwd)
		}
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return normalizeIP(realIP)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return normalizeIP(r.RemoteAddr)
	}
	return normalizeIP(host)
}

func normalizeIP(ip string) string {
	ip = trimSpace(ip)
	if parsed := net.ParseIP(ip); parsed != nil {
		return parsed.String()
	}
	return ip
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// IsLockedOut checks if the IP is currently locked out.
func (rl *RateLimiter) IsLockedOut(ip string) (bool, time.Duration) {
	cutoff := time.Now().UTC().Add(-AttemptWindow)
	var count int
	err := rl.db.QueryRow(
		`SELECT COUNT(*) FROM login_attempts WHERE ip=? AND attempted_at>? AND success=0`,
		ip, cutoff.Format("2006-01-02 15:04:05"),
	).Scan(&count)
	if err != nil {
		return false, 0
	}
	if count < MaxFailedAttempts {
		return false, 0
	}
	// Find the most recent failed attempt to calculate remaining lockout.
	var lastAttempt string
	err = rl.db.QueryRow(
		`SELECT attempted_at FROM login_attempts WHERE ip=? AND success=0 ORDER BY attempted_at DESC LIMIT 1`,
		ip,
	).Scan(&lastAttempt)
	if err != nil {
		return true, LockoutDuration
	}
	lastTime, err := time.ParseInLocation("2006-01-02 15:04:05", lastAttempt, time.UTC)
	if err != nil {
		// Try RFC3339.
		lastTime, _ = time.Parse(time.RFC3339, lastAttempt)
	}
	remaining := LockoutDuration - time.Since(lastTime)
	if remaining < 0 {
		remaining = 0
	}
	return true, remaining
}

// RecordAttempt logs a login attempt.
func (rl *RateLimiter) RecordAttempt(ip string, success bool) {
	successInt := 0
	if success {
		successInt = 1
	}
	rl.db.Exec(
		`INSERT INTO login_attempts (ip, attempted_at, success) VALUES (?, ?, ?)`,
		ip, time.Now().UTC().Format("2006-01-02 15:04:05"), successInt,
	)
	// Clean up old attempts.
	cutoff := time.Now().Add(-24 * time.Hour)
	rl.db.Exec(`DELETE FROM login_attempts WHERE attempted_at < ?`,
		cutoff.Format("2006-01-02 15:04:05"))
}

// CheckRequest is a convenience method for HTTP handlers.
func (rl *RateLimiter) CheckRequest(r *http.Request) (bool, time.Duration) {
	return rl.IsLockedOut(clientIP(r))
}

// RecordRequest records an attempt for the request's IP.
func (rl *RateLimiter) RecordRequest(r *http.Request, success bool) {
	rl.RecordAttempt(clientIP(r), success)
}
