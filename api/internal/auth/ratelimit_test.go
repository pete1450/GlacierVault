package auth

import (
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func testRateLimitDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := `
	CREATE TABLE login_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ip TEXT NOT NULL,
		attempted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		success INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX idx_login_attempts_ip_time ON login_attempts(ip, attempted_at);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRateLimiterAllowsInitially(t *testing.T) {
	rl := NewRateLimiter(testRateLimitDB(t))
	locked, _ := rl.IsLockedOut("1.2.3.4")
	if locked {
		t.Fatal("should not be locked out initially")
	}
}

func TestRateLimiterLocksAfterMaxFailures(t *testing.T) {
	rl := NewRateLimiter(testRateLimitDB(t))
	ip := "1.2.3.4"
	for i := 0; i < MaxFailedAttempts; i++ {
		rl.RecordAttempt(ip, false)
	}
	locked, remaining := rl.IsLockedOut(ip)
	if !locked {
		t.Fatal("should be locked out after max failures")
	}
	if remaining <= 0 || remaining > LockoutDuration {
		t.Errorf("remaining = %v, want (0, %v]", remaining, LockoutDuration)
	}
}

func TestRateLimiterSuccessDoesNotLock(t *testing.T) {
	rl := NewRateLimiter(testRateLimitDB(t))
	ip := "1.2.3.4"
	for i := 0; i < MaxFailedAttempts; i++ {
		rl.RecordAttempt(ip, true)
	}
	locked, _ := rl.IsLockedOut(ip)
	if locked {
		t.Fatal("successful attempts should not cause lockout")
	}
}

func TestRateLimiterIsolatesIPs(t *testing.T) {
	rl := NewRateLimiter(testRateLimitDB(t))
	for i := 0; i < MaxFailedAttempts; i++ {
		rl.RecordAttempt("1.2.3.4", false)
	}
	locked, _ := rl.IsLockedOut("5.6.7.8")
	if locked {
		t.Fatal("different IP should not be locked out")
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{"direct", "1.2.3.4:1234", nil, "1.2.3.4"},
		{"x-forwarded-for", "10.0.0.1:1234", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "1.2.3.4"},
		{"x-forwarded-for chain", "10.0.0.1:1234", map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"}, "1.2.3.4"},
		{"x-real-ip", "10.0.0.1:1234", map[string]string{"X-Real-IP": "1.2.3.4"}, "1.2.3.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimiterExpiry(t *testing.T) {
	db := testRateLimitDB(t)
	rl := NewRateLimiter(db)
	ip := "1.2.3.4"
	// Insert old failed attempts (outside the window).
	oldTime := time.Now().Add(-AttemptWindow - time.Hour).UTC().Format("2006-01-02 15:04:05")
	for i := 0; i < MaxFailedAttempts; i++ {
		db.Exec(`INSERT INTO login_attempts (ip, attempted_at, success) VALUES (?, ?, 0)`, ip, oldTime)
	}
	locked, _ := rl.IsLockedOut(ip)
	if locked {
		t.Fatal("old attempts should not cause lockout")
	}
}
