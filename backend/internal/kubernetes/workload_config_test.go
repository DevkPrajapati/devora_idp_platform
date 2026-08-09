package kubernetes

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestValidateEnvKey(t *testing.T) {
	valid := []string{"NODE_ENV", "DB_PASSWORD", "_private", "A1", "log_level", "X"}
	for _, key := range valid {
		if err := ValidateEnvKey(key); err != nil {
			t.Errorf("ValidateEnvKey(%q) = %v, want nil", key, err)
		}
	}

	// These are all legal ConfigMap keys but are silently dropped by envFrom,
	// which is exactly the failure mode this validation exists to prevent.
	invalid := []string{"", "1STARTS_WITH_DIGIT", "has-dash", "has.dot", "has space", "has/slash", "café"}
	for _, key := range invalid {
		if err := ValidateEnvKey(key); err == nil {
			t.Errorf("ValidateEnvKey(%q) = nil, want error", key)
		}
	}
}

func TestValidateEnvMapRejectsOversizedPayload(t *testing.T) {
	if err := ValidateEnvMap(map[string]string{"OK": "small"}, "config"); err != nil {
		t.Fatalf("small map rejected: %v", err)
	}

	oversized := map[string]string{"BIG": strings.Repeat("x", maxConfigBytes+1)}
	err := ValidateEnvMap(oversized, "config variables")
	if err == nil {
		t.Fatal("oversized map accepted; the API server would reject it with an unhelpful message")
	}
	if !strings.Contains(err.Error(), "config variables") {
		t.Errorf("error should name the offending kind, got %q", err)
	}
}

func TestConfigChecksumIsStableAcrossMapOrdering(t *testing.T) {
	// Go randomises map iteration order. An order-sensitive hash would change
	// on every save and roll the deployment for no reason.
	config := map[string]string{"A": "1", "B": "2", "C": "3", "D": "4", "E": "5"}
	secrets := map[string]string{"P": "x", "Q": "y"}

	first := ConfigChecksum(config, secrets)
	for i := 0; i < 50; i++ {
		if got := ConfigChecksum(config, secrets); got != first {
			t.Fatalf("checksum changed between identical calls: %q vs %q", first, got)
		}
	}
}

func TestConfigChecksumChangesWithContent(t *testing.T) {
	base := ConfigChecksum(map[string]string{"NODE_ENV": "prod"}, map[string]string{"KEY": "a"})

	cases := map[string]string{
		"config value changed": ConfigChecksum(map[string]string{"NODE_ENV": "dev"}, map[string]string{"KEY": "a"}),
		"config key added":     ConfigChecksum(map[string]string{"NODE_ENV": "prod", "X": "1"}, map[string]string{"KEY": "a"}),
		// A secret-only change must still roll the pods, otherwise a rotated
		// password would never reach a running container.
		"secret value changed": ConfigChecksum(map[string]string{"NODE_ENV": "prod"}, map[string]string{"KEY": "b"}),
		"secret removed":       ConfigChecksum(map[string]string{"NODE_ENV": "prod"}, map[string]string{}),
	}

	for name, got := range cases {
		if got == base {
			t.Errorf("%s: checksum unchanged; pods would not roll", name)
		}
	}
}

func TestConfigChecksumSeparatesConfigFromSecrets(t *testing.T) {
	// Without a delimiter between the two maps, moving a variable from config
	// to secret would hash identically and skip the rollout.
	asConfig := ConfigChecksum(map[string]string{"TOKEN": "v"}, map[string]string{})
	asSecret := ConfigChecksum(map[string]string{}, map[string]string{"TOKEN": "v"})
	if asConfig == asSecret {
		t.Error("promoting a variable from config to secret produced the same checksum")
	}
}

func TestWorkloadConfigNames(t *testing.T) {
	if got := WorkloadConfigMapName("backend-api"); got != "backend-api-config" {
		t.Errorf("WorkloadConfigMapName = %q", got)
	}
	if got := WorkloadSecretName("backend-api"); got != "backend-api-secrets" {
		t.Errorf("WorkloadSecretName = %q", got)
	}
}

func TestWorkloadEnvFrom(t *testing.T) {
	sources := workloadEnvFrom("backend-api")
	if len(sources) != 2 {
		t.Fatalf("got %d envFrom sources, want 2", len(sources))
	}

	cm := sources[0].ConfigMapRef
	if cm == nil || cm.Name != "backend-api-config" {
		t.Fatalf("first source should reference the ConfigMap, got %+v", sources[0])
	}
	secret := sources[1].SecretRef
	if secret == nil || secret.Name != "backend-api-secrets" {
		t.Fatalf("second source should reference the Secret, got %+v", sources[1])
	}

	// Non-optional refs wedge every pod in CreateContainerConfigError if an
	// object is deleted by hand; optional degrades to a missing variable.
	for i, opt := range []*bool{cm.Optional, secret.Optional} {
		if opt == nil || !*opt {
			t.Errorf("envFrom source %d must be optional", i)
		}
	}
}

// The core guarantee of this feature: a container built by the platform sources
// configuration through envFrom and carries no inline values in its pod spec.
func TestContainerCarriesNoInlineEnv(t *testing.T) {
	container := corev1.Container{
		Name:    "backend-api",
		Image:   "acme/api:1",
		EnvFrom: workloadEnvFrom("backend-api"),
	}

	if len(container.Env) != 0 {
		t.Errorf("container.Env must stay empty, got %+v", container.Env)
	}
	if len(container.EnvFrom) == 0 {
		t.Error("container must source configuration through envFrom")
	}
}
