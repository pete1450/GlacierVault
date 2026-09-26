package server

import (
	"encoding/json"
	"net/http"

	"github.com/glaciervault/api/internal/restore"
)

// ── Restore warm-up tuning ───────────────────────────────────────────────────
//
// The conservative download rate (GB/day) sizes the download headroom in the
// per-batch restored-copy expiry calculation (see restore/warmup_plan.go and
// docs/workflow.md). Single-batch restores always use the 1-day minimum; the
// rate only matters once a restore spans more than one 1000-pack batch.

func (s *Server) handleGetRestoreTuning(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.RestoreMgr.GetRestoreTuning(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSaveRestoreTuning(w http.ResponseWriter, r *http.Request) {
	var cfg restore.RestoreTuning
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.RestoreMgr.SaveRestoreTuning(r.Context(), cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
