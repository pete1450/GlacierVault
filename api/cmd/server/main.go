package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log"
	"net/http"
	"os"

	"golang.org/x/crypto/bcrypt"

	appCrypto "github.com/glaciervault/api/internal/crypto"
	"github.com/glaciervault/api/internal/catalog"
	"github.com/glaciervault/api/internal/db"
	"github.com/glaciervault/api/internal/engine"
	"github.com/glaciervault/api/internal/restore"
	"github.com/glaciervault/api/internal/scheduler"
	"github.com/glaciervault/api/internal/server"
)

func main() {
	configDir := env("CONFIG_DIR", "/config")
	databasePath := env("DATABASE_PATH", "/database/glaciervault.db")
	listenAddr := env("LISTEN_ADDR", ":8080")
	rusticConfigPath := configDir + "/rustic.toml"
	masterKeyPath := configDir + "/master.key"

	// Ensure config directory exists.
	if err := os.MkdirAll(configDir, 0700); err != nil {
		log.Fatalf("create config dir: %v", err)
	}

	// Load or generate the AES master key.
	if err := appCrypto.LoadOrCreateMasterKey(masterKeyPath); err != nil {
		log.Fatalf("master key: %v", err)
	}

	// Open database.
	database, err := db.Open(databasePath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	// Bootstrap app_config if first run.
	if err := bootstrapAppConfig(database, configDir); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	// Load SQS URL from DB (may be empty before setup).
	sqsURL := loadSQSURL(database)

	eng := engine.New(rusticConfigPath)
	cat := catalog.New(database, eng)
	restoreMgr := restore.New(database, eng, sqsURL)

	sched := scheduler.New(database, eng, cat)
	if err := sched.Start(context.Background()); err != nil {
		log.Printf("scheduler start: %v", err)
	}
	defer sched.Stop()

	jwtSecret := loadOrGenerateJWTSecret(configDir)

	srv := &server.Server{
		DB:         database,
		Engine:     eng,
		Catalog:    cat,
		Scheduler:  sched,
		RestoreMgr: restoreMgr,
		JWTSecret:  jwtSecret,
		ConfigPath: configDir,
	}

	log.Printf("GlacierVault listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, srv.Router()); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// bootstrapAppConfig creates the initial app_config row with a random
// password hash placeholder. The user sets their password through the UI.
func bootstrapAppConfig(database *sql.DB, configDir string) error {
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM app_config`).Scan(&count)
	if count > 0 {
		return nil
	}

	// Use a random initial password if INITIAL_PASSWORD is set; otherwise
	// require the user to set it through the setup wizard on first login.
	initialPassword := os.Getenv("INITIAL_PASSWORD")
	if initialPassword == "" {
		b := make([]byte, 16)
		rand.Read(b)
		initialPassword = hex.EncodeToString(b)
		log.Printf("⚠️  No INITIAL_PASSWORD set. Generated random: %s", initialPassword)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(initialPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = database.Exec(
		`INSERT INTO app_config (id, password_hash, master_key_path, setup_complete) VALUES (1, ?, ?, 0)`,
		string(hash), configDir+"/master.key",
	)
	return err
}

func loadOrGenerateJWTSecret(configDir string) []byte {
	path := configDir + "/jwt.key"
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return data
	}
	secret := make([]byte, 64)
	rand.Read(secret)
	os.WriteFile(path, secret, 0600)
	return secret
}

func loadSQSURL(database *sql.DB) string {
	var sqsURL sql.NullString
	database.QueryRow(`SELECT sqs_url FROM aws_config WHERE id=1`).Scan(&sqsURL)
	return sqsURL.String
}
