package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateMasterKeyCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	if err := LoadOrCreateMasterKey(path); err != nil {
		t.Fatalf("LoadOrCreateMasterKey: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if len(data) != 32 {
		t.Fatalf("key length = %d, want 32", len(data))
	}
}

func TestLoadOrCreateMasterKeyLoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	if err := LoadOrCreateMasterKey(path); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, _ := os.ReadFile(path)
	if err := LoadOrCreateMasterKey(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatal("key changed on reload")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := LoadOrCreateMasterKey(filepath.Join(dir, "k")); err != nil {
		t.Fatalf("load key: %v", err)
	}
	for _, pt := range []string{"", "hello", "a longer secret with unicode: 🔐"} {
		enc, err := Encrypt(pt)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", pt, err)
		}
		dec, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if dec != pt {
			t.Fatalf("round trip: got %q, want %q", dec, pt)
		}
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	dir := t.TempDir()
	if err := LoadOrCreateMasterKey(filepath.Join(dir, "k")); err != nil {
		t.Fatalf("load key: %v", err)
	}
	a, _ := Encrypt("same")
	b, _ := Encrypt("same")
	if a == b {
		t.Fatal("expected different ciphertexts due to random nonce")
	}
}

func TestDecryptInvalid(t *testing.T) {
	dir := t.TempDir()
	if err := LoadOrCreateMasterKey(filepath.Join(dir, "k")); err != nil {
		t.Fatalf("load key: %v", err)
	}
	for _, bad := range []string{"", "!!!", "aGVsbG8="} {
		if _, err := Decrypt(bad); err == nil {
			t.Fatalf("Decrypt(%q): expected error", bad)
		}
	}
	// Tampered ciphertext.
	enc, _ := Encrypt("secret")
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := Decrypt(tampered); err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestEncryptWithoutKey(t *testing.T) {
	masterKey = nil
	if _, err := Encrypt("x"); err == nil {
		t.Fatal("expected error without loaded key")
	}
	if _, err := Decrypt("eA=="); err == nil {
		t.Fatal("expected error without loaded key")
	}
}
