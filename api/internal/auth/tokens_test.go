package auth

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func testTokenDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := `
	CREATE TABLE api_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME,
		expires_at DATETIME,
		revoked INTEGER NOT NULL DEFAULT 0
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGenerateToken(t *testing.T) {
	tm := NewTokenManager(testTokenDB(t))
	plaintext, token, err := tm.GenerateToken("test-token", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if plaintext == "" {
		t.Fatal("empty plaintext")
	}
	if len(plaintext) < 10 || plaintext[:3] != "gv_" {
		t.Errorf("plaintext should start with gv_, got %s", plaintext[:10])
	}
	if token.Name != "test-token" {
		t.Errorf("name = %q", token.Name)
	}
	if token.ExpiresAt != nil {
		t.Error("should not expire when expiresIn=0")
	}
}

func TestGenerateTokenWithExpiry(t *testing.T) {
	tm := NewTokenManager(testTokenDB(t))
	_, token, err := tm.GenerateToken("expiring", 24*time.Hour)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if token.ExpiresAt == nil {
		t.Fatal("should have expiry")
	}
}

func TestValidateToken(t *testing.T) {
	tm := NewTokenManager(testTokenDB(t))
	plaintext, _, err := tm.GenerateToken("valid", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	token := tm.ValidateToken(plaintext)
	if token == nil {
		t.Fatal("valid token should validate")
	}
	if token.Name != "valid" {
		t.Errorf("name = %q", token.Name)
	}
	// Wrong token should not validate.
	if tm.ValidateToken("gv_wrongtoken123") != nil {
		t.Error("invalid token should not validate")
	}
}

func TestValidateExpiredToken(t *testing.T) {
	db := testTokenDB(t)
	tm := NewTokenManager(db)
	plaintext, _, err := tm.GenerateToken("expired", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Manually set expiry in the past.
	past := time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05")
	db.Exec(`UPDATE api_tokens SET expires_at=? WHERE name='expired'`, past)
	if tm.ValidateToken(plaintext) != nil {
		t.Error("expired token should not validate")
	}
}

func TestRevokeToken(t *testing.T) {
	tm := NewTokenManager(testTokenDB(t))
	plaintext, token, err := tm.GenerateToken("revoke-me", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := tm.RevokeToken(token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if tm.ValidateToken(plaintext) != nil {
		t.Error("revoked token should not validate")
	}
}

func TestListTokens(t *testing.T) {
	tm := NewTokenManager(testTokenDB(t))
	tm.GenerateToken("token1", 0)
	tm.GenerateToken("token2", 0)
	tokens, err := tm.ListTokens()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}
	// Revoke one.
	tm.RevokeToken(tokens[0].ID)
	tokens, _ = tm.ListTokens()
	if len(tokens) != 1 {
		t.Fatalf("after revoke: got %d tokens, want 1", len(tokens))
	}
}
