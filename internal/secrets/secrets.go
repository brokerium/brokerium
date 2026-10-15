// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package secrets encrypts credentials at rest with AES-256-GCM.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
)

// Box seals and opens small payloads. The ciphertext layout is nonce || sealed data.
type Box struct {
	aead cipher.AEAD
}

// New creates a Box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("secret key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts data produced by Seal.
func (b *Box) Open(data []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(data) < n {
		return nil, errors.New("ciphertext too short")
	}
	plain, err := b.aead.Open(nil, data[:n], data[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}
	return plain, nil
}

// SealJSON encrypts the JSON encoding of v.
func (b *Box) SealJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b.Seal(raw)
}

// OpenJSON decrypts data and decodes it into v. Empty data leaves v untouched.
func (b *Box) OpenJSON(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	raw, err := b.Open(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
