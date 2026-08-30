package kubernetes

import "testing"

func TestAutoscalingEnabled(t *testing.T) {
	if (AutoscalingSpec{}).Enabled() {
		t.Fatal("empty spec is disabled")
	}
	if (AutoscalingSpec{MaxReplicas: 1}).Enabled() {
		t.Fatal("max=1 is a fixed replica, not an HPA")
	}
	if !(AutoscalingSpec{MinReplicas: 2, MaxReplicas: 6}).Enabled() {
		t.Fatal("max>1 must enable")
	}
}

func TestEnsureRequestsForHPA(t *testing.T) {
	got := EnsureRequestsForHPA(ResourceSpec{}, AutoscalingSpec{MaxReplicas: 4})
	if got.CPURequest != "100m" || got.MemoryRequest != "128Mi" {
		t.Fatalf("got %+v", got)
	}
	kept := EnsureRequestsForHPA(ResourceSpec{CPURequest: "250m", MemoryRequest: "256Mi"}, AutoscalingSpec{MaxReplicas: 4})
	if kept.CPURequest != "250m" || kept.MemoryRequest != "256Mi" {
		t.Fatalf("must not overwrite set requests: %+v", kept)
	}
	untouched := EnsureRequestsForHPA(ResourceSpec{}, AutoscalingSpec{})
	if !untouched.Empty() {
		t.Fatalf("disabled HPA must not invent requests: %+v", untouched)
	}
}

func TestHPAForIncludesCPUMetric(t *testing.T) {
	h := hpaFor("ns", "api", AutoscalingSpec{MinReplicas: 2, MaxReplicas: 6, CPUTarget: 70}.normalize())
	if h.Spec.MaxReplicas != 6 || h.Spec.MinReplicas == nil || *h.Spec.MinReplicas != 2 {
		t.Fatalf("replicas min/max = %v/%d", h.Spec.MinReplicas, h.Spec.MaxReplicas)
	}
	if len(h.Spec.Metrics) == 0 {
		t.Fatal("expected a CPU metric")
	}
}
