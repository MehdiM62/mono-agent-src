package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// Encrypt seals plaintext under key (must be 32 bytes) using AES-256-GCM,
// generating a fresh random nonce for this call.
func Encrypt(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	return encryptAD(key, plaintext, nil)
}

// encryptAD is Encrypt with additional authenticated data: the ciphertext opens only under the
// same ad (the install binding of a wrapped DEK, bind.go).
func encryptAD(key, plaintext, ad []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("secrets: generating nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, ad)
	return ciphertext, nonce, nil
}

// Decrypt opens ciphertext sealed by Encrypt with the same key and nonce.
func Decrypt(key, ciphertext, nonce []byte) ([]byte, error) {
	return decryptAD(key, ciphertext, nonce, nil)
}

// decryptAD is Decrypt with the additional authenticated data the ciphertext was sealed with.
func decryptAD(key, ciphertext, nonce, ad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("secrets: invalid nonce size %d, want %d", len(nonce), gcm.NonceSize())
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, ad)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	return plaintext, nil
}
