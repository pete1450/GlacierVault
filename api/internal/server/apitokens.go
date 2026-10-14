package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/glaciervault/api/internal/auth"
	"github.com/go-chi/chi/v5"
)

// handleListAPITokens returns all active API tokens.
func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	if s.TokenManager == nil {
		writeError(w, http.StatusInternalServerError, "token manager not configured")
		return
	}
	tokens, err := s.TokenManager.ListTokens()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}
	// Convert to JSON-friendly format.
	out := make([]map[string]interface{}, 0, len(tokens))
	for _, t := range tokens {
		m := map[string]interface{}{
			"id":        t.ID,
			"name":      t.Name,
			"createdAt": t.CreatedAt.Format(time.RFC3339),
		}
		if t.LastUsed != nil {
			m["lastUsedAt"] = t.LastUsed.Format(time.RFC3339)
		}
		if t.ExpiresAt != nil {
			m["expiresAt"] = t.ExpiresAt.Format(time.RFC3339)
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateAPIToken creates a new API token.
// The plaintext token is returned once; it cannot be retrieved again.
func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	if s.TokenManager == nil {
		writeError(w, http.StatusInternalServerError, "token manager not configured")
		return
	}
	var body struct {
		Name      string `json:"name"`
		ExpiresIn string `json:"expiresIn"` // e.g. "720h", empty = never
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	var expiresIn time.Duration
	if body.ExpiresIn != "" {
		var err error
		expiresIn, err = time.ParseDuration(body.ExpiresIn)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid expiresIn format")
			return
		}
	}
	plaintext, token, err := s.TokenManager.GenerateToken(body.Name, expiresIn)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":    token.ID,
		"name":  token.Name,
		"token": plaintext, // Show once!
	})
}

// handleRevokeAPIToken revokes an API token.
func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if s.TokenManager == nil {
		writeError(w, http.StatusInternalServerError, "token manager not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.TokenManager.RevokeToken(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// validateAPIToken checks if the request has a valid API token.
// Returns the token if valid, nil otherwise.
func (s *Server) validateAPIToken(r *http.Request) *auth.APIToken {
	if s.TokenManager == nil {
		return nil
	}
	h := r.Header.Get("Authorization")
	if len(h) < 7 || h[:7] != "Bearer " {
		return nil
	}
	tokenStr := h[7:]
	// API tokens start with gv_. JWTs don't.
	if len(tokenStr) < 3 || tokenStr[:3] != "gv_" {
		return nil
	}
	return s.TokenManager.ValidateToken(tokenStr)
}
