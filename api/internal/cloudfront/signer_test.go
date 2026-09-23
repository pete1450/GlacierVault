package cloudfront

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

// verifySignedURL reverses the CloudFront URL-safe encoding and verifies the
// RSA-SHA1 signature against the expected canned policy. It mirrors what the
// CloudFront edge does when validating a signed URL.
func verifySignedURL(t *testing.T, kp *KeyPair, signed string) {
	t.Helper()
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	q := u.Query()
	sigEncoded, expires, keyPairID := q.Get("Signature"), q.Get("Expires"), q.Get("Key-Pair-Id")
	if sigEncoded == "" || expires == "" || keyPairID == "" {
		t.Fatalf("signed URL missing query params: %s", signed)
	}
	// Reverse CloudFront's character replacements.
	standard := strings.NewReplacer("-", "+", "_", "=", "~", "/").Replace(sigEncoded)
	sig, err := base64.StdEncoding.DecodeString(standard)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	resource := "https://" + u.Host + u.EscapedPath()
	policy := `{"Statement":[{"Resource":` + quote(resource) +
		`,"Condition":{"DateLessThan":{"AWS:EpochTime":` + expires + `}}}]}`

	priv, err := ParsePrivateKeyPEM(kp.PrivateKeyPEM)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	digest := sha1.Sum([]byte(policy))
	if err := rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA1, digest[:], sig); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func quote(s string) string {
	// Minimal JSON string quoting sufficient for our resource URLs.
	r := strings.ReplaceAll(s, `\`, `\\`)
	r = strings.ReplaceAll(r, `"`, `\"`)
	return `"` + r + `"`
}

func TestSignURLRoundTrip(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	priv, err := ParsePrivateKeyPEM(kp.PrivateKeyPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKeyPEM: %v", err)
	}
	signer := NewSigner("https://d111111abcdef8.cloudfront.net", "K2JCJMDEHXQW5F", priv)

	signed, err := signer.SignURL("data/ab12cd34ef56", time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("SignURL: %v", err)
	}
	if !strings.HasPrefix(signed, "https://d111111abcdef8.cloudfront.net/data/ab12cd34ef56?") {
		t.Fatalf("unexpected signed URL prefix: %s", signed)
	}
	verifySignedURL(t, kp, signed)
}

func TestSignURLEscapesSpecialChars(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	priv, _ := ParsePrivateKeyPEM(kp.PrivateKeyPEM)
	signer := NewSigner("https://d111111abcdef8.cloudfront.net", "K2JCJMDEHXQW5F", priv)

	// Keys with spaces must be escaped identically in the resource and the URL.
	signed, err := signer.SignURL("data/weird key+name", time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("SignURL: %v", err)
	}
	if strings.Contains(signed, " ") {
		t.Fatalf("signed URL contains raw space: %s", signed)
	}
	verifySignedURL(t, kp, signed)
}

func TestKeyPairPEMFormats(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if !strings.HasPrefix(kp.PrivateKeyPEM, "-----BEGIN PRIVATE KEY-----") {
		t.Fatalf("private key is not PKCS#8 PEM:\n%s", kp.PrivateKeyPEM)
	}
	if !strings.HasPrefix(kp.PublicKeyPEM, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("public key is not PKIX PEM:\n%s", kp.PublicKeyPEM)
	}
	if _, err := ParsePrivateKeyPEM(kp.PrivateKeyPEM); err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if _, err := ParsePrivateKeyPEM("not a key"); err == nil {
		t.Fatalf("expected error for garbage input")
	}
}
