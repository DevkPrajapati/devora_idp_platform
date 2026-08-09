// Package secretbox encrypts small secrets — registry passwords, secret
// environment values — before they are written to the database.
//
// Without it the platform's own database becomes the weakest link: a dump, a
// read replica, or a backup handed to a contractor would expose every registry
// password the platform holds. Values are sealed with AES-256-GCM, which is
// authenticated, so tampering with a ciphertext is detected on read rather than
// silently producing a different password.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the required key length in bytes (AES-256).
const KeySize = 32

// envelopeV1 tags ciphertexts produced by this implementation. Storing a
// version byte means a future key-rotation or algorithm change can still read
// everything written today instead of orphaning it.
const envelopeV1 byte = 0x01

// ErrNoKey reports that encryption was requested while no key is configured.
// Callers surface this as FailedPrecondition rather than storing plaintext.
var ErrNoKey = errors.New("encryption key not configured")

// ErrCiphertext reports a value that is not a well-formed envelope, or whose
// authentication tag does not verify (wrong key, or tampering).
var ErrCiphertext = errors.New("invalid or corrupt ciphertext")

// Box seals and opens secrets with a single symmetric key.
type Box struct {
	aead cipher.AEAD
}

// New creates a Box from a raw 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", KeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// NewFromEncodedKey builds a Box from a base64- or hex-encoded key, the two
// forms a key is realistically pasted into an environment variable. An empty
// string yields a nil Box, so an operator who has not configured a key gets a
// clear ErrNoKey at write time instead of silent plaintext storage.
func NewFromEncodedKey(encoded string) (*Box, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}

	if key, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(key) == KeySize {
		return New(key)
	}
	if key, err := hex.DecodeString(encoded); err == nil && len(key) == KeySize {
		return New(key)
	}

	return nil, fmt.Errorf(
		"encryption key must decode to %d bytes from base64 or hex; generate one with: openssl rand -base64 %d",
		KeySize, KeySize,
	)
}

// GenerateKey returns a new base64-encoded key, used by tests and by the
// key-generation helper.
func GenerateKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// Encrypt seals plaintext into a versioned envelope: version || nonce || ciphertext.
// A nil Box returns ErrNoKey so a missing key can never degrade into plaintext.
func (b *Box) Encrypt(plaintext string) ([]byte, error) {
	if b == nil {
		return nil, ErrNoKey
	}

	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	envelope := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	envelope = append(envelope, envelopeV1)
	envelope = append(envelope, nonce...)
	envelope = b.aead.Seal(envelope, nonce, []byte(plaintext), nil)
	return envelope, nil
}

// Decrypt opens an envelope produced by Encrypt.
func (b *Box) Decrypt(envelope []byte) (string, error) {
	if b == nil {
		return "", ErrNoKey
	}

	nonceSize := b.aead.NonceSize()
	if len(envelope) < 1+nonceSize || envelope[0] != envelopeV1 {
		return "", ErrCiphertext
	}

	nonce := envelope[1 : 1+nonceSize]
	plaintext, err := b.aead.Open(nil, nonce, envelope[1+nonceSize:], nil)
	if err != nil {
		// The underlying error only ever says "message authentication failed",
		// which leaks nothing useful; collapse it to a stable sentinel.
		return "", ErrCiphertext
	}
	return string(plaintext), nil
}

// Enabled reports whether a usable key is configured.
func (b *Box) Enabled() bool { return b != nil }
