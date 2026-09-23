// Package cloudfront implements GlacierVault's free-egress restore path.
//
// Restoring from S3 directly incurs $0.09/GB data-transfer-out charges (after
// the 100 GB/month free tier). CloudFront instead offers 1 TB/month of data
// transfer out plus 10M requests free to every account, S3→CloudFront origin
// fetches are free, and there is no fixed monthly cost for a distribution —
// so routing restore downloads through CloudFront makes typical restores
// effectively free.
//
// Design (keeps the bucket private end to end):
//
//   - A CloudFront distribution fronts the cold bucket using Origin Access
//     Control (OAC). The bucket policy grants s3:GetObject only to the
//     CloudFront service principal for this distribution; block-public-access
//     stays on. Nothing is publicly reachable.
//   - The distribution's cache behavior requires signed URLs (trusted key
//     group). Only the holder of the private key — this container — can mint
//     them.
//   - A localhost-only proxy (see proxy.go) translates rustic's S3 GET/HEAD
//     requests into short-lived, single-object signed URLs, fetches the bytes
//     server-side, and streams them back as S3-shaped responses. Signed URLs
//     never leave the container.
//   - Pack objects are content-addressed and immutable, so the cache policy
//     excludes query strings from the cache key (the signature lives in the
//     query string) and uses a long TTL. Repeat restores are served from the
//     edge.
//
// The distribution is provisioned with the AWS SDK (see provision.go) rather
// than the CDK stack, because the CDK app is fetched from upstream
// rustic-rs/rustic-aws at image build time and is not part of this repo.
package cloudfront

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// KeyPair is an RSA key pair used for CloudFront signed URLs.
type KeyPair struct {
	// PrivateKeyPEM is the PKCS#8 PEM-encoded private key. Stored encrypted
	// in the database; never leaves the container.
	PrivateKeyPEM string
	// PublicKeyPEM is the PEM-encoded PKIX public key, uploaded to CloudFront.
	PublicKeyPEM string
	private      *rsa.PrivateKey
}

// GenerateKeyPair creates a fresh 2048-bit RSA key pair for CloudFront URL
// signing.
func GenerateKeyPair() (*KeyPair, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	kp := &KeyPair{
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})),
		PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})),
		private:       priv,
	}
	return kp, nil
}

// ParsePrivateKeyPEM decodes a PKCS#8 (or PKCS#1) PEM private key.
func ParsePrivateKeyPEM(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("PKCS#8 key is not RSA")
	}
	if rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return rsaKey, nil
	}
	return nil, fmt.Errorf("unrecognized private key format")
}
