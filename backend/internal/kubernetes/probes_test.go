package kubernetes

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// The headline requirement: leaving the form blank must produce no probe at
// all. An empty probe in the pod spec is not equivalent — Kubernetes would
// apply its own defaults against a path the app may not serve.
func TestBuildProbeOmittedWhenUnconfigured(t *testing.T) {
	unconfigured := []ProbeSpec{
		{},
		{Path: "   "},
		// Timings without a path are still "not configured": there is nothing
		// to probe, so the numbers are meaningless on their own.
		{InitialDelaySeconds: 10, PeriodSeconds: 5, FailureThreshold: 3},
	}

	for i, spec := range unconfigured {
		if spec.Configured() {
			t.Errorf("case %d: Configured() = true for %+v", i, spec)
		}
		if got := BuildProbe(spec, 8080); got != nil {
			t.Errorf("case %d: BuildProbe returned %+v, want nil", i, got)
		}
	}
}

func TestBuildProbeAppliesKubernetesDefaults(t *testing.T) {
	// A user who fills in only a path should get exactly what Kubernetes would
	// have applied itself — no surprises hidden in the platform.
	probe := BuildProbe(ProbeSpec{Path: "/healthz"}, 8080)
	if probe == nil {
		t.Fatal("BuildProbe returned nil for a configured probe")
	}

	if probe.HTTPGet == nil {
		t.Fatal("probe has no HTTPGet handler")
	}
	if probe.HTTPGet.Path != "/healthz" {
		t.Errorf("Path = %q, want /healthz", probe.HTTPGet.Path)
	}
	// An unset probe port must inherit the container port, not default to 0.
	if got := probe.HTTPGet.Port.IntValue(); got != 8080 {
		t.Errorf("Port = %d, want the container port 8080", got)
	}
	if probe.HTTPGet.Scheme != corev1.URISchemeHTTP {
		t.Errorf("Scheme = %q, want HTTP", probe.HTTPGet.Scheme)
	}

	if probe.TimeoutSeconds != defaultProbeTimeoutSeconds {
		t.Errorf("TimeoutSeconds = %d, want %d", probe.TimeoutSeconds, defaultProbeTimeoutSeconds)
	}
	if probe.PeriodSeconds != defaultProbePeriodSeconds {
		t.Errorf("PeriodSeconds = %d, want %d", probe.PeriodSeconds, defaultProbePeriodSeconds)
	}
	if probe.FailureThreshold != defaultProbeFailureThreshold {
		t.Errorf("FailureThreshold = %d, want %d", probe.FailureThreshold, defaultProbeFailureThreshold)
	}
	if probe.InitialDelaySeconds != 0 {
		t.Errorf("InitialDelaySeconds = %d, want 0", probe.InitialDelaySeconds)
	}
}

func TestBuildProbeHonoursExplicitValues(t *testing.T) {
	probe := BuildProbe(ProbeSpec{
		Path:                "/ready",
		Port:                9000,
		InitialDelaySeconds: 15,
		TimeoutSeconds:      3,
		PeriodSeconds:       20,
		FailureThreshold:    5,
	}, 8080)

	if probe.HTTPGet.Port.IntValue() != 9000 {
		t.Errorf("explicit probe port ignored, got %d", probe.HTTPGet.Port.IntValue())
	}
	if probe.InitialDelaySeconds != 15 || probe.TimeoutSeconds != 3 ||
		probe.PeriodSeconds != 20 || probe.FailureThreshold != 5 {
		t.Errorf("explicit timings not carried through: %+v", probe)
	}
}

func TestProbeValidate(t *testing.T) {
	// An unconfigured probe must never be an error: blank is the default state
	// of the form.
	if err := (ProbeSpec{}).Validate("readiness"); err != nil {
		t.Errorf("blank probe rejected: %v", err)
	}

	invalid := []struct {
		name string
		spec ProbeSpec
	}{
		{"path without leading slash", ProbeSpec{Path: "healthz"}},
		{"port too high", ProbeSpec{Path: "/x", Port: 70000}},
		{"negative delay", ProbeSpec{Path: "/x", InitialDelaySeconds: -1}},
		{"negative threshold", ProbeSpec{Path: "/x", FailureThreshold: -2}},
		// Kubernetes rejects this at admission with a message that does not
		// name the probe, so it is caught here instead.
		{"timeout exceeds period", ProbeSpec{Path: "/x", TimeoutSeconds: 30, PeriodSeconds: 10}},
	}

	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.spec.Validate("readiness"); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestProbeValidateNamesTheProbe(t *testing.T) {
	err := ProbeSpec{Path: "healthz"}.Validate("liveness")
	if err == nil {
		t.Fatal("expected an error")
	}
	// With two probes on the form, an error that does not say which one is
	// broken makes the user guess.
	if !strings.HasPrefix(err.Error(), "liveness") {
		t.Errorf("error should name the probe, got %q", err)
	}
}

func TestProbeToSpecRoundTrip(t *testing.T) {
	original := ProbeSpec{
		Path:                "/healthz",
		Port:                8080,
		InitialDelaySeconds: 5,
		TimeoutSeconds:      2,
		PeriodSeconds:       10,
		FailureThreshold:    3,
	}

	got := probeToSpec(BuildProbe(original, 8080))
	if got != original {
		t.Errorf("round trip changed the spec:\n got %+v\nwant %+v", got, original)
	}
}

func TestProbeToSpecIgnoresNonHTTPProbes(t *testing.T) {
	// A TCP or exec probe attached by hand must not be reported as an HTTP one
	// with an empty path, which the UI would render as a misconfigured probe.
	tcp := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{},
		},
	}
	if got := probeToSpec(tcp); got.Configured() {
		t.Errorf("TCP probe reported as configured HTTP probe: %+v", got)
	}
	if got := probeToSpec(nil); got.Configured() {
		t.Errorf("nil probe reported as configured: %+v", got)
	}
}
