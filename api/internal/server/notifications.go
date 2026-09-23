package server

import (
	"encoding/json"
	"net/http"

	"github.com/glaciervault/api/internal/notify"
)

// ── Apprise notifications ────────────────────────────────────────────────────
//
// Destinations are apprise URLs (discord://, ntfy://, mailto://, … — any
// service apprise supports). The URLs contain secrets, so they live in the
// app database alongside the other deployment settings and are only
// exposed to authenticated sessions.

func (s *Server) handleGetNotificationConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.Notify.GetConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSaveNotificationConfig(w http.ResponseWriter, r *http.Request) {
	var cfg notify.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.Notify.SaveConfig(r.Context(), cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Destinations []string `json:"destinations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Synchronous: the user is waiting on this result to verify their URLs.
	if err := s.Notify.SendTest(r.Context(), body.Destinations); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
