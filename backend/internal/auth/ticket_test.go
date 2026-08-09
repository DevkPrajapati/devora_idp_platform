package auth

import (
	"errors"
	"strings"
	"testing"
)

// Synthetic 32-byte key. Not a credential from any environment.
const testKey = "dGVzdC1vbmx5LWtleS1mb3ItdW5pdC10ZXN0cy0zMmJ5dGU="

func newTestSigner(t *testing.T) *TicketSigner {
	t.Helper()
	signer, err := NewTicketSigner(testKey)
	if err != nil {
		t.Fatalf("NewTicketSigner() error = %v", err)
	}
	return signer
}

func TestTicketRoundTrip(t *testing.T) {
	signer := newTestSigner(t)

	ticket, err := signer.Mint("demo", "web", "user-1")
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}

	subject, err := signer.Verify(ticket, "demo", "web")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if subject != "user-1" {
		t.Errorf("subject = %q, want %q", subject, "user-1")
	}
}

// The whole point of binding namespace and name into the signature: a ticket
// for a workload the caller may reach must not unlock one they may not.
func TestTicketIsBoundToItsWorkload(t *testing.T) {
	signer := newTestSigner(t)

	ticket, err := signer.Mint("demo", "web", "user-1")
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		workload  string
	}{
		{"different namespace", "kube-system", "web"},
		{"different workload", "demo", "postgres"},
		{"both different", "kube-system", "postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := signer.Verify(ticket, tt.namespace, tt.workload); !errors.Is(err, ErrInvalidTicket) {
				t.Errorf("Verify() error = %v, want ErrInvalidTicket", err)
			}
		})
	}
}

func TestTicketRejectsTamperedInput(t *testing.T) {
	signer := newTestSigner(t)

	valid, err := signer.Mint("demo", "web", "user-1")
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	body, signature, _ := strings.Cut(valid, ".")

	tests := []struct {
		name   string
		ticket string
	}{
		{"empty", ""},
		{"no separator", body + signature},
		{"empty body", "." + signature},
		{"empty signature", body + "."},
		{"forged signature", body + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"swapped payload", "eyJucyI6Imt1YmUtc3lzdGVtIiwibiI6IndlYiJ9." + signature},
		{"garbage", "not-a-ticket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := signer.Verify(tt.ticket, "demo", "web"); err == nil {
				t.Error("Verify() accepted an invalid ticket")
			}
		})
	}
}

// A ticket signed by one deployment must not verify against another's key.
func TestTicketDoesNotVerifyAcrossKeys(t *testing.T) {
	minter := newTestSigner(t)

	other, err := NewTicketSigner("YW5vdGhlci10ZXN0LWtleS0zMi1ieXRlcy1sb25nLXh4")
	if err != nil {
		t.Fatalf("NewTicketSigner() error = %v", err)
	}

	ticket, err := minter.Mint("demo", "web", "user-1")
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}

	if _, err := other.Verify(ticket, "demo", "web"); !errors.Is(err, ErrInvalidTicket) {
		t.Errorf("Verify() error = %v, want ErrInvalidTicket", err)
	}
}

// An absent encryption key must not disable signing — it falls back to a random
// per-process key, which still has to produce verifiable tickets.
func TestTicketSignerWithoutConfiguredKey(t *testing.T) {
	signer, err := NewTicketSigner("")
	if err != nil {
		t.Fatalf("NewTicketSigner(\"\") error = %v", err)
	}

	ticket, err := signer.Mint("demo", "web", "user-1")
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	if _, err := signer.Verify(ticket, "demo", "web"); err != nil {
		t.Errorf("Verify() error = %v", err)
	}

	// Two ephemeral signers must not agree, or the fallback would be a fixed
	// key masquerading as a random one.
	otherProcess, err := NewTicketSigner("")
	if err != nil {
		t.Fatalf("NewTicketSigner(\"\") error = %v", err)
	}
	if _, err := otherProcess.Verify(ticket, "demo", "web"); err == nil {
		t.Error("a separate ephemeral signer verified another process's ticket")
	}
}

func TestNilSignerFailsClosed(t *testing.T) {
	var signer *TicketSigner

	if _, err := signer.Mint("demo", "web", "u"); !errors.Is(err, ErrNoSigningKey) {
		t.Errorf("Mint() error = %v, want ErrNoSigningKey", err)
	}
	if _, err := signer.Verify("anything", "demo", "web"); !errors.Is(err, ErrNoSigningKey) {
		t.Errorf("Verify() error = %v, want ErrNoSigningKey", err)
	}
}
