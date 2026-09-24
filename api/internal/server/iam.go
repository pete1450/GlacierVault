package server

import (
	"encoding/json"
	"net/http"

	"github.com/glaciervault/api/internal/provisioning"
)

// ── IAM permission repair ──────────────────────────────────────────────────
//
// The upstream CDK stack is append-only by design, so the rustic IAM user has
// no s3:DeleteObject rights and `rustic forget` / `rustic prune` fail with
// AccessDenied. New setups get a narrow inline policy automatically (unless
// read-only snapshots were chosen); these endpoints let any appliance grant
// or revoke it later with a one-time admin key. The submitted credentials
// are used for the single IAM call and never persisted.
//
// Whether the policy is currently attached is tracked in
// aws_config.snapshot_delete_granted — a record of what GlacierVault last
// did, not a live IAM query (the admin credentials used to change it are
// never retained).

func (s *Server) handleGrantSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.AccessKey == "" || body.SecretKey == "" {
		writeError(w, http.StatusBadRequest, "access key and secret key are required")
		return
	}
	var region, coldBucket, hotBucket, iamUser string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT region, cold_bucket, hot_bucket, iam_user FROM aws_config WHERE id=1`,
	).Scan(&region, &coldBucket, &hotBucket, &iamUser)
	if err != nil || coldBucket == "" || iamUser == "" {
		writeError(w, http.StatusBadRequest, "setup has not completed — no stack configured")
		return
	}

	if err := provisioning.EnsureSnapshotDeletePolicy(
		r.Context(), body.AccessKey, body.SecretKey, region, iamUser, coldBucket, hotBucket,
	); err != nil {
		writeError(w, http.StatusBadGateway, "could not grant permission: "+err.Error())
		return
	}
	s.DB.ExecContext(r.Context(), `UPDATE aws_config SET snapshot_delete_granted=1 WHERE id=1`)
	writeJSON(w, http.StatusOK, map[string]string{"status": "granted"})
}

func (s *Server) handleRevokeSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.AccessKey == "" || body.SecretKey == "" {
		writeError(w, http.StatusBadRequest, "access key and secret key are required")
		return
	}
	var region, iamUser string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT region, iam_user FROM aws_config WHERE id=1`,
	).Scan(&region, &iamUser)
	if err != nil || iamUser == "" {
		writeError(w, http.StatusBadRequest, "setup has not completed — no stack configured")
		return
	}

	if err := provisioning.RevokeSnapshotDeletePolicy(
		r.Context(), body.AccessKey, body.SecretKey, region, iamUser,
	); err != nil {
		writeError(w, http.StatusBadGateway, "could not revoke permission: "+err.Error())
		return
	}
	s.DB.ExecContext(r.Context(), `UPDATE aws_config SET snapshot_delete_granted=0 WHERE id=1`)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleSnapshotDeleteStatus(w http.ResponseWriter, r *http.Request) {
	var granted int
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT snapshot_delete_granted FROM aws_config WHERE id=1`,
	).Scan(&granted)
	if err != nil {
		writeError(w, http.StatusBadRequest, "setup has not completed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"granted": granted != 0})
}
