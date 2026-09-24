package server

import (
	"archive/zip"
	"bytes"
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/glaciervault/api/internal/catalog"
	"github.com/glaciervault/api/internal/cloudfront"
	appCrypto "github.com/glaciervault/api/internal/crypto"
	"github.com/glaciervault/api/internal/engine"
	"github.com/glaciervault/api/internal/notify"
	"github.com/glaciervault/api/internal/provisioning"
	"github.com/glaciervault/api/internal/restore"
	"github.com/glaciervault/api/internal/scheduler"
)

const jwtSecret = "" // set via Server.JWTSecret

// Server holds all dependencies and the HTTP handler.
type Server struct {
	DB         *sql.DB
	Engine     *engine.Engine
	Catalog    *catalog.Catalog
	Scheduler  *scheduler.Scheduler
	RestoreMgr *restore.Manager
	CFManager  *cloudfront.Manager
	Notify     *notify.Manager
	JWTSecret  []byte
	ConfigPath string
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Public routes.
	r.Post("/api/auth/login", s.handleLogin)

	// Protected routes.
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Post("/api/auth/logout", s.handleLogout)
		r.Post("/api/auth/change-password", s.handleChangePassword)

		// Setup wizard.
		r.Post("/api/setup/validate", s.handleValidateCredentials)
		r.Post("/api/setup/deploy", s.handleDeploy)
		r.Get("/api/setup/status", s.handleSetupStatus)

		// Backup definitions.
		r.Get("/api/backups", s.handleListBackups)
		r.Post("/api/backups", s.handleCreateBackup)
		r.Get("/api/backups/{id}", s.handleGetBackup)
		r.Put("/api/backups/{id}", s.handleUpdateBackup)
		r.Delete("/api/backups/{id}", s.handleDeleteBackup)
		r.Post("/api/backups/{id}/run", s.handleRunBackupNow)

		// Jobs.
		r.Get("/api/jobs", s.handleListJobs)
		r.Get("/api/jobs/{id}", s.handleGetJob)
		r.Get("/api/jobs/{id}/stream", s.handleStreamJob)

		// Snapshots.
		r.Get("/api/snapshots", s.handleListSnapshots)
		r.Get("/api/snapshots/{id}", s.handleGetSnapshot)
		r.Get("/api/snapshots/{id}/files", s.handleSnapshotFiles)
		r.Delete("/api/snapshots/{id}", s.handleDeleteSnapshot)

		// Storage.
		r.Get("/api/storage", s.handleGetStorage)
		r.Post("/api/repo/prune", s.handlePruneRepo)

		// Restores.
		r.Post("/api/restores", s.handleInitiateRestore)
		r.Get("/api/restores", s.handleListRestores)
		r.Get("/api/restores/{id}", s.handleGetRestore)

		// Catalog.
		r.Post("/api/catalog/rebuild", s.handleRebuildCatalog)

		// CloudFront free-egress restore path.
		r.Get("/api/settings/cloudfront", s.handleCloudFrontStatus)
		r.Post("/api/settings/cloudfront/enable", s.handleCloudFrontEnable)
		r.Post("/api/settings/cloudfront/disable", s.handleCloudFrontDisable)

		// IAM permission repair (snapshot delete).
		r.Post("/api/settings/iam/snapshot-delete", s.handleGrantSnapshotDelete)
		r.Post("/api/settings/iam/snapshot-delete/revoke", s.handleRevokeSnapshotDelete)
		r.Get("/api/settings/iam/snapshot-delete", s.handleSnapshotDeleteStatus)

		// Notifications (apprise).
		r.Get("/api/notifications/config", s.handleGetNotificationConfig)
		r.Put("/api/notifications/config", s.handleSaveNotificationConfig)
		r.Post("/api/notifications/test", s.handleTestNotification)

		// Recovery package.
		r.Get("/api/recovery/package", s.handleRecoveryPackage)
	})

	// Serve Next.js static build.
	r.Handle("/*", http.FileServer(http.Dir("/app/frontend")))

	return r
}

// ── Auth ─────────────────────────────────────────────────────────────────────

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var hash string
	row := s.DB.QueryRowContext(r.Context(), `SELECT password_hash FROM app_config WHERE id = 1`)
	if err := row.Scan(&hash); err != nil {
		writeError(w, http.StatusUnauthorized, "not configured")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(8 * time.Hour).Unix(),
	})
	signed, err := token.SignedString(s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int((8 * time.Hour).Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", MaxAge: -1, Path: "/"})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleChangePassword verifies the current password and sets a new one.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if len(body.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	var hash string
	row := s.DB.QueryRowContext(r.Context(), `SELECT password_hash FROM app_config WHERE id = 1`)
	if err := row.Scan(&hash); err != nil {
		writeError(w, http.StatusUnauthorized, "not configured")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.CurrentPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash error")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE app_config SET password_hash=? WHERE id=1`, string(newHash)); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := ""
		if c, err := r.Cookie("session"); err == nil {
			tokenStr = c.Value
		}
		if tokenStr == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				tokenStr = h[7:]
			}
		}
		if tokenStr == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return s.JWTSecret, nil
		})
		if err != nil || !token.Valid {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Setup ─────────────────────────────────────────────────────────────────────

func (s *Server) handleValidateCredentials(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
		Region    string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AccessKey == "" || body.SecretKey == "" || body.Region == "" {
		writeError(w, http.StatusBadRequest, "accessKey, secretKey, region required")
		return
	}

	ok, identity, err := provisioning.ValidateCredentials(r.Context(), body.AccessKey, body.SecretKey, body.Region)
	if err != nil || !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid credentials: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":    true,
		"identity": identity,
		"estimate": provisioning.Estimate(),
	})
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessKey         string `json:"accessKey"`
		SecretKey         string `json:"secretKey"`
		Region            string `json:"region"`
		StackName         string `json:"stackName"`
		ReadonlySnapshots bool   `json:"readonlySnapshots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.StackName == "" {
		body.StackName = "rustic-cold-backups"
	}

	// Create a deploy job record.
	res, _ := s.DB.ExecContext(r.Context(),
		`INSERT INTO backup_jobs (backup_def_id, started_at, status) VALUES (NULL, ?, 'running')`,
		time.Now().UTC(),
	)
	jobID, _ := res.LastInsertId()

	encKey, _ := appCrypto.Encrypt(body.AccessKey)
	encSecret, _ := appCrypto.Encrypt(body.SecretKey)

	// Persist credentials immediately (deployment may take minutes).
	s.DB.ExecContext(r.Context(), `
		INSERT INTO aws_config (id, region, encrypted_access_key, encrypted_secret_key, stack_name)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			region=excluded.region,
			encrypted_access_key=excluded.encrypted_access_key,
			encrypted_secret_key=excluded.encrypted_secret_key,
			stack_name=excluded.stack_name`,
		body.Region, encKey, encSecret, body.StackName,
	)

	buf := engine.GetBuffer(jobID)

	go func() {
		ctx := context.Background()
		p := &provisioning.Provisioner{
			AccessKey: body.AccessKey,
			SecretKey: body.SecretKey,
			Region:    body.Region,
			StackName: body.StackName,
			LogFn:     buf.Write,
		}

		buf.Write("Bootstrapping CDK...")
		accountID, err := p.Bootstrap(ctx)
		if err != nil {
			buf.Write("[error] bootstrap: " + err.Error())
			s.DB.ExecContext(ctx, `UPDATE backup_jobs SET status='failed', error_message=?, completed_at=? WHERE id=?`,
				err.Error(), time.Now().UTC(), jobID)
			return
		}

		s.DB.ExecContext(ctx, `UPDATE aws_config SET account_id=? WHERE id=1`, accountID)

		buf.Write("Deploying stack...")
		outputs, err := p.Deploy(ctx)
		if err != nil {
			buf.Write("[error] deploy: " + err.Error())
			s.DB.ExecContext(ctx, `UPDATE backup_jobs SET status='failed', error_message=?, completed_at=? WHERE id=?`,
				err.Error(), time.Now().UTC(), jobID)
			return
		}

		// Create IAM access key for the deployed user.
		accessKey, secretKey, err := provisioning.CreateIAMAccessKey(ctx, body.AccessKey, body.SecretKey, body.Region, outputs.IAMUser)
		if err != nil {
			buf.Write("[error] could not create IAM key: " + err.Error())
			logText := strings.Join(buf.Lines(), "\n")
			s.DB.ExecContext(ctx, `UPDATE backup_jobs SET status='failed', error_message=?, log_output=?, completed_at=? WHERE id=?`,
				err.Error(), logText, time.Now().UTC(), jobID)
			return
		}
		encAK, _ := appCrypto.Encrypt(accessKey)
		encSK, _ := appCrypto.Encrypt(secretKey)
		s.DB.ExecContext(ctx, `UPDATE aws_config SET encrypted_access_key=?, encrypted_secret_key=? WHERE id=1`, encAK, encSK)

		// Grant the rustic user s3:DeleteObject on the buckets so snapshot
		// delete/prune works — unless the user chose read-only snapshots,
		// which keeps the upstream append-only posture. The upstream CDK
		// stack is append-only by design and omits it. Non-fatal: it can be
		// granted or revoked later from Settings with another set of
		// temporary admin credentials.
		if body.ReadonlySnapshots {
			buf.Write("Read-only snapshots selected — keeping append-only protection (snapshot delete will fail with AccessDenied).")
			s.DB.ExecContext(ctx, `UPDATE aws_config SET snapshot_delete_granted=0 WHERE id=1`)
		} else {
			buf.Write("Granting snapshot-delete permission to the backup user...")
			if err := provisioning.EnsureSnapshotDeletePolicy(ctx, body.AccessKey, body.SecretKey, body.Region, outputs.IAMUser, outputs.ColdBucket, outputs.HotBucket); err != nil {
				buf.Write("[warn] Could not grant snapshot-delete permission: " + err.Error())
				buf.Write("[warn] Snapshot deletion will fail until you grant it in Settings.")
				s.DB.ExecContext(ctx, `UPDATE aws_config SET snapshot_delete_granted=0 WHERE id=1`)
			} else {
				buf.Write("Snapshot-delete permission granted.")
				s.DB.ExecContext(ctx, `UPDATE aws_config SET snapshot_delete_granted=1 WHERE id=1`)
			}
		}

		// Wait for IAM access key to propagate before using it.
		buf.Write("Waiting for IAM credentials to propagate...")
		time.Sleep(15 * time.Second)

		s.DB.ExecContext(ctx, `
			UPDATE aws_config SET hot_bucket=?, cold_bucket=?, batch_manifests_bucket=?, batch_reports_bucket=?,
				sqs_url=?, iam_user=?, batch_role_arn=?, deployed_at=?
			WHERE id=1`,
			outputs.HotBucket, outputs.ColdBucket, outputs.BatchManifestsBucket, outputs.BatchReportsBucket,
			outputs.SQSUrl, outputs.IAMUser, outputs.BatchRoleArn, time.Now().UTC(),
		)

		// Provision the CloudFront free-egress path while the setup
		// credentials are still available. Non-fatal: if it fails, the
		// deployment still completes and the user can enable it later from
		// Settings with another set of temporary admin credentials.
		buf.Write("Provisioning CloudFront free-egress distribution...")
		cfInfo, cfKeyPEM, err := cloudfront.EnsureReady(ctx, s.DB, cloudfront.EnsureConfig{
			Region:     body.Region,
			ColdBucket: outputs.ColdBucket,
			StackName:  body.StackName,
			AccessKey:  body.AccessKey,
			SecretKey:  body.SecretKey,
			Log:        buf.Write,
		})
		if err != nil {
			buf.Write("[warn] CloudFront provisioning failed: " + err.Error())
			buf.Write("[warn] Restores will use paid S3 egress until you enable it in Settings.")
		} else {
			if err := cloudfront.Save(s.DB, cfInfo, cfKeyPEM); err != nil {
				buf.Write("[warn] Could not save CloudFront config: " + err.Error())
			} else if err := s.CFManager.Reload(); err != nil {
				buf.Write("[warn] CloudFront proxy did not start: " + err.Error())
			} else {
				buf.Write("CloudFront free-egress path ready: " + cfInfo.Domain)
			}
		}

		// Write rustic config and generate repo password.
		buf.Write("Writing Rustic config...")
		repoPass, err := writeRusticConfig(s.ConfigPath, rusticConfigParams{
			HotBucket:      outputs.HotBucket,
			ColdBucket:     outputs.ColdBucket,
			Region:         body.Region,
			IAMAccessKeyID: accessKey,
			IAMSecretKey:   secretKey,
		})
		if err != nil {
			buf.Write("[error] rustic config: " + err.Error())
			logText := strings.Join(buf.Lines(), "\n")
			s.DB.ExecContext(ctx, `UPDATE backup_jobs SET status='failed', error_message=?, log_output=?, completed_at=? WHERE id=?`,
				err.Error(), logText, time.Now().UTC(), jobID)
			return
		}
		encPass, _ := appCrypto.Encrypt(repoPass)
		s.DB.ExecContext(ctx, `UPDATE app_config SET encrypted_repo_password=? WHERE id=1`, encPass)

		// Initialise rustic repository in the cold+hot buckets.
		buf.Write("Initialising Rustic repository...")
		if err := s.Engine.InitRepository(ctx, buf); err != nil {
			buf.Write("[error] rustic init: " + err.Error())
			logText := strings.Join(buf.Lines(), "\n")
			s.DB.ExecContext(ctx, `UPDATE backup_jobs SET status='failed', error_message=?, log_output=?, completed_at=? WHERE id=?`,
				err.Error(), logText, time.Now().UTC(), jobID)
			return
		}

		s.DB.ExecContext(ctx, `UPDATE app_config SET setup_complete=1 WHERE id=1`)
		logText := strings.Join(buf.Lines(), "\n")
		s.DB.ExecContext(ctx, `UPDATE backup_jobs SET status='completed', log_output=?, completed_at=? WHERE id=?`,
			logText, time.Now().UTC(), jobID)
		buf.Write("Setup complete.")
	}()

	writeJSON(w, http.StatusAccepted, map[string]int64{"jobId": jobID})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	var cfg struct {
		Region     string `db:"region"`
		HotBucket  sql.NullString
		ColdBucket sql.NullString
		SQSUrl     sql.NullString
		DeployedAt sql.NullTime
		Complete   int
	}
	row := s.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(a.region,''), a.hot_bucket, a.cold_bucket, a.sqs_url, a.deployed_at, c.setup_complete
		FROM aws_config a, app_config c WHERE a.id=1 AND c.id=1`)
	row.Scan(&cfg.Region, &cfg.HotBucket, &cfg.ColdBucket, &cfg.SQSUrl, &cfg.DeployedAt, &cfg.Complete)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"setupComplete": cfg.Complete == 1,
		"region":        cfg.Region,
		"hotBucket":     cfg.HotBucket.String,
		"coldBucket":    cfg.ColdBucket.String,
		"sqsUrl":        cfg.SQSUrl.String,
		"deployedAt":    nullTimeStr(cfg.DeployedAt),
		"estimate":      provisioning.Estimate(),
	})
}

// ── Backup definitions ────────────────────────────────────────────────────────

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, name, source_paths, schedule, compression_level, enabled, created_at FROM backup_definitions ORDER BY created_at DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id int64
		var name, sourcePaths, schedule string
		var compressionLevel, enabled int
		var createdAt time.Time
		rows.Scan(&id, &name, &sourcePaths, &schedule, &compressionLevel, &enabled, &createdAt)
		results = append(results, map[string]interface{}{
			"id": id, "name": name, "sourcePaths": sourcePaths, "schedule": schedule,
			"compressionLevel": compressionLevel,
			"enabled": enabled == 1, "createdAt": createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name             string   `json:"name"`
		SourcePaths      []string `json:"sourcePaths"`
		Schedule         string   `json:"schedule"`
		CompressionLevel int      `json:"compressionLevel"`
		Password         string   `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || len(body.SourcePaths) == 0 {
		writeError(w, http.StatusBadRequest, "name, sourcePaths, schedule, password required")
		return
	}
	if body.CompressionLevel == 0 {
		body.CompressionLevel = 3
	}

	encPass, err := appCrypto.Encrypt(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encryption error")
		return
	}

	pathsJSON := toJSONArray(body.SourcePaths)
	cronExpr := scheduler.NormalizeCron(body.Schedule)

	res, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO backup_definitions (name, source_paths, schedule, compression_level, encrypted_password)
		VALUES (?, ?, ?, ?, ?)`,
		body.Name, pathsJSON, cronExpr, body.CompressionLevel, encPass,
	)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	id, _ := res.LastInsertId()

	// Reload scheduler.
	go s.Scheduler.Reload(context.Background())

	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleGetBackup(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := s.DB.QueryRowContext(r.Context(),
		`SELECT id, name, source_paths, schedule, compression_level, enabled, created_at FROM backup_definitions WHERE id=?`, id)
	var name, sourcePaths, schedule string
	var compressionLevel, enabled int
	var createdAt time.Time
	if err := row.Scan(&id, &name, &sourcePaths, &schedule, &compressionLevel, &enabled, &createdAt); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": id, "name": name, "sourcePaths": sourcePaths, "schedule": schedule,
		"compressionLevel": compressionLevel,
		"enabled": enabled == 1, "createdAt": createdAt,
	})
}

func (s *Server) handleUpdateBackup(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var body struct {
		Name             *string  `json:"name"`
		Schedule         *string  `json:"schedule"`
		SourcePaths      []string `json:"sourcePaths"`
		CompressionLevel *int     `json:"compressionLevel"`
		Enabled          *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if body.Name != nil && *body.Name != "" {
		s.DB.ExecContext(r.Context(), `UPDATE backup_definitions SET name=?, updated_at=? WHERE id=?`, *body.Name, time.Now().UTC(), id)
	}

	if body.Schedule != nil {
		expr := scheduler.NormalizeCron(*body.Schedule)
		s.DB.ExecContext(r.Context(), `UPDATE backup_definitions SET schedule=?, updated_at=? WHERE id=?`, expr, time.Now().UTC(), id)
	}
	if len(body.SourcePaths) > 0 {
		s.DB.ExecContext(r.Context(), `UPDATE backup_definitions SET source_paths=?, updated_at=? WHERE id=?`, toJSONArray(body.SourcePaths), time.Now().UTC(), id)
	}
	if body.CompressionLevel != nil {
		s.DB.ExecContext(r.Context(), `UPDATE backup_definitions SET compression_level=?, updated_at=? WHERE id=?`, *body.CompressionLevel, time.Now().UTC(), id)
	}
	if body.Enabled != nil {
		enabled := 0
		if *body.Enabled {
			enabled = 1
		}
		s.DB.ExecContext(r.Context(), `UPDATE backup_definitions SET enabled=?, updated_at=? WHERE id=?`, enabled, time.Now().UTC(), id)
	}
	go s.Scheduler.Reload(context.Background())
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// backup_jobs and snapshots reference backup_definitions with a
	// RESTRICT-style foreign key, so detach them first: history and repo
	// data stay intact, only the definition is removed.
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start transaction")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `UPDATE backup_jobs SET backup_def_id=NULL WHERE backup_def_id=?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not detach backup jobs")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE snapshots SET backup_def_id=NULL WHERE backup_def_id=?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not detach snapshots")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM backup_definitions WHERE id=?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete backup")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete backup")
		return
	}
	go s.Scheduler.Reload(context.Background())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunBackupNow(w http.ResponseWriter, r *http.Request) {
	defID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	// Fetch definition.
	row := s.DB.QueryRowContext(r.Context(),
		`SELECT name, source_paths, encrypted_password, compression_level FROM backup_definitions WHERE id=?`, defID)
	var name, sourcePaths, encPass string
	var compressionLevel int
	if err := row.Scan(&name, &sourcePaths, &encPass, &compressionLevel); err != nil {
		writeError(w, http.StatusNotFound, "backup not found")
		return
	}

	res, _ := s.DB.ExecContext(r.Context(),
		`INSERT INTO backup_jobs (backup_def_id, started_at, status) VALUES (?, ?, 'running')`,
		defID, time.Now().UTC(),
	)
	jobID, _ := res.LastInsertId()

	buf := engine.GetBuffer(jobID)
	paths := parseJSONStringArray(sourcePaths)

	go func() {
		ctx := context.Background()
		err := s.Engine.RunBackup(ctx, buf, paths, []string{name}, compressionLevel)
		status, errMsg := "completed", ""
		if err != nil {
			status, errMsg = "failed", err.Error()
		}
		logText := strings.Join(buf.Lines(), "\n")
		s.DB.ExecContext(ctx, `UPDATE backup_jobs SET status=?, completed_at=?, error_message=?, log_output=? WHERE id=?`,
			status, time.Now().UTC(), errMsg, logText, jobID)
		if err == nil {
			if s.Notify != nil {
				s.Notify.BackupCompleted(name)
			}
			if syncErr := s.Catalog.SyncAfterBackup(ctx, defID); syncErr != nil {
				// Surface catalog sync failures on the job record — a silent
				// failure here is what made snapshots never appear in the UI.
				msg := fmt.Sprintf("catalog sync failed: %v", syncErr)
				buf.Write("[error] " + msg)
				logText = strings.Join(buf.Lines(), "\n")
				s.DB.ExecContext(ctx, `UPDATE backup_jobs SET error_message=?, log_output=? WHERE id=?`,
					msg, logText, jobID)
			}
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]int64{"jobId": jobID})
}

// ── Jobs ──────────────────────────────────────────────────────────────────────

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, backup_def_id, started_at, completed_at, status, bytes_transferred, error_message FROM backup_jobs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var jobs []map[string]interface{}
	for rows.Next() {
		var id, bytesTransferred int64
		var backupDefID sql.NullInt64
		var startedAt time.Time
		var completedAt sql.NullTime
		var status string
		var errMsg sql.NullString
		rows.Scan(&id, &backupDefID, &startedAt, &completedAt, &status, &bytesTransferred, &errMsg)
		jobs = append(jobs, map[string]interface{}{
			"id": id, "backupDefId": backupDefID.Int64, "startedAt": startedAt,
			"completedAt": nullTimeStr(completedAt), "status": status,
			"bytesTransferred": bytesTransferred, "errorMessage": errMsg.String,
		})
	}
	if jobs == nil {
		jobs = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := s.DB.QueryRowContext(r.Context(),
		`SELECT id, backup_def_id, started_at, completed_at, status, bytes_transferred, error_message, log_output FROM backup_jobs WHERE id=?`, id)
	var jid, bytes int64
	var defID sql.NullInt64
	var startedAt time.Time
	var completedAt sql.NullTime
	var status string
	var errMsg, logOutput sql.NullString
	if err := row.Scan(&jid, &defID, &startedAt, &completedAt, &status, &bytes, &errMsg, &logOutput); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": jid, "backupDefId": defID.Int64, "startedAt": startedAt,
		"completedAt": nullTimeStr(completedAt), "status": status,
		"bytesTransferred": bytes, "errorMessage": errMsg.String, "logOutput": logOutput.String,
	})
}

// handleStreamJob streams live log lines as Server-Sent Events.
func (s *Server) handleStreamJob(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	buf := engine.GetBuffer(id)
	sent := 0
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			lines := buf.Lines()
			for ; sent < len(lines); sent++ {
				fmt.Fprintf(w, "data: %s\n\n", lines[sent])
			}
			flusher.Flush()

			// Check if job is done.
			var status string
			row := s.DB.QueryRowContext(r.Context(), `SELECT status FROM backup_jobs WHERE id=?`, id)
			row.Scan(&status)
			if status == "completed" || status == "failed" {
				fmt.Fprintf(w, "event: done\ndata: %s\n\n", status)
				flusher.Flush()
				return
			}
		}
	}
}

// ── Snapshots ─────────────────────────────────────────────────────────────────

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, snapshot_id, backup_def_id, hostname, tags, total_size, file_count, backup_time FROM snapshots ORDER BY backup_time DESC LIMIT 200`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var snaps []map[string]interface{}
	for rows.Next() {
		var id int64
		var snapshotID, hostname, tags string
		var defID sql.NullInt64
		var totalSize, fileCount int64
		var backupTime time.Time
		rows.Scan(&id, &snapshotID, &defID, &hostname, &tags, &totalSize, &fileCount, &backupTime)
		snaps = append(snaps, map[string]interface{}{
			"id": id, "snapshotId": snapshotID, "backupDefId": defID.Int64,
			"hostname": hostname, "tags": tags, "totalSize": totalSize,
			"fileCount": fileCount, "backupTime": backupTime,
		})
	}
	if snaps == nil {
		snaps = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := s.DB.QueryRowContext(r.Context(),
		`SELECT id, snapshot_id, backup_def_id, hostname, tags, total_size, file_count, backup_time FROM snapshots WHERE id=?`, id)
	var sid int64
	var snapshotID, hostname, tags string
	var defID sql.NullInt64
	var totalSize, fileCount int64
	var backupTime time.Time
	if err := row.Scan(&sid, &snapshotID, &defID, &hostname, &tags, &totalSize, &fileCount, &backupTime); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": sid, "snapshotId": snapshotID, "backupDefId": defID.Int64,
		"hostname": hostname, "tags": tags, "totalSize": totalSize,
		"fileCount": fileCount, "backupTime": backupTime,
	})
}

func (s *Server) handleSnapshotFiles(w http.ResponseWriter, r *http.Request) {
	snapshotRowID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	prefix := r.URL.Query().Get("prefix")

	// Lazy index if not yet populated.
	var rusticID string
	s.DB.QueryRowContext(r.Context(), `SELECT snapshot_id FROM snapshots WHERE id=?`, snapshotRowID).Scan(&rusticID)
	if rusticID != "" {
		if err := s.Catalog.IndexSnapshot(r.Context(), snapshotRowID, rusticID); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("index snapshot: %v", err))
			return
		}
	}

	query := `SELECT path, size, mtime, is_dir FROM file_index WHERE snapshot_id=?`
	args := []interface{}{snapshotRowID}
	if prefix != "" {
		query += ` AND path LIKE ?`
		args = append(args, prefix+"%")
	}
	query += ` ORDER BY is_dir DESC, path LIMIT 1000`

	rows, err := s.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var files []map[string]interface{}
	for rows.Next() {
		var path, mtime string
		var size int64
		var isDir int
		rows.Scan(&path, &size, &mtime, &isDir)
		files = append(files, map[string]interface{}{
			"path": path, "size": size, "mtime": mtime, "isDir": isDir == 1,
		})
	}
	if files == nil {
		files = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, files)
}

// handleDeleteSnapshot removes a snapshot from the repository via
// `rustic forget` and drops its catalog rows. With ?prune=true, the forget
// also runs prune to reclaim unreferenced space.
func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	rowID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var rusticID string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT snapshot_id FROM snapshots WHERE id=?`, rowID).Scan(&rusticID); err != nil {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}

	prune := r.URL.Query().Get("prune") == "true"
	alreadyGone := false
	if rusticID != "" {
		if err := s.Engine.ForgetSnapshot(r.Context(), rusticID, prune); err != nil {
			if errors.Is(err, engine.ErrSnapshotAlreadyGone) {
				// The snapshot file was already gone from the repository
				// (both backends); the catalog entry is stale. Clean it up
				// and report success.
				alreadyGone = true
			} else {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("forget snapshot: %v", err))
				return
			}
		}
	}

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM file_index WHERE snapshot_id=?`, rowID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM snapshots WHERE id=?`, rowID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": true, "pruned": prune, "alreadyGone": alreadyGone})
}

// handleGetStorage reports repository storage usage from `rustic repoinfo`
// plus logical snapshot totals from the catalog.
func (s *Server) handleGetStorage(w http.ResponseWriter, r *http.Request) {
	info, err := s.Engine.GetRepoInfo(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("repo info: %v", err))
		return
	}
	var snapshotCount int64
	var logicalBytes int64
	row := s.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(total_size),0) FROM snapshots`)
	_ = row.Scan(&snapshotCount, &logicalBytes)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"totalBytes":    info.TotalBytes,
		"packBytes":     info.PackBytes,
		"indexBytes":    info.IndexBytes,
		"repoSnapshots": info.SnapshotCount,
		"snapshotCount": snapshotCount,
		"logicalBytes":  logicalBytes,
	})
}

// handlePruneRepo runs `rustic prune` to remove unreferenced data and repack
// pack files, reclaiming space freed by forgotten snapshots.
func (s *Server) handlePruneRepo(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.PruneRepo(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("prune: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"pruned": true})
}

// ── Restores ──────────────────────────────────────────────────────────────────

func (s *Server) handleInitiateRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SnapshotID  int64    `json:"snapshotId"`
		Paths       []string `json:"paths"`
		Destination string   `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SnapshotID == 0 || body.Destination == "" {
		writeError(w, http.StatusBadRequest, "snapshotId and destination required")
		return
	}

	jobID, err := s.RestoreMgr.Initiate(r.Context(), body.SnapshotID, body.Paths, body.Destination)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"jobId": jobID})
}

func (s *Server) handleListRestores(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, snapshot_id, destination, status, created_at, completed_at FROM restore_jobs ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var jobs []map[string]interface{}
	for rows.Next() {
		var id int64
		var snapshotID sql.NullInt64
		var destination, status string
		var createdAt time.Time
		var completedAt sql.NullTime
		rows.Scan(&id, &snapshotID, &destination, &status, &createdAt, &completedAt)
		jobs = append(jobs, map[string]interface{}{
			"id": id, "snapshotId": nullInt64Val(snapshotID), "destination": destination,
			"status": status, "createdAt": createdAt, "completedAt": nullTimeStr(completedAt),
		})
	}
	if jobs == nil {
		jobs = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleGetRestore(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := s.DB.QueryRowContext(r.Context(),
		`SELECT id, snapshot_id, requested_paths, destination, status, warmup_status, retrieval_started_at, restore_started_at, completed_at, error_message, created_at, batch_job_id FROM restore_jobs WHERE id=?`, id)
	var rid int64
	var snapshotID sql.NullInt64
	var requestedPaths, destination, status string
	var warmupStatus, errMsg, batchJobID sql.NullString
	var retrievalStarted, restoreStarted, completedAt, createdAt sql.NullTime
	if err := row.Scan(&rid, &snapshotID, &requestedPaths, &destination, &status, &warmupStatus,
		&retrievalStarted, &restoreStarted, &completedAt, &errMsg, &createdAt, &batchJobID); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": rid, "snapshotId": nullInt64Val(snapshotID), "requestedPaths": requestedPaths,
		"destination": destination, "status": status, "warmupStatus": warmupStatus.String,
		"retrievalStartedAt": nullTimeStr(retrievalStarted), "restoreStartedAt": nullTimeStr(restoreStarted),
		"completedAt": nullTimeStr(completedAt), "errorMessage": errMsg.String,
		"batchJobId": batchJobID.String,
		"logOutput":  strings.Join(engine.GetBuffer(id).Lines(), "\n"),
		"createdAt":  createdAt.Time,
	})
}

// ── Catalog ───────────────────────────────────────────────────────────────────

func (s *Server) handleRebuildCatalog(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := s.Catalog.RebuildCatalog(context.Background()); err != nil {
			log.Printf("catalog rebuild: %v", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "rebuilding"})
}

// ── Recovery package ──────────────────────────────────────────────────────────

func (s *Server) handleRecoveryPackage(w http.ResponseWriter, r *http.Request) {
	var cfg struct {
		Region               string
		HotBucket            sql.NullString
		ColdBucket           sql.NullString
		StackName            string
		EncRepoPassword      sql.NullString
		EncAccessKey         sql.NullString
		EncSecretKey         sql.NullString
		AccountID            sql.NullString
		BatchManifestsBucket sql.NullString
		BatchReportsBucket   sql.NullString
		BatchRoleArn         sql.NullString
		SQSUrl               sql.NullString
	}
	s.DB.QueryRowContext(r.Context(),
		`SELECT region, hot_bucket, cold_bucket, stack_name, encrypted_access_key, encrypted_secret_key,
		        account_id, batch_manifests_bucket, batch_reports_bucket, batch_role_arn, sqs_url
		 FROM aws_config WHERE id=1`,
	).Scan(&cfg.Region, &cfg.HotBucket, &cfg.ColdBucket, &cfg.StackName, &cfg.EncAccessKey, &cfg.EncSecretKey,
		&cfg.AccountID, &cfg.BatchManifestsBucket, &cfg.BatchReportsBucket, &cfg.BatchRoleArn, &cfg.SQSUrl)
	s.DB.QueryRowContext(r.Context(),
		`SELECT encrypted_repo_password FROM app_config WHERE id=1`,
	).Scan(&cfg.EncRepoPassword)

	// Decrypt IAM credentials for the rustic config template.
	iamKey, _ := appCrypto.Decrypt(cfg.EncAccessKey.String)
	iamSecret, _ := appCrypto.Decrypt(cfg.EncSecretKey.String)
	repoPass, _ := appCrypto.Decrypt(cfg.EncRepoPassword.String)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	writeZipFile(zw, "README.txt", buildRecoveryReadme(cfg.Region, cfg.HotBucket.String, cfg.ColdBucket.String, repoPass))
	writeZipFile(zw, "rustic.toml", buildRusticConfigTemplate(rusticConfigParams{
		HotBucket:      cfg.HotBucket.String,
		ColdBucket:     cfg.ColdBucket.String,
		Region:         cfg.Region,
		IAMAccessKeyID: iamKey,
		IAMSecretKey:   iamSecret,
	}))
	writeZipFile(zw, "repo.password", repoPass)
	writeZipFile(zw, "warmup-s3-archives-config.toml", fmt.Sprintf(`# GlacierVault warmup tool config. Place in the working directory
# from which you run the restore command below.
[aws_resources]
account_id = %q
cold_bucket_name = %q
batch_manifests_bucket_name = %q
batch_reports_bucket_name = %q
batch_role_arn = %q
restore_queue_url = %q

[initiate_restore_object]
expiration_in_days = 7
glacier_job_tier = "BULK"
`,
		cfg.AccountID.String, cfg.ColdBucket.String, cfg.BatchManifestsBucket.String,
		cfg.BatchReportsBucket.String, cfg.BatchRoleArn.String, cfg.SQSUrl.String))

	zw.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="glaciervault-recovery.zip"`)
	w.Write(buf.Bytes())
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func nullTimeStr(t sql.NullTime) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(time.RFC3339)
	return &s
}

// nullInt64Val renders a nullable integer for JSON: the value, or nil.
func nullInt64Val(v sql.NullInt64) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func toJSONArray(paths []string) string {
	b, _ := json.Marshal(paths)
	return string(b)
}

func parseJSONStringArray(s string) []string {
	var result []string
	json.Unmarshal([]byte(s), &result)
	return result
}

func writeZipFile(zw *zip.Writer, name, content string) {
	f, err := zw.Create(name)
	if err == nil {
		f.Write([]byte(content))
	}
}

func buildRecoveryReadme(region, hotBucket, coldBucket, repoPassword string) string {
	return fmt.Sprintf(`GlacierVault Recovery Package
==============================

AWS Region:       %s
Hot Bucket:       %s
Cold Bucket:      %s
Repository Password: %s

To recover data without GlacierVault:

1. Install Rustic: https://rustic.cli.rs
2. Install warmup-s3-archives: https://gitlab.com/philipmw/warmup-s3-archives
3. Unzip this package and cd into its directory. It contains:
     rustic.toml                       (repository config, credentials included)
     repo.password                     (repository password)
     warmup-s3-archives-config.toml    (Glacier warmup tool config)
4. Export your AWS credentials (same values as in rustic.toml):
     export AWS_ACCESS_KEY_ID=...
     export AWS_SECRET_ACCESS_KEY=...
     export AWS_REGION=%s
5. List snapshots to find the one you want:
     rustic -P ./rustic snapshots
6. Restore (destination is positional). Rustic computes the exact pack set
   the snapshot needs and calls warmup-s3-archives with those S3 keys
   (%%paths); the tool submits an S3 Batch restore at the cheapest BULK
   tier and waits for Glacier to finish, then rustic downloads:
     rustic -P ./rustic restore <snapshot-id> /destination \
       --warm-up-command "warmup-s3-archives %%paths" --warm-up-batch 1000
   Bulk retrieval from Deep Archive can take up to ~48 hours; the command
   blocks until the packs are available and then restores automatically.

IMPORTANT: Keep this package secure. It contains credentials to your backup storage.
`, region, hotBucket, coldBucket, repoPassword, region)
}

type rusticConfigParams struct {
	HotBucket      string
	ColdBucket     string
	Region         string
	IAMAccessKeyID string
	IAMSecretKey   string
}

func buildRusticConfigTemplate(p rusticConfigParams) string {
	return fmt.Sprintf(`[repository]
repository = "opendal:s3"
repo-hot = "opendal:s3"
password-file = "/config/repo.password"

[repository.options]
access_key_id = "%s"
secret_access_key = "%s"
region = "%s"

[repository.options-hot]
bucket = "%s"

[repository.options-cold]
bucket = "%s"
default_storage_class = "DEEP_ARCHIVE"
`, p.IAMAccessKeyID, p.IAMSecretKey, p.Region, p.HotBucket, p.ColdBucket)
}

// writeRusticConfig writes rustic.toml and generates /config/repo.password.
// Returns the plaintext repo password so the caller can store it.
func writeRusticConfig(configDir string, p rusticConfigParams) (string, error) {
	content := buildRusticConfigTemplate(p)
	if err := os.WriteFile(configDir+"/rustic.toml", []byte(content), 0600); err != nil {
		return "", fmt.Errorf("write rustic.toml: %w", err)
	}

	// Generate a random 32-byte repo password and write to the password file.
	b := make([]byte, 32)
	if _, err := crand.Read(b); err != nil {
		return "", fmt.Errorf("generate repo password: %w", err)
	}
	password := hex.EncodeToString(b)
	if err := os.WriteFile(configDir+"/repo.password", []byte(password), 0600); err != nil {
		return "", fmt.Errorf("write repo.password: %w", err)
	}
	return password, nil
}
