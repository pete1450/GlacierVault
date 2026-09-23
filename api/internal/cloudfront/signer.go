package cloudfront

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Signer mints CloudFront canned-policy signed URLs.
type Signer struct {
	keyPairID  string
	privateKey *rsa.PrivateKey
	baseURL    string // e.g. https://d111111abcdef8.cloudfront.net (no trailing slash)
}

// NewSigner builds a signer for the given distribution base URL and key pair.
func NewSigner(baseURL, keyPairID string, privateKey *rsa.PrivateKey) *Signer {
	return &Signer{baseURL: strings.TrimSuffix(baseURL, "/"), keyPairID: keyPairID, privateKey: privateKey}
}

// SignURL returns a signed HTTPS URL for the object at the given key
// (e.g. "data/ab12cd..."), valid until expiresAt. The signature covers only
// this exact object; the URL is unusable for anything else.
//
// CloudFront canned-policy signing: RSA PKCS#1 v1.5 with SHA-1 over the
// whitespace-stripped policy JSON, base64-encoded with CloudFront's URL-safe
// character replacements (+ -> -, = -> _, / -> ~).
func (s *Signer) SignURL(objectKey string, expiresAt time.Time) (string, error) {
	escapedPath := escapePath(objectKey)
	resource := s.baseURL + "/" + escapedPath
	expiry := expiresAt.Unix()

	// Canned policy, whitespace stripped (CloudFront requires the exact form).
	policy := fmt.Sprintf(
		`{"Statement":[{"Resource":%q,"Condition":{"DateLessThan":{"AWS:EpochTime":%d}}}]}`,
		resource, expiry,
	)

	digest := sha1.Sum([]byte(policy))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA1, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign policy: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(sig)
	encoded = strings.NewReplacer("+", "-", "=", "_", "/", "~").Replace(encoded)

	signed := fmt.Sprintf("%s?Expires=%d&Signature=%s&Key-Pair-Id=%s",
		resource, expiry, encoded, url.QueryEscape(s.keyPairID))
	return signed, nil
}

// escapePath percent-encodes an S3 object key for use in a URL path,
// preserving '/' separators.
func escapePath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
