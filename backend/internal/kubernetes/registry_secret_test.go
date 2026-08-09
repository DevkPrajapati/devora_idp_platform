package kubernetes

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestNormalizeRegistryHost(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Docker Hub is the one registry whose auth key is not its hostname.
		// Every spelling users type must collapse to the legacy v1 index URL,
		// or the kubelet silently fails to find the credential.
		{"docker hub bare", "docker.io", dockerHubAuthKey},
		{"docker hub with scheme", "https://index.docker.io", dockerHubAuthKey},
		{"docker hub api host", "registry-1.docker.io", dockerHubAuthKey},
		{"docker hub legacy", "registry.hub.docker.com", dockerHubAuthKey},

		{"ghcr", "ghcr.io", "ghcr.io"},
		{"ghcr with scheme", "https://ghcr.io", "ghcr.io"},
		{"gitlab", "registry.gitlab.com", "registry.gitlab.com"},
		{"ecr", "123456789012.dkr.ecr.eu-west-1.amazonaws.com", "123456789012.dkr.ecr.eu-west-1.amazonaws.com"},
		{"acr", "myreg.azurecr.io", "myreg.azurecr.io"},
		{"gcr", "gcr.io", "gcr.io"},
		{"self hosted with port", "harbor.corp.example:5000", "harbor.corp.example:5000"},
		{"uppercase is lowered", "GHCR.IO", "ghcr.io"},
		{"trailing slash", "https://ghcr.io/", "ghcr.io"},

		// Credential lookup is hierarchical, so the org path must be dropped:
		// keeping it makes the entry miss for images in a sibling org.
		{"path is dropped", "quay.io/myorg", "quay.io"},
		{"whitespace trimmed", "  ghcr.io  ", "ghcr.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeRegistryHost(tt.input)
			if err != nil {
				t.Fatalf("NormalizeRegistryHost(%q) returned error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeRegistryHost(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRegistryHostRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"unsupported scheme", "ftp://registry.example.com"},
		// A pasted "user:pass@host" would otherwise become part of the auth key
		// and leak the password into the Secret's JSON keys.
		{"embedded credentials", "https://user:pass@ghcr.io"},
		{"space in host", "ghcr .io"},
		{"underscore in host", "my_registry.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := NormalizeRegistryHost(tt.input); err == nil {
				t.Errorf("NormalizeRegistryHost(%q) = %q, want error", tt.input, got)
			}
		})
	}
}

func TestBuildDockerConfigJSON(t *testing.T) {
	payload, err := BuildDockerConfigJSON(RegistryCredential{
		Name:        "dockerhub",
		RegistryURL: "docker.io",
		Username:    "ci-bot",
		Password:    "hunter2",
		Email:       "ci@example.com",
	})
	if err != nil {
		t.Fatalf("BuildDockerConfigJSON returned error: %v", err)
	}

	var cfg dockerConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}

	entry, ok := cfg.Auths[dockerHubAuthKey]
	if !ok {
		t.Fatalf("auths has no %q key; got keys %v", dockerHubAuthKey, keysOf(cfg.Auths))
	}
	if entry.Username != "ci-bot" || entry.Password != "hunter2" {
		t.Errorf("credentials not carried through: %+v", entry)
	}
	if entry.Email != "ci@example.com" {
		t.Errorf("email = %q, want ci@example.com", entry.Email)
	}

	// Some registries read only the combined `auth` field, so it must be
	// present and correct even though username/password are also set.
	wantAuth := base64.StdEncoding.EncodeToString([]byte("ci-bot:hunter2"))
	if entry.Auth != wantAuth {
		t.Errorf("auth = %q, want %q", entry.Auth, wantAuth)
	}
}

func TestBuildDockerConfigJSONOmitsEmptyEmail(t *testing.T) {
	payload, err := BuildDockerConfigJSON(RegistryCredential{
		RegistryURL: "ghcr.io",
		Username:    "u",
		Password:    "p",
	})
	if err != nil {
		t.Fatalf("BuildDockerConfigJSON returned error: %v", err)
	}
	// An explicit empty email makes some registries reject the config.
	if strings.Contains(string(payload), `"email"`) {
		t.Errorf("payload should omit empty email, got %s", payload)
	}
}

func TestBuildDockerConfigJSONRequiresCredentials(t *testing.T) {
	tests := []struct {
		name string
		cred RegistryCredential
	}{
		{"no username", RegistryCredential{RegistryURL: "ghcr.io", Password: "p"}},
		{"no password", RegistryCredential{RegistryURL: "ghcr.io", Username: "u"}},
		{"no registry", RegistryCredential{Username: "u", Password: "p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildDockerConfigJSON(tt.cred); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestRegistrySecretName(t *testing.T) {
	if got := RegistrySecretName("dockerhub"); got != "idp-registry-dockerhub" {
		t.Errorf("RegistrySecretName = %q, want idp-registry-dockerhub", got)
	}
}

func TestMergeImagePullSecrets(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		managed  []string
		want     []string
	}{
		{
			name:    "adds managed secrets to an empty list",
			managed: []string{"idp-registry-dockerhub"},
			want:    []string{"idp-registry-dockerhub"},
		},
		{
			// Anything a user attached by hand is theirs to keep; the platform
			// only owns its own prefix.
			name:     "preserves hand-attached secrets",
			existing: []string{"my-own-secret"},
			managed:  []string{"idp-registry-ghcr"},
			want:     []string{"my-own-secret", "idp-registry-ghcr"},
		},
		{
			// This is how a deleted credential stops being referenced.
			name:     "drops managed secrets that no longer exist",
			existing: []string{"idp-registry-old", "keep-me"},
			managed:  []string{"idp-registry-new"},
			want:     []string{"keep-me", "idp-registry-new"},
		},
		{
			name:     "is idempotent",
			existing: []string{"idp-registry-dockerhub"},
			managed:  []string{"idp-registry-dockerhub"},
			want:     []string{"idp-registry-dockerhub"},
		},
		{
			name:     "removes every managed secret when none remain",
			existing: []string{"idp-registry-dockerhub"},
			managed:  nil,
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeImagePullSecrets(refs(tt.existing), tt.managed)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", names(got), tt.want)
			}
			for i := range got {
				if got[i].Name != tt.want[i] {
					t.Fatalf("got %v, want %v", names(got), tt.want)
				}
			}
		})
	}
}

func refs(in []string) []corev1.LocalObjectReference {
	out := make([]corev1.LocalObjectReference, 0, len(in))
	for _, n := range in {
		out = append(out, corev1.LocalObjectReference{Name: n})
	}
	return out
}

func names(in []corev1.LocalObjectReference) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		out = append(out, r.Name)
	}
	return out
}

func keysOf(m map[string]dockerConfigEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
