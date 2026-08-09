package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Ticket errors are deliberately indistinguishable to the caller: a client that
// can tell "expired" from "bad signature" from "wrong resource" learns whether
// it guessed a valid namespace, which is exactly what the ticket exists to
// prevent.
var (
	// ErrInvalidTicket covers every rejection reason.
	ErrInvalidTicket = errors.New("invalid or expired access ticket")
	// ErrNoSigningKey reports a signer that was never configured.
	ErrNoSigningKey = errors.New("app access signing key unavailable")
)

// TicketTTL bounds how long a minted ticket stays usable. Access tickets are
// redeemed by an immediate browser navigation, so this only has to survive the
// round trip from RPC response to redirect — not a user's reading time.
const TicketTTL = 60 * time.Second

// ticketKeyLabel domain-separates the ticket signing key from the credential
// encryption key. Reusing one secret for two purposes lets a weakness in either
// construction attack the other, so the raw key is never used directly.
const ticketKeyLabel = "idp:app-access-ticket:v1"

// ticketPayload is the signed portion of a ticket.
//
// Namespace and Name are bound into the signature so a ticket minted for one
// workload cannot be replayed against another. Subject records who requested
// it, which makes the redemption auditable.
type ticketPayload struct {
	Namespace string `json:"ns"`
	Name      string `json:"n"`
	Subject   string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
}

// TicketSigner mints and verifies short-lived app-access tickets.
//
// Tickets exist because /apps/{namespace}/{name} is reached by a top-level
// browser navigation, which cannot carry an Authorization header. The
// authenticated RPC that mints the ticket is where authorization actually
// happens; the ticket is then proof that it happened.
type TicketSigner struct {
	key []byte
}

// NewTicketSigner derives a signing key from the platform encryption key.
//
// An empty encryption key yields a random per-process key rather than an error:
// single-replica deployments keep working, and the only visible effect is that
// tickets minted before a restart stop verifying — they live 60 seconds, so the
// window is negligible. Multi-replica deployments must set the encryption key,
// because otherwise each replica signs with a different secret and a ticket
// minted by one is rejected by another.
func NewTicketSigner(encodedEncryptionKey string) (*TicketSigner, error) {
	if strings.TrimSpace(encodedEncryptionKey) == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate ephemeral ticket key: %w", err)
		}
		return &TicketSigner{key: key}, nil
	}

	// The encryption key is mixed with a fixed label rather than used verbatim,
	// so the ticket key cannot be recovered from the encryption key or vice
	// versa without inverting SHA-256.
	sum := sha256.Sum256(append([]byte(ticketKeyLabel), []byte(encodedEncryptionKey)...))
	key := make([]byte, len(sum))
	copy(key, sum[:])
	return &TicketSigner{key: key}, nil
}

// Mint returns a signed ticket authorizing access to one workload.
func (s *TicketSigner) Mint(namespace, name, subject string) (string, error) {
	if s == nil || len(s.key) == 0 {
		return "", ErrNoSigningKey
	}

	payload := ticketPayload{
		Namespace: namespace,
		Name:      name,
		Subject:   subject,
		ExpiresAt: time.Now().Add(TicketTTL).Unix(),
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode ticket: %w", err)
	}

	body := base64.RawURLEncoding.EncodeToString(encoded)
	return body + "." + s.sign(body), nil
}

// Verify checks a ticket and confirms it was minted for this exact workload.
//
// It returns the subject the ticket was issued to, so the redemption can be
// logged against the user who requested it.
func (s *TicketSigner) Verify(ticket, namespace, name string) (string, error) {
	if s == nil || len(s.key) == 0 {
		return "", ErrNoSigningKey
	}

	body, signature, found := strings.Cut(ticket, ".")
	if !found || body == "" || signature == "" {
		return "", ErrInvalidTicket
	}

	// Compared before decoding: an attacker who cannot forge the MAC never
	// reaches the JSON parser. hmac.Equal is constant time.
	if !hmac.Equal([]byte(signature), []byte(s.sign(body))) {
		return "", ErrInvalidTicket
	}

	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", ErrInvalidTicket
	}

	var payload ticketPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", ErrInvalidTicket
	}

	if time.Now().Unix() > payload.ExpiresAt {
		return "", ErrInvalidTicket
	}

	// The signature proves the payload is ours; this proves it is *this*
	// workload's. Without it, a ticket for a namespace the user may reach would
	// unlock every namespace they may not.
	if payload.Namespace != namespace || payload.Name != name {
		return "", ErrInvalidTicket
	}

	return payload.Subject, nil
}

func (s *TicketSigner) sign(body string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
