package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/glaciervault/api/internal/auth"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// handleTOTPStatus returns whether TOTP is enabled.
func (s *Server) handleTOTPStatus(w http.ResponseWriter, r *http.Request) {
	var enabled int
	s.DB.QueryRow(`SELECT totp_enabled FROM app_config WHERE id=1`).Scan(&enabled)
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled == 1})
}

// handleTOTPSetup generates a new TOTP secret and returns the QR code URL.
// The secret is stored but not enabled until verified.
func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	secret, url, err := auth.GenerateTOTPSecret("GlacierVault", "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	if _, err := s.DB.Exec(`UPDATE app_config SET totp_secret=? WHERE id=1`, secret); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": secret,
		"url":    url,
	})
}

// handleTOTPVerifySetup verifies the TOTP code and enables 2FA.
// Returns backup codes (shown once).
func (s *Server) handleTOTPVerifySetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		writeError(w, http.StatusBadRequest, "code required")
		return
	}

	var secret string
	var enabled int
	err := s.DB.QueryRow(`SELECT totp_secret, totp_enabled FROM app_config WHERE id=1`).Scan(&secret, &enabled)
	if err != nil || secret == "" {
		writeError(w, http.StatusBadRequest, "no totp setup in progress")
		return
	}
	if enabled == 1 {
		writeError(w, http.StatusBadRequest, "totp already enabled")
		return
	}

	if !auth.VerifyTOTP(secret, body.Code) {
		writeError(w, http.StatusUnauthorized, "invalid code")
		return
	}

	// Generate backup codes.
	codes, hashes, err := auth.GenerateBackupCodes(10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate backup codes")
		return
	}
	hashesJSON, _ := json.Marshal(hashes)

	if _, err := s.DB.Exec(`UPDATE app_config SET totp_enabled=1, totp_backup_codes=? WHERE id=1`,
		string(hashesJSON)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enable totp")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "enabled",
		"backupCodes": codes,
	})
}

// handleTOTPDisable disables TOTP. Requires password confirmation.
func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		writeError(w, http.StatusBadRequest, "password required")
		return
	}

	var hash string
	if err := s.DB.QueryRow(`SELECT password_hash FROM app_config WHERE id=1`).Scan(&hash); err != nil {
		writeError(w, http.StatusInternalServerError, "not configured")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	if _, err := s.DB.Exec(`UPDATE app_config SET totp_secret='', totp_enabled=0, totp_backup_codes='[]' WHERE id=1`); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// handleTOTPVerifyLogin completes a TOTP-challenged login.
// Expects a pending JWT (from handleLogin with totpRequired) and the TOTP code.
func (s *Server) handleTOTPVerifyLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PendingToken string `json:"pendingToken"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		writeError(w, http.StatusBadRequest, "pendingToken and code required")
		return
	}

	// Verify the pending token.
	token, err := jwt.Parse(body.PendingToken, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return s.JWTSecret, nil
	})
	if err != nil || !token.Valid {
		writeError(w, http.StatusUnauthorized, "invalid pending token")
		return
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["totp_pending"] != true {
		writeError(w, http.StatusUnauthorized, "not a totp pending token")
		return
	}

	var secret string
	var enabled int
	var backupCodesJSON string
	err = s.DB.QueryRow(`SELECT totp_secret, totp_enabled, totp_backup_codes FROM app_config WHERE id=1`).
		Scan(&secret, &enabled, &backupCodesJSON)
	if err != nil || enabled != 1 {
		writeError(w, http.StatusBadRequest, "totp not enabled")
		return
	}

	// Try TOTP code first.
	valid := auth.VerifyTOTP(secret, body.Code)
	// Try backup codes.
	if !valid {
		var hashes []string
		if json.Unmarshal([]byte(backupCodesJSON), &hashes) == nil {
			if idx := auth.VerifyBackupCode(body.Code, hashes); idx >= 0 {
				valid = true
				// Remove the used backup code.
				hashes = append(hashes[:idx], hashes[idx+1:]...)
				newJSON, _ := json.Marshal(hashes)
				s.DB.Exec(`UPDATE app_config SET totp_backup_codes=? WHERE id=1`, string(newJSON))
			}
		}
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid code")
		return
	}

	// Issue the full session.
	var epoch int
	s.DB.QueryRow(`SELECT token_epoch FROM app_config WHERE id=1`).Scan(&epoch)
	fullToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "admin",
		"epoch": epoch,
		"exp":   time.Now().Add(8 * time.Hour).Unix(),
	})
	signed, err := fullToken.SignedString(s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token error")
		return
	}
	setSessionCookie(w, r, signed, 8*time.Hour)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// totpEnabledForLogin checks if TOTP is enabled (for the login flow).
func (s *Server) totpEnabledForLogin() bool {
	var enabled int
	s.DB.QueryRow(`SELECT totp_enabled FROM app_config WHERE id=1`).Scan(&enabled)
	return enabled == 1
}

// issuePendingTOTPToken creates a short-lived token for the TOTP challenge step.
func (s *Server) issuePendingTOTPToken() (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":          "admin",
		"totp_pending": true,
		"exp":          time.Now().Add(5 * time.Minute).Unix(),
	})
	return token.SignedString(s.JWTSecret)
}
