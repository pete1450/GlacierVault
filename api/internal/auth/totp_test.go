package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateTOTPSecret(t *testing.T) {
	secret, url, err := GenerateTOTPSecret("GlacierVault", "admin")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if secret == "" {
		t.Fatal("empty secret")
	}
	if url == "" {
		t.Fatal("empty url")
	}
	// URL should be otpauth://
	if len(url) < 10 || url[:10] != "otpauth://" {
		t.Errorf("url should start with otpauth://, got %s", url[:20])
	}
}

func TestVerifyTOTP(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("GlacierVault", "admin")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Generate a valid code for now and verify it.
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if !VerifyTOTP(secret, code) {
		t.Error("valid code should verify")
	}
	if VerifyTOTP(secret, "000000") {
		t.Error("invalid code should not verify")
	}
}

func TestGenerateBackupCodes(t *testing.T) {
	codes, hashes, err := GenerateBackupCodes(10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("got %d codes, %d hashes, want 10 each", len(codes), len(hashes))
	}
	// Codes should be unique.
	seen := make(map[string]bool)
	for _, c := range codes {
		if seen[c] {
			t.Error("duplicate code generated")
		}
		seen[c] = true
	}
}

func TestVerifyBackupCode(t *testing.T) {
	codes, hashes, err := GenerateBackupCodes(5)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Each code should verify.
	for i, code := range codes {
		idx := VerifyBackupCode(code, hashes)
		if idx != i {
			t.Errorf("code %d: got idx %d, want %d", i, idx, i)
		}
	}
	// Wrong code should not verify.
	if idx := VerifyBackupCode("WRONG123", hashes); idx != -1 {
		t.Errorf("wrong code: got idx %d, want -1", idx)
	}
}
