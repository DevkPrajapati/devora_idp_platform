package deployment

import (
	"testing"

	"github.com/idp/platform/backend/internal/kubernetes"
)

// Every template ships as the platform's recommendation, so each one has to be
// something the cluster will actually accept. A typo in a quantity string would
// otherwise only surface when a developer picked that template.
func TestTemplatesAreValid(t *testing.T) {
	templates := Templates()
	if len(templates) == 0 {
		t.Fatal("no templates defined")
	}

	seen := map[string]bool{}
	for _, tpl := range templates {
		t.Run(tpl.Id, func(t *testing.T) {
			if tpl.Id == "" || tpl.Name == "" {
				t.Fatal("template needs an id and a name")
			}
			if seen[tpl.Id] {
				t.Fatalf("duplicate template id %q", tpl.Id)
			}
			seen[tpl.Id] = true

			if tpl.Replicas < 1 {
				t.Errorf("replicas = %d, want at least 1", tpl.Replicas)
			}
			if tpl.Port < 1 || tpl.Port > 65535 {
				t.Errorf("port = %d, out of range", tpl.Port)
			}
			if tpl.ExampleImage == "" {
				t.Error("template should suggest an example image")
			}
			if tpl.Rationale == "" {
				t.Error("template should explain why its defaults were chosen")
			}

			resources := resourcesFromProto(tpl.Resources)
			if resources.Empty() {
				t.Fatal("template must recommend resources")
			}
			if err := resources.Validate(); err != nil {
				t.Errorf("resources invalid: %v", err)
			}

			if err := probeFromProto(tpl.ReadinessProbe).Validate("readiness"); err != nil {
				t.Errorf("readiness probe invalid: %v", err)
			}
			if err := probeFromProto(tpl.LivenessProbe).Validate("liveness"); err != nil {
				t.Errorf("liveness probe invalid: %v", err)
			}

			// Config keys go through envFrom, which silently drops names that
			// are not C_IDENTIFIERs.
			for _, kv := range tpl.ConfigVars {
				if err := kubernetes.ValidateEnvKey(kv.Key); err != nil {
					t.Errorf("config var: %v", err)
				}
			}
			for _, key := range tpl.SuggestedSecretKeys {
				if err := kubernetes.ValidateEnvKey(key); err != nil {
					t.Errorf("suggested secret key: %v", err)
				}
			}
		})
	}
}

// Suggesting a secret *value* would mean shipping a default credential that
// teams would deploy unchanged.
func TestTemplatesSuggestSecretNamesWithoutValues(t *testing.T) {
	for _, tpl := range Templates() {
		for _, key := range tpl.SuggestedSecretKeys {
			if key == "" {
				t.Errorf("%s: empty suggested secret key", tpl.Id)
			}
			// A name appearing in both lists would be sent as a config var
			// with a visible value and as a secret, and envFrom would resolve
			// it to the Secret silently.
			for _, kv := range tpl.ConfigVars {
				if kv.Key == key {
					t.Errorf("%s: %q is listed as both a config var and a secret", tpl.Id, key)
				}
			}
		}
	}
}

// The catalogue is package-level state shared by every request. Handing out the
// same pointers would let one request's mutation leak into all later ones.
func TestTemplatesReturnsIndependentCopies(t *testing.T) {
	first := Templates()
	second := Templates()

	if first[0] == second[0] {
		t.Fatal("Templates returned the same pointer twice")
	}

	first[0].Name = "mutated"
	first[0].Replicas = 99
	if first[0].Resources != nil {
		first[0].Resources.CpuLimit = "mutated"
	}
	if len(first[0].ConfigVars) > 0 {
		first[0].ConfigVars[0].Value = "mutated"
	}

	third := Templates()
	if third[0].Name == "mutated" || third[0].Replicas == 99 {
		t.Error("mutating a returned template corrupted the catalogue")
	}
	if third[0].Resources != nil && third[0].Resources.CpuLimit == "mutated" {
		t.Error("nested Resources message is shared between calls")
	}
	if len(third[0].ConfigVars) > 0 && third[0].ConfigVars[0].Value == "mutated" {
		t.Error("nested ConfigVars messages are shared between calls")
	}
}

// The five stacks named in the requirements must all be present.
func TestRequiredTemplatesPresent(t *testing.T) {
	want := []string{"nodejs-api", "react-app", "go-api", "python-fastapi", "spring-boot"}

	have := map[string]bool{}
	for _, tpl := range Templates() {
		have[tpl.Id] = true
	}
	for _, id := range want {
		if !have[id] {
			t.Errorf("missing template %q", id)
		}
	}
}

// Memory is incompressible: a container over its limit is OOM-killed, so a
// request below the limit only buys scheduling onto a node that cannot honour
// it. Every template should follow that rule.
func TestTemplateMemoryRequestEqualsLimit(t *testing.T) {
	for _, tpl := range Templates() {
		if tpl.Resources == nil {
			continue
		}
		if tpl.Resources.MemoryRequest != tpl.Resources.MemoryLimit {
			t.Errorf("%s: memory request %q != limit %q",
				tpl.Id, tpl.Resources.MemoryRequest, tpl.Resources.MemoryLimit)
		}
	}
}
