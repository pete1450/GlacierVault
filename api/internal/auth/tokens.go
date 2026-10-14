package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// APIToken represents a long-lived token for automation.
type APIToken struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	LastUsed  *time.Time
	ExpiresAt *time.Time
	Revoked   bool
}

// parseDBTime parses a datetime from SQLite (handles multiple formats).
func parseDBTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time: %s", s)
}

// TokenManager handles API token lifecycle.
type TokenManager struct {
	db *sql.DB
}

// NewTokenManager creates a token manager.
func NewTokenManager(db *sql.DB) *TokenManager {
	return &TokenManager{db: db}
}

// GenerateToken creates a new API token. Returns the plaintext token (show once)
// and the token metadata.
func (tm *TokenManager) GenerateToken(name string, expiresIn time.Duration) (plaintext string, token *APIToken, err error) {
	// Generate 32 random bytes (64 hex chars).
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	plaintext = "gv_" + hex.EncodeToString(b)

	// Store bcrypt hash (not plaintext).
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, err
	}

	var expiresAt *time.Time
	if expiresIn > 0 {
		t := time.Now().UTC().Add(expiresIn)
		expiresAt = &t
	}

	var expiresStr sql.NullString
	if expiresAt != nil {
		expiresStr = sql.NullString{String: expiresAt.Format("2006-01-02 15:04:05"), Valid: true}
	}

	res, err := tm.db.Exec(
		`INSERT INTO api_tokens (name, token_hash, expires_at) VALUES (?, ?, ?)`,
		name, string(hash), expiresStr,
	)
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()

	token = &APIToken{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}
	return plaintext, token, nil
}

// ValidateToken checks a plaintext token and returns the token metadata.
// Returns nil if invalid, expired, or revoked.
func (tm *TokenManager) ValidateToken(plaintext string) *APIToken {
	rows, err := tm.db.Query(
		`SELECT id, name, token_hash, created_at, last_used_at, expires_at, revoked FROM api_tokens WHERE revoked=0`,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var t APIToken
		var hash string
		var createdAt, lastUsedAt, expiresAt sql.NullString
		var revoked int
		if err := rows.Scan(&t.ID, &t.Name, &hash, &createdAt, &lastUsedAt, &expiresAt, &revoked); err != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) != nil {
			continue
		}
		// Check expiry.
		if expiresAt.Valid {
			exp, err := parseDBTime(expiresAt.String)
			if err == nil && time.Now().UTC().After(exp) {
				continue
			}
			t.ExpiresAt = &exp
		}
		if createdAt.Valid {
			if ct, err := parseDBTime(createdAt.String); err == nil {
				t.CreatedAt = ct
			}
		}
		// Update last_used.
		tm.db.Exec(`UPDATE api_tokens SET last_used_at=? WHERE id=?`,
			time.Now().UTC().Format("2006-01-02 15:04:05"), t.ID)
		return &t
	}
	return nil
}

// ListTokens returns all non-revoked tokens (without hashes).
func (tm *TokenManager) ListTokens() ([]*APIToken, error) {
	rows, err := tm.db.Query(
		`SELECT id, name, created_at, last_used_at, expires_at, revoked FROM api_tokens WHERE revoked=0 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*APIToken
	for rows.Next() {
		var t APIToken
		var createdAt, lastUsedAt, expiresAt sql.NullString
		var revoked int
		if err := rows.Scan(&t.ID, &t.Name, &createdAt, &lastUsedAt, &expiresAt, &revoked); err != nil {
			continue
		}
		if createdAt.Valid {
			if ct, err := parseDBTime(createdAt.String); err == nil {
				t.CreatedAt = ct
			}
		}
		if lastUsedAt.Valid {
			if lu, err := parseDBTime(lastUsedAt.String); err == nil {
				t.LastUsed = &lu
			}
		}
		if expiresAt.Valid {
			if exp, err := parseDBTime(expiresAt.String); err == nil {
				t.ExpiresAt = &exp
			}
		}
		tokens = append(tokens, &t)
	}
	return tokens, nil
}

// RevokeToken marks a token as revoked.
func (tm *TokenManager) RevokeToken(id int64) error {
	_, err := tm.db.Exec(`UPDATE api_tokens SET revoked=1 WHERE id=?`, id)
	return err
}
