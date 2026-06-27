package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

// MasterKey is the 32-byte AES-256 key loaded at startup.
var masterKey []byte

// LoadOrCreateMasterKey loads the master key from path, creating a new random
// key and writing it to disk if it doesn't exist yet.
func LoadOrCreateMasterKey(path string) error {
	data, err := os.ReadFile(path)
	if err == nil && len(data) == 32 {
		masterKey = data
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) && err != nil {
		return err
	}

	// Generate a new 32-byte key.
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return err
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return err
	}
	masterKey = key
	return nil
}

// Encrypt encrypts plaintext with AES-256-GCM using the master key.
// The returned string is base64url-encoded (nonce || ciphertext || tag).
func Encrypt(plaintext string) (string, error) {
	if len(masterKey) != 32 {
		return "", errors.New("master key not loaded")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(sealed), nil
}

// Decrypt decrypts a value produced by Encrypt.
func Decrypt(encoded string) (string, error) {
	if len(masterKey) != 32 {
		return "", errors.New("master key not loaded")
	}
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
