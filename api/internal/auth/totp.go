package auth

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// TOTPConfig holds TOTP settings.
type TOTPConfig struct {
	Issuer      string
	AccountName string
}

// GenerateTOTPSecret creates a new TOTP secret.
func GenerateTOTPSecret(issuer, accountName string) (secret, url string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp secret: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTP checks a TOTP code against the secret.
func VerifyTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
}

// GenerateBackupCodes creates n single-use backup codes.
// Returns the plaintext codes (show once) and bcrypt hashes (store).
func GenerateBackupCodes(n int) (codes []string, hashes []string, err error) {
	codes = make([]string, n)
	hashes = make([]string, n)
	for i := 0; i < n; i++ {
		// 8-character alphanumeric code.
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)[:8]
		codes[i] = code
		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		hashes[i] = string(hash)
	}
	return codes, hashes, nil
}

// VerifyBackupCode checks a backup code against the stored hashes.
// Returns the index of the matched code, or -1.
func VerifyBackupCode(code string, hashes []string) int {
	for i, h := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(code)) == nil {
			return i
		}
	}
	return -1
}
