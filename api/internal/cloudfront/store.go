package cloudfront

import (
	"crypto/rsa"
	"database/sql"
	"fmt"
	"time"

	appCrypto "github.com/glaciervault/api/internal/crypto"
)

// StoredConfig mirrors the cf_* columns of the aws_config table (migration
// 004). The private signing key is stored encrypted with the app master key;
// use PrivateKey to obtain the parsed key.
type StoredConfig struct {
	Enabled        bool
	DistributionID string
	Domain         string
	KeyPairID      string
	KeyGroupID     string
	OACID          string
	CachePolicyID  string
	PrivateKeyEnc  string // encrypted PEM; empty when reusing a distribution provisioned elsewhere
	ProvisionedAt  string
}

// Load reads the CloudFront configuration. It returns Enabled=false when the
// free-egress path has not been provisioned.
func Load(db *sql.DB) (*StoredConfig, error) {
	var c StoredConfig
	var enabled int
	err := db.QueryRow(`
		SELECT cf_enabled, cf_distribution_id, cf_domain, cf_key_pair_id,
		       cf_key_group_id, cf_oac_id, cf_cache_policy_id,
		       cf_private_key_enc, cf_provisioned_at
		FROM aws_config WHERE id=1`,
	).Scan(&enabled, &c.DistributionID, &c.Domain, &c.KeyPairID,
		&c.KeyGroupID, &c.OACID, &c.CachePolicyID, &c.PrivateKeyEnc, &c.ProvisionedAt)
	if err != nil {
		return nil, fmt.Errorf("load cloudfront config: %w", err)
	}
	c.Enabled = enabled == 1
	return &c, nil
}

// Save persists the CloudFront configuration after provisioning. The private
// key PEM is encrypted before it is stored. Empty fields in info (as returned
// when Ensure reuses an existing distribution) keep their stored values, as
// does an empty privateKeyPEM.
func Save(db *sql.DB, info *Info, privateKeyPEM string) error {
	existing, err := Load(db)
	if err != nil {
		return err
	}
	merged := *info
	if merged.DistributionID == "" {
		merged.DistributionID = existing.DistributionID
	}
	if merged.Domain == "" {
		merged.Domain = existing.Domain
	}
	if merged.KeyPairID == "" {
		merged.KeyPairID = existing.KeyPairID
	}
	if merged.KeyGroupID == "" {
		merged.KeyGroupID = existing.KeyGroupID
	}
	if merged.OACID == "" {
		merged.OACID = existing.OACID
	}
	if merged.CachePolicyID == "" {
		merged.CachePolicyID = existing.CachePolicyID
	}
	if merged.Domain == "" {
		return fmt.Errorf("no CloudFront distribution domain to save")
	}
	encKey := existing.PrivateKeyEnc
	if privateKeyPEM != "" {
		encKey, err = appCrypto.Encrypt(privateKeyPEM)
		if err != nil {
			return fmt.Errorf("encrypt signing key: %w", err)
		}
	}
	if merged.KeyPairID == "" || encKey == "" {
		return fmt.Errorf("reused an existing CloudFront distribution but no signing key is stored — " +
			"delete the GlacierVault-managed distribution and provision again")
	}
	_, err = db.Exec(`
		UPDATE aws_config SET cf_enabled=1, cf_distribution_id=?, cf_domain=?,
			cf_key_pair_id=?, cf_key_group_id=?, cf_oac_id=?, cf_cache_policy_id=?,
			cf_private_key_enc=?, cf_provisioned_at=? WHERE id=1`,
		merged.DistributionID, merged.Domain, merged.KeyPairID, merged.KeyGroupID,
		merged.OACID, merged.CachePolicyID, encKey, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Disable turns the free-egress path off without deleting the stored
// identifiers (re-enabling reuses the same distribution).
func Disable(db *sql.DB) error {
	_, err := db.Exec(`UPDATE aws_config SET cf_enabled=0 WHERE id=1`)
	return err
}

// PrivateKey decrypts and parses the stored signing key.
func PrivateKey(db *sql.DB) (*rsa.PrivateKey, error) {
	cfg, err := Load(db)
	if err != nil {
		return nil, err
	}
	if cfg.PrivateKeyEnc == "" {
		return nil, fmt.Errorf("no CloudFront signing key stored")
	}
	pemData, err := appCrypto.Decrypt(cfg.PrivateKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt signing key: %w", err)
	}
	return ParsePrivateKeyPEM(pemData)
}

// BaseURL returns the https base URL of the distribution.
func (c *StoredConfig) BaseURL() string {
	return "https://" + c.Domain
}
