package build

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// The webhook endpoint is unauthenticated by design and starts builds, which
// execute repository code. Every case here is remote code execution if
// verification lets it through.
func TestVerifySignatureRejectsForgeries(t *testing.T) {
	const secret = "shhh"
	body := []byte(`{"ref":"refs/heads/main","after":"abc1234"}`)

	cases := []struct {
		name     string
		provider string
		headers  WebhookHeaders
	}{
		{"github missing signature", ProviderGitHub, WebhookHeaders{}},
		{"github wrong signature", ProviderGitHub, WebhookHeaders{GitHubSignature: sign("wrong", body)}},
		{"github malformed hex", ProviderGitHub, WebhookHeaders{GitHubSignature: "sha256=zzzz"}},
		{"github signature over other body", ProviderGitHub,
			WebhookHeaders{GitHubSignature: sign(secret, []byte(`{"ref":"refs/heads/evil"}`))}},
		{"gitlab wrong token", ProviderGitLab, WebhookHeaders{GitLabToken: "nope"}},
		{"gitlab empty token", ProviderGitLab, WebhookHeaders{}},
		{"bitbucket wrong signature", ProviderBitbucket,
			WebhookHeaders{BitbucketSignature: sign("wrong", body)}},
		{"generic with nothing", ProviderGeneric, WebhookHeaders{}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := VerifySignature(tt.provider, secret, body, tt.headers); err == nil {
				t.Error("forged or unsigned webhook accepted")
			}
		})
	}
}

// A repository with no configured secret must not accept anything at all.
// Treating "no secret" as "no verification required" would leave the endpoint
// open to anyone who learns a repository id.
func TestVerifySignatureRequiresAConfiguredSecret(t *testing.T) {
	body := []byte(`{}`)
	for _, provider := range []string{ProviderGitHub, ProviderGitLab, ProviderBitbucket, ProviderGeneric} {
		if err := VerifySignature(provider, "", body, WebhookHeaders{GitHubSignature: sign("", body)}); err == nil {
			t.Errorf("%s: accepted a webhook with no secret configured", provider)
		}
	}
}

func TestVerifySignatureAcceptsValidDeliveries(t *testing.T) {
	const secret = "shhh"
	body := []byte(`{"ref":"refs/heads/main","after":"abc1234def"}`)

	cases := []struct {
		name     string
		provider string
		headers  WebhookHeaders
	}{
		{"github", ProviderGitHub, WebhookHeaders{GitHubSignature: sign(secret, body)}},
		// The sha256= prefix is optional across providers and versions.
		{"github without prefix", ProviderGitHub,
			WebhookHeaders{GitHubSignature: sign(secret, body)[len("sha256="):]}},
		{"gitlab", ProviderGitLab, WebhookHeaders{GitLabToken: secret}},
		{"bitbucket", ProviderBitbucket, WebhookHeaders{BitbucketSignature: sign(secret, body)}},
		{"generic hmac", ProviderGeneric, WebhookHeaders{GitHubSignature: sign(secret, body)}},
		{"generic token", ProviderGeneric, WebhookHeaders{GitLabToken: secret}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := VerifySignature(tt.provider, secret, body, tt.headers); err != nil {
				t.Errorf("valid delivery rejected: %v", err)
			}
		})
	}
}

func TestParsePushEventGitHub(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main","after":"abc1234def5678","deleted":false}`)

	event, err := ParsePushEvent(ProviderGitHub, body, "push")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event.Branch != "main" {
		t.Errorf("Branch = %q, want main", event.Branch)
	}
	if event.CommitSHA != "abc1234def5678" {
		t.Errorf("CommitSHA = %q", event.CommitSHA)
	}
	if event.Deleted {
		t.Error("Deleted = true for a normal push")
	}
}

// A branch deletion has no commit to build. Both the flag and the all-zero SHA
// signal it, and providers are inconsistent about which they send.
func TestParsePushEventDetectsBranchDeletion(t *testing.T) {
	cases := map[string]struct {
		provider string
		body     string
	}{
		"github flag": {ProviderGitHub, `{"ref":"refs/heads/x","after":"abc1234","deleted":true}`},
		"github zero sha": {ProviderGitHub,
			`{"ref":"refs/heads/x","after":"0000000000000000000000000000000000000000"}`},
		"gitlab null checkout": {ProviderGitLab,
			`{"ref":"refs/heads/x","after":"0000000000000000000000000000000000000000","checkout_sha":null}`},
		"bitbucket null new": {ProviderBitbucket, `{"push":{"changes":[{"new":null}]}}`},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			event, err := ParsePushEvent(tt.provider, []byte(tt.body), "")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !event.Deleted {
				t.Error("branch deletion not detected; a build would start with no commit")
			}
		})
	}
}

func TestParsePushEventGitLab(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/develop","after":"aaa111","checkout_sha":"bbb222ccc"}`)

	event, err := ParsePushEvent(ProviderGitLab, body, "Push Hook")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event.Branch != "develop" {
		t.Errorf("Branch = %q", event.Branch)
	}
	// checkout_sha is the commit actually checked out and wins over after.
	if event.CommitSHA != "bbb222ccc" {
		t.Errorf("CommitSHA = %q, want checkout_sha", event.CommitSHA)
	}
}

func TestParsePushEventBitbucket(t *testing.T) {
	body := []byte(`{"push":{"changes":[{"new":{"name":"main","target":{"hash":"deadbeef123"}}}]}}`)

	event, err := ParsePushEvent(ProviderBitbucket, body, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event.Branch != "main" || event.CommitSHA != "deadbeef123" {
		t.Errorf("got %+v", event)
	}
}

// Non-push deliveries (ping, tag push, issues) must be ignored rather than
// building whatever the payload happens to contain.
func TestParsePushEventIgnoresOtherEvents(t *testing.T) {
	if _, err := ParsePushEvent(ProviderGitHub, []byte(`{}`), "ping"); !errors.Is(err, ErrUnsupportedEvent) {
		t.Errorf("github ping = %v, want ErrUnsupportedEvent", err)
	}
	if _, err := ParsePushEvent(ProviderGitLab, []byte(`{}`), "Tag Push Hook"); !errors.Is(err, ErrUnsupportedEvent) {
		t.Errorf("gitlab tag push = %v, want ErrUnsupportedEvent", err)
	}
	if _, err := ParsePushEvent(ProviderBitbucket, []byte(`{"push":{"changes":[]}}`), ""); !errors.Is(err, ErrUnsupportedEvent) {
		t.Errorf("bitbucket empty changes = %v, want ErrUnsupportedEvent", err)
	}
}

// A tag ref yields no branch. Building it under a branch name it never had
// would mislabel the resulting image.
func TestParsePushEventTagRefHasNoBranch(t *testing.T) {
	event, err := ParsePushEvent(ProviderGitHub, []byte(`{"ref":"refs/tags/v1.0.0","after":"abc123"}`), "push")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event.Branch != "" {
		t.Errorf("Branch = %q for a tag ref, want empty", event.Branch)
	}
}

func TestNormalizeProvider(t *testing.T) {
	cases := map[string]string{
		"github": ProviderGitHub, "GitHub": ProviderGitHub, "  gitlab ": ProviderGitLab,
		"bitbucket": ProviderBitbucket, "": ProviderGeneric, "svn": ProviderGeneric,
	}
	for input, want := range cases {
		if got := NormalizeProvider(input); got != want {
			t.Errorf("NormalizeProvider(%q) = %q, want %q", input, got, want)
		}
	}
}
