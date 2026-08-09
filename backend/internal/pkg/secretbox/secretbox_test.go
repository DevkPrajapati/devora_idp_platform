package secretbox

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func newTestBox(t *testing.T) *Box {
	t.Helper()
	encoded, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	box, err := NewFromEncodedKey(encoded)
	if err != nil {
		t.Fatalf("NewFromEncodedKey: %v", err)
	}
	if box == nil {
		t.Fatal("NewFromEncodedKey returned a nil box for a valid key")
	}
	return box
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	box := newTestBox(t)

	for _, plaintext := range []string{
		"hunter2",
		"",
		"a password with spaces and ünïcödé ✓",
		strings.Repeat("x", 4096),
	} {
		sealed, err := box.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		got, err := box.Decrypt(sealed)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plaintext {
			t.Errorf("round trip = %q, want %q", got, plaintext)
		}
	}
}

func TestCiphertextDoesNotContainPlaintext(t *testing.T) {
	box := newTestBox(t)
	const password = "correct-horse-battery-staple"

	sealed, err := box.Encrypt(password)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// The whole point of the package: a database dump must not reveal the
	// password to anyone grepping the BYTEA column.
	if bytes.Contains(sealed, []byte(password)) {
		t.Error("ciphertext contains the plaintext password")
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	box := newTestBox(t)

	first, err := box.Encrypt("same-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := box.Encrypt("same-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Identical ciphertexts would let an observer tell which projects share a
	// registry password without decrypting anything.
	if bytes.Equal(first, second) {
		t.Error("two encryptions of the same value produced identical ciphertext")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	box := newTestBox(t)

	sealed, err := box.Encrypt("hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := box.Decrypt(tampered); !errors.Is(err, ErrCiphertext) {
		t.Errorf("Decrypt(tampered) error = %v, want ErrCiphertext", err)
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	sealed, err := newTestBox(t).Encrypt("hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	other := newTestBox(t)
	if _, err := other.Decrypt(sealed); !errors.Is(err, ErrCiphertext) {
		t.Errorf("Decrypt(wrong key) error = %v, want ErrCiphertext", err)
	}
}

func TestDecryptRejectsMalformedEnvelope(t *testing.T) {
	box := newTestBox(t)

	tests := []struct {
		name     string
		envelope []byte
	}{
		{"empty", nil},
		{"too short", []byte{envelopeV1, 0x00}},
		{"unknown version", append([]byte{0xFF}, make([]byte, 32)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := box.Decrypt(tt.envelope); !errors.Is(err, ErrCiphertext) {
				t.Errorf("Decrypt error = %v, want ErrCiphertext", err)
			}
		})
	}
}

func TestNilBoxRefusesToOperate(t *testing.T) {
	var box *Box

	// A missing key must never degrade into storing plaintext.
	if _, err := box.Encrypt("hunter2"); !errors.Is(err, ErrNoKey) {
		t.Errorf("Encrypt on nil box = %v, want ErrNoKey", err)
	}
	if _, err := box.Decrypt([]byte{envelopeV1}); !errors.Is(err, ErrNoKey) {
		t.Errorf("Decrypt on nil box = %v, want ErrNoKey", err)
	}
	if box.Enabled() {
		t.Error("nil box reports Enabled() = true")
	}
}

func TestNewFromEncodedKey(t *testing.T) {
	raw := make([]byte, KeySize)
	for i := range raw {
		raw[i] = byte(i)
	}

	t.Run("accepts base64", func(t *testing.T) {
		box, err := NewFromEncodedKey(base64.StdEncoding.EncodeToString(raw))
		if err != nil || box == nil {
			t.Fatalf("base64 key rejected: box=%v err=%v", box, err)
		}
	})

	t.Run("accepts hex", func(t *testing.T) {
		box, err := NewFromEncodedKey(hex.EncodeToString(raw))
		if err != nil || box == nil {
			t.Fatalf("hex key rejected: box=%v err=%v", box, err)
		}
	})

	t.Run("empty yields a disabled box, not an error", func(t *testing.T) {
		box, err := NewFromEncodedKey("   ")
		if err != nil {
			t.Fatalf("empty key returned an error: %v", err)
		}
		if box.Enabled() {
			t.Error("empty key produced an enabled box")
		}
	})

	t.Run("rejects a short key", func(t *testing.T) {
		if _, err := NewFromEncodedKey(base64.StdEncoding.EncodeToString([]byte("too-short"))); err == nil {
			t.Error("expected an error for a short key")
		}
	})
}

func TestNewRejectsWrongKeySize(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil {
		t.Error("New accepted a 16-byte key; AES-256 requires 32")
	}
}
