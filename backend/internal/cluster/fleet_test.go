package cluster

import "testing"

func TestValidateClusterName(t *testing.T) {
	ok := []string{"prod", "team-a", "kind-1", "a"}
	for _, name := range ok {
		if err := validateClusterName(name); err != nil {
			t.Errorf("validateClusterName(%q) = %v, want nil", name, err)
		}
	}

	bad := []string{"", "Prod", "-lead", "end-", "has_underscore", "spaces not", "this-name-is-way-too-long-for-the-forty-eight-char-limit-x"}
	for _, name := range bad {
		if err := validateClusterName(name); err == nil {
			t.Errorf("validateClusterName(%q) = nil, want error", name)
		}
	}
}
