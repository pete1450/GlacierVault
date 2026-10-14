package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/glaciervault/api/internal/auth"
	"github.com/golang-jwt/jwt/v5"
)

// oidcStates stores pending OIDC login states for CSRF protection.
var oidcStates = struct {
	sync.Mutex
	m map[string]time.Time
}{m: make(map[string]time.Time)}

// handleAuthConfig returns the active auth method for the frontend.
func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	authType := s.AuthType
	if authType == "" {
		authType = "password"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"authType": authType,
	})
}

// handleVersion returns the build version. Public — no auth required.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	v := s.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"version": v,
	})
}

// handleOIDCLogin initiates the OIDC flow by redirecting to the IdP.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		writeError(w, http.StatusBadRequest, "oidc not configured")
		return
	}

	state, err := auth.GenerateState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	oidcStates.Lock()
	oidcStates.m[state] = time.Now()
	// Clean up old states.
	for k, v := range oidcStates.m {
		if time.Since(v) > auth.StateExpiry {
			delete(oidcStates.m, k)
		}
	}
	oidcStates.Unlock()

	// Store state in a short-lived cookie for verification on callback.
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.StateExpiry.Seconds()),
	})

	// Derive the redirect URL from the request — no static config needed.
	// Register this URL (scheme://host/api/auth/oidc/callback) in the IdP.
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	redirectURL := scheme + "://" + r.Host + "/api/auth/oidc/callback"

	http.Redirect(w, r, s.OIDC.AuthCodeURL(state, redirectURL), http.StatusFound)
}

// handleOIDCCallback handles the OIDC provider callback.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		writeError(w, http.StatusBadRequest, "oidc not configured")
		return
	}

	// Verify state.
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing state cookie")
		return
	}
	queryState := r.URL.Query().Get("state")
	if queryState == "" || queryState != stateCookie.Value {
		writeError(w, http.StatusBadRequest, "invalid state")
		return
	}

	oidcStates.Lock()
	_, ok := oidcStates.m[queryState]
	delete(oidcStates.m, queryState)
	oidcStates.Unlock()
	if !ok {
		writeError(w, http.StatusBadRequest, "expired or invalid state")
		return
	}

	// Clear the state cookie.
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", Value: "", MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}

	email, name, err := s.OIDC.Exchange(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	// Create a session JWT. The subject is the OIDC email.
	// Include the token epoch for session invalidation.
	var epoch int
	s.DB.QueryRowContext(r.Context(), `SELECT token_epoch FROM app_config WHERE id=1`).Scan(&epoch)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   email,
		"name":  name,
		"email": email,
		"epoch": epoch,
		"exp":   time.Now().Add(8 * time.Hour).Unix(),
	})
	signed, err := token.SignedString(s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token error")
		return
	}

	setSessionCookie(w, r, signed, 8*time.Hour)

	// Redirect to the app root.
	http.Redirect(w, r, "/", http.StatusFound)
}
