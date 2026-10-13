package cloudfront

import (
	"database/sql"
	"path/filepath"
	"testing"

	appCrypto "github.com/glaciervault/api/internal/crypto"
	_ "github.com/mattn/go-sqlite3"
)

func testCFDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", t.TempDir()+"/test.db?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := `
	CREATE TABLE aws_config (
		id INTEGER PRIMARY KEY,
		cf_enabled INTEGER DEFAULT 0,
		cf_distribution_id TEXT DEFAULT '',
		cf_domain TEXT DEFAULT '',
		cf_key_pair_id TEXT DEFAULT '',
		cf_key_group_id TEXT DEFAULT '',
		cf_oac_id TEXT DEFAULT '',
		cf_cache_policy_id TEXT DEFAULT '',
		cf_private_key_enc TEXT DEFAULT '',
		cf_provisioned_at TEXT DEFAULT ''
	);
	INSERT INTO aws_config (id) VALUES (1);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if err := appCrypto.LoadOrCreateMasterKey(filepath.Join(t.TempDir(), "master.key")); err != nil {
		t.Fatalf("crypto: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLoadEmpty(t *testing.T) {
	db := testCFDB(t)
	cfg, err := Load(db)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("should not be enabled")
	}
}

func TestSaveAndLoad(t *testing.T) {
	db := testCFDB(t)
	info := &Info{
		DistributionID: "E12345",
		Domain:         "d123.cloudfront.net",
		KeyPairID:      "KP123",
		KeyGroupID:     "KG123",
		OACID:          "OAC123",
		CachePolicyID:  "CP123",
	}
	if err := Save(db, info, "fake-pem-key"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg, err := Load(db)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Enabled || cfg.Domain != "d123.cloudfront.net" || cfg.KeyPairID != "KP123" {
		t.Fatalf("bad config: %+v", cfg)
	}
	if cfg.BaseURL() != "https://d123.cloudfront.net" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL())
	}
}

func TestSaveRequiresDomain(t *testing.T) {
	db := testCFDB(t)
	err := Save(db, &Info{DistributionID: "E123"}, "")
	if err == nil {
		t.Fatal("expected error without domain")
	}
}

func TestDisable(t *testing.T) {
	db := testCFDB(t)
	info := &Info{DistributionID: "E1", Domain: "d1.cloudfront.net", KeyPairID: "K1"}
	if err := Save(db, info, "pem"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Disable(db); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	cfg, _ := Load(db)
	if cfg.Enabled {
		t.Fatal("should be disabled")
	}
}

func TestPrivateKeyNoKey(t *testing.T) {
	db := testCFDB(t)
	if _, err := PrivateKey(db); err == nil {
		t.Fatal("expected error with no key stored")
	}
}
