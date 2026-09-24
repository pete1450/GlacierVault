package cloudfront

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	appCrypto "github.com/glaciervault/api/internal/crypto"
)

// Manager owns the localhost S3→CloudFront proxy's lifecycle. The proxy is
// started at boot when the free-egress path is enabled and reloaded after
// (re)provisioning, without restarting the API process.
type Manager struct {
	mu           sync.Mutex
	db           *sql.DB
	addr         string
	server       *http.Server
	provisioning bool
	lastErr      string
}

// NewManager builds the proxy manager. The listen address defaults to
// DefaultAddr (loopback-only) and can be overridden with CF_PROXY_ADDR; a
// non-loopback override is refused.
func NewManager(db *sql.DB) *Manager {
	addr := os.Getenv("CF_PROXY_ADDR")
	if addr == "" {
		addr = DefaultAddr
	}
	return &Manager{db: db, addr: addr}
}

// Addr returns the proxy's listen address (host:port).
func (m *Manager) Addr() string { return m.addr }

// Ready reports whether the proxy is currently serving.
func (m *Manager) Ready() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.server != nil
}

// Reload (re)reads the CloudFront configuration from the database and
// (re)starts or stops the proxy to match. It is safe to call repeatedly.
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := Load(m.db)
	if err != nil {
		return fmt.Errorf("reload cloudfront proxy: %w", err)
	}
	if !cfg.Enabled {
		m.stopLocked()
		return nil
	}
	if cfg.Domain == "" || cfg.KeyPairID == "" {
		m.stopLocked()
		return fmt.Errorf("cloudfront enabled but not provisioned (missing domain/key id)")
	}
	priv, err := PrivateKey(m.db)
	if err != nil {
		m.stopLocked()
		return fmt.Errorf("reload cloudfront proxy: %w", err)
	}
	host, _, err := net.SplitHostPort(m.addr)
	if err != nil {
		return fmt.Errorf("invalid proxy address %q: %w", m.addr, err)
	}
	if !isLoopback(host) {
		return fmt.Errorf("refusing to bind cloudfront proxy to non-loopback address %q", m.addr)
	}

	// Cold bucket is needed so the proxy only serves that bucket's keys.
	// Region and credentials let the proxy pass requests it can't serve
	// via CloudFront (e.g. ListObjectsV2) through to real S3, signed
	// with SigV4.
	var coldBucket, region, encKey, encSecret string
	if err := m.db.QueryRow(`SELECT cold_bucket, region, encrypted_access_key, encrypted_secret_key FROM aws_config WHERE id=1`).Scan(&coldBucket, &region, &encKey, &encSecret); err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	if coldBucket == "" {
		return fmt.Errorf("no cold bucket configured")
	}
	accessKey, err := appCrypto.Decrypt(encKey)
	if err != nil {
		return fmt.Errorf("decrypt access key: %w", err)
	}
	secretKey, err := appCrypto.Decrypt(encSecret)
	if err != nil {
		return fmt.Errorf("decrypt secret key: %w", err)
	}

	m.stopLocked()
	proxy := NewProxy(cfg.BaseURL(), coldBucket, cfg.KeyPairID, priv, region, accessKey, secretKey)
	srv := &http.Server{
		Addr:              m.addr,
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
	}
	m.server = srv
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[cloudfront] proxy error: %v", err)
		}
	}()
	// Give the listener a moment to bind so Ready() is truthful right away.
	deadline := time.Now().Add(3 * time.Second)
	for !Reachable(m.addr) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	log.Printf("[cloudfront] proxy listening on %s (distribution %s)", m.addr, cfg.Domain)
	return nil
}

// stopLocked shuts down a running proxy. The caller must hold m.mu.
func (m *Manager) stopLocked() {
	if m.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.server.Shutdown(ctx)
	m.server = nil
}

// SetProvisioning records the state of a background provisioning run.
func (m *Manager) SetProvisioning(running bool, lastErr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provisioning = running
	m.lastErr = lastErr
}

// Provisioning reports whether a provisioning run is in flight and the last
// error, if any.
func (m *Manager) Provisioning() (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provisioning, m.lastErr
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
