package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/glaciervault/api/internal/cloudfront"
)

// ── CloudFront free-egress restore path ──────────────────────────────────────
//
// Restores download pack objects through a CloudFront distribution (signed
// URLs minted by a localhost-only proxy) so they consume CloudFront's monthly
// free data-transfer allowance instead of paid S3 egress. Provisioning needs
// admin credentials; they are used for the provisioning call only and are
// never persisted.

func (s *Server) handleCloudFrontStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := cloudfront.Load(s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load cloudfront config")
		return
	}
	provisioning, lastErr := s.CFManager.Provisioning()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":        cfg.Enabled,
		"domain":         cfg.Domain,
		"distributionId": cfg.DistributionID,
		"provisionedAt":  cfg.ProvisionedAt,
		"proxyReady":     s.CFManager.Ready(),
		"provisioning":   provisioning,
		"lastError":      lastErr,
	})
}

func (s *Server) handleCloudFrontEnable(w http.ResponseWriter, r *http.Request) {
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
	var region, coldBucket, stackName string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT region, cold_bucket, stack_name FROM aws_config WHERE id=1`,
	).Scan(&region, &coldBucket, &stackName)
	if err != nil || coldBucket == "" {
		writeError(w, http.StatusBadRequest, "setup has not completed — no cold bucket configured")
		return
	}
	if provisioning, _ := s.CFManager.Provisioning(); provisioning {
		writeError(w, http.StatusConflict, "a provisioning run is already in progress")
		return
	}

	// Provision in the background: creating and deploying a distribution
	// takes several minutes. The submitted credentials live only in this
	// goroutine's closure and are never written to disk or logs.
	go func() {
		ctx := context.Background()
		s.CFManager.SetProvisioning(true, "")

		info, keyPEM, err := cloudfront.Ensure(ctx, cloudfront.EnsureConfig{
			Region:     region,
			ColdBucket: coldBucket,
			StackName:  stackName,
			AccessKey:  body.AccessKey,
			SecretKey:  body.SecretKey,
			Log:        func(msg string) { /* surfaced via status polling */ },
		})
		if err != nil {
			s.CFManager.SetProvisioning(false, "provisioning failed: "+err.Error())
			return
		}
		if err := cloudfront.Save(s.DB, info, keyPEM); err != nil {
			s.CFManager.SetProvisioning(false, "saving config failed: "+err.Error())
			return
		}
		if err := s.CFManager.Reload(); err != nil {
			s.CFManager.SetProvisioning(false, "proxy did not start: "+err.Error())
			return
		}
		s.CFManager.SetProvisioning(false, "")
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "provisioning"})
}

func (s *Server) handleCloudFrontDisable(w http.ResponseWriter, r *http.Request) {
	if err := cloudfront.Disable(s.DB); err != nil {
		writeError(w, http.StatusInternalServerError, "could not disable")
		return
	}
	if err := s.CFManager.Reload(); err != nil {
		writeError(w, http.StatusInternalServerError, "disabled, but proxy did not stop: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}
