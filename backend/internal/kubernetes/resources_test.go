package kubernetes

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestResourceSpecEmpty(t *testing.T) {
	if !(ResourceSpec{}).Empty() {
		t.Error("zero spec should be empty")
	}
	if !(ResourceSpec{CPURequest: "  "}).Empty() {
		t.Error("whitespace-only spec should be empty")
	}
	if (ResourceSpec{CPURequest: "100m"}).Empty() {
		t.Error("populated spec reported as empty")
	}
}

// Unset fields must be absent from the container, not zero. A zero CPU limit
// means "unlimited" to Kubernetes, and a zero memory request would schedule the
// pod onto any node regardless of what it needs.
func TestBuildResourceRequirementsOmitsUnsetFields(t *testing.T) {
	got := BuildResourceRequirements(ResourceSpec{CPURequest: "100m"})

	if _, present := got.Requests[corev1.ResourceCPU]; !present {
		t.Error("cpu request missing")
	}
	if _, present := got.Requests[corev1.ResourceMemory]; present {
		t.Error("unset memory request should be absent, not zero")
	}
	if _, present := got.Limits[corev1.ResourceCPU]; present {
		t.Error("unset cpu limit should be absent, not zero")
	}
	if _, present := got.Limits[corev1.ResourceMemory]; present {
		t.Error("unset memory limit should be absent, not zero")
	}
}

func TestBuildResourceRequirementsRoundTrip(t *testing.T) {
	spec := ResourceSpec{
		CPURequest:    "250m",
		CPULimit:      "1",
		MemoryRequest: "512Mi",
		MemoryLimit:   "1Gi",
	}

	got := resourceSpecFrom(BuildResourceRequirements(spec))
	if got != spec {
		t.Errorf("round trip changed the spec:\n got %+v\nwant %+v", got, spec)
	}
}

func TestResourceSpecValidate(t *testing.T) {
	valid := []ResourceSpec{
		{},
		{CPURequest: "100m", CPULimit: "500m", MemoryRequest: "256Mi", MemoryLimit: "256Mi"},
		{CPURequest: "1", CPULimit: "2"},
		{MemoryRequest: "1Gi", MemoryLimit: "2Gi"},
		// Equal request and limit is the recommended shape for memory.
		{MemoryRequest: "512Mi", MemoryLimit: "512Mi"},
	}
	for i, spec := range valid {
		if err := spec.Validate(); err != nil {
			t.Errorf("case %d rejected: %v", i, err)
		}
	}
}

func TestResourceSpecValidateRejectsBadInput(t *testing.T) {
	cases := map[string]ResourceSpec{
		"garbage cpu":              {CPURequest: "lots"},
		"bytes without unit style": {MemoryRequest: "512 MB"},
		"negative cpu":             {CPURequest: "-1"},
		// Kubernetes rejects these only at admission, with a message that does
		// not name the offending resource.
		"cpu request above limit":    {CPURequest: "2", CPULimit: "1"},
		"memory request above limit": {MemoryRequest: "1Gi", MemoryLimit: "512Mi"},
	}

	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if err := spec.Validate(); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestResourceSpecValidateNamesTheField(t *testing.T) {
	err := ResourceSpec{MemoryLimit: "not-a-quantity"}.Validate()
	if err == nil {
		t.Fatal("expected an error")
	}
	// With four fields on the form, an error that does not say which is broken
	// makes the user guess.
	if !strings.Contains(err.Error(), "memory limit") {
		t.Errorf("error should name the field, got %q", err)
	}
}
