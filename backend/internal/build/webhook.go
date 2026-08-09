package build

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Supported git providers. The provider decides how a webhook is authenticated
// and how its payload is read — the three differ in both.
const (
	ProviderGitHub    = "github"
	ProviderGitLab    = "gitlab"
	ProviderBitbucket = "bitbucket"
	ProviderGeneric   = "generic"
)

// ErrInvalidSignature reports a webhook that failed authentication. It is
// deliberately opaque: telling a caller *why* verification failed helps an
// attacker tune their forgery.
var ErrInvalidSignature = errors.New("webhook signature verification failed")

// ErrUnsupportedEvent reports a delivery the platform does not act on, such as
// a ping or a branch deletion.
var ErrUnsupportedEvent = errors.New("webhook event ignored")

// NormalizeProvider maps user input to a supported provider.
func NormalizeProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ProviderGitHub:
		return ProviderGitHub
	case ProviderGitLab:
		return ProviderGitLab
	case ProviderBitbucket:
		return ProviderBitbucket
	default:
		return ProviderGeneric
	}
}

// WebhookHeaders carries the provider-specific headers needed for verification.
type WebhookHeaders struct {
	// GitHubSignature is X-Hub-Signature-256, "sha256=<hex>".
	GitHubSignature string
	// GitLabToken is X-Gitlab-Token, compared directly rather than as an HMAC.
	GitLabToken string
	// BitbucketSignature is X-Hub-Signature, "sha256=<hex>".
	BitbucketSignature string
	// EventType is the provider's event name, used to ignore non-push
	// deliveries before doing any work.
	EventType string
}

// VerifySignature authenticates a webhook delivery.
//
// A configured secret is mandatory: without one, anyone who learns a repository
// id could trigger arbitrary builds, and builds execute repository code. An
// unauthenticated webhook endpoint is remote code execution with extra steps.
func VerifySignature(provider, secret string, body []byte, headers WebhookHeaders) error {
	if secret == "" {
		return fmt.Errorf("no webhook secret configured for this repository")
	}

	switch provider {
	case ProviderGitHub:
		return verifyHMAC(secret, body, headers.GitHubSignature)

	case ProviderBitbucket:
		return verifyHMAC(secret, body, headers.BitbucketSignature)

	case ProviderGitLab:
		// GitLab sends the shared secret verbatim rather than signing the body.
		// Still compared in constant time so the endpoint does not leak the
		// secret one byte at a time through response timing.
		if constantTimeEqual(headers.GitLabToken, secret) {
			return nil
		}
		return ErrInvalidSignature

	default:
		// A generic provider is treated as HMAC-SHA256 over the body, which is
		// the common convention; the secret is still required.
		if headers.GitHubSignature != "" {
			return verifyHMAC(secret, body, headers.GitHubSignature)
		}
		if constantTimeEqual(headers.GitLabToken, secret) {
			return nil
		}
		return ErrInvalidSignature
	}
}

// verifyHMAC checks a "sha256=<hex>" signature over the raw body.
func verifyHMAC(secret string, body []byte, header string) error {
	signature := strings.TrimSpace(header)
	if signature == "" {
		return ErrInvalidSignature
	}
	// The prefix is optional across providers and versions.
	signature = strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)

	got, err := hex.DecodeString(signature)
	if err != nil {
		return ErrInvalidSignature
	}
	// hmac.Equal is constant time; a plain bytes.Equal would leak the signature
	// through timing, letting an attacker forge one byte at a time.
	if !hmac.Equal(got, want) {
		return ErrInvalidSignature
	}
	return nil
}

// constantTimeEqual compares two strings without an early exit.
func constantTimeEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// PushEvent is the subset of a webhook payload the platform acts on.
type PushEvent struct {
	Branch    string
	CommitSHA string
	// Deleted marks a branch-deletion push, which must not trigger a build:
	// there is no longer a commit to build.
	Deleted bool
}

// ParsePushEvent extracts the branch and commit from a provider payload.
//
// Only the fields the platform needs are decoded. Modelling each provider's
// full payload would be a large surface that changes without notice, for data
// that is never used.
func ParsePushEvent(provider string, body []byte, eventType string) (PushEvent, error) {
	switch provider {
	case ProviderGitHub:
		if eventType != "" && eventType != "push" {
			return PushEvent{}, ErrUnsupportedEvent
		}
		var payload struct {
			Ref     string `json:"ref"`
			After   string `json:"after"`
			Deleted bool   `json:"deleted"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return PushEvent{}, fmt.Errorf("parse github payload: %w", err)
		}
		return PushEvent{
			Branch:    branchFromRef(payload.Ref),
			CommitSHA: payload.After,
			// GitHub reports a deletion both with the flag and with an
			// all-zero "after" SHA.
			Deleted: payload.Deleted || isZeroSHA(payload.After),
		}, nil

	case ProviderGitLab:
		if eventType != "" && !strings.EqualFold(eventType, "Push Hook") {
			return PushEvent{}, ErrUnsupportedEvent
		}
		var payload struct {
			Ref         string `json:"ref"`
			After       string `json:"after"`
			CheckoutSHA string `json:"checkout_sha"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return PushEvent{}, fmt.Errorf("parse gitlab payload: %w", err)
		}
		commit := payload.CheckoutSHA
		if commit == "" {
			commit = payload.After
		}
		// GitLab signals a deleted branch with a null checkout_sha.
		return PushEvent{
			Branch:    branchFromRef(payload.Ref),
			CommitSHA: commit,
			Deleted:   commit == "" || isZeroSHA(payload.After),
		}, nil

	case ProviderBitbucket:
		var payload struct {
			Push struct {
				Changes []struct {
					New *struct {
						Name   string `json:"name"`
						Target struct {
							Hash string `json:"hash"`
						} `json:"target"`
					} `json:"new"`
				} `json:"changes"`
			} `json:"push"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return PushEvent{}, fmt.Errorf("parse bitbucket payload: %w", err)
		}
		if len(payload.Push.Changes) == 0 {
			return PushEvent{}, ErrUnsupportedEvent
		}
		change := payload.Push.Changes[0]
		// A nil "new" is Bitbucket's branch deletion.
		if change.New == nil {
			return PushEvent{Deleted: true}, nil
		}
		return PushEvent{Branch: change.New.Name, CommitSHA: change.New.Target.Hash}, nil

	default:
		// Generic payloads carry whatever the sender chooses; accept the two
		// shapes the other providers use.
		var payload struct {
			Ref    string `json:"ref"`
			Branch string `json:"branch"`
			After  string `json:"after"`
			Commit string `json:"commit"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return PushEvent{}, fmt.Errorf("parse webhook payload: %w", err)
		}
		branch := payload.Branch
		if branch == "" {
			branch = branchFromRef(payload.Ref)
		}
		commit := payload.Commit
		if commit == "" {
			commit = payload.After
		}
		return PushEvent{Branch: branch, CommitSHA: commit, Deleted: isZeroSHA(commit)}, nil
	}
}

// branchFromRef turns refs/heads/main into main. A tag ref yields an empty
// string, so tag pushes do not build under a branch name they never had.
func branchFromRef(ref string) string {
	if strings.HasPrefix(ref, "refs/heads/") {
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return ""
}

// isZeroSHA reports the all-zero SHA providers send for a deleted ref.
func isZeroSHA(sha string) bool {
	if sha == "" {
		return false
	}
	return strings.Trim(sha, "0") == ""
}
