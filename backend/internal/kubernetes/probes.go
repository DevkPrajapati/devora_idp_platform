package kubernetes

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Probe defaults mirror Kubernetes' own, so a user who fills in only a path
// gets the behaviour the API server would have applied anyway.
const (
	defaultProbeTimeoutSeconds   int32 = 1
	defaultProbePeriodSeconds    int32 = 10
	defaultProbeFailureThreshold int32 = 3
)

// ProbeSpec is a platform-level HTTP health check.
//
// An empty Path means "not configured". That is the only signal used: probes
// are opt-in, because a liveness probe pointed at a path the app does not serve
// restarts a healthy container forever, which is strictly worse than no probe.
type ProbeSpec struct {
	Path                string
	Port                int32
	InitialDelaySeconds int32
	TimeoutSeconds      int32
	PeriodSeconds       int32
	FailureThreshold    int32
}

// Configured reports whether the user asked for this probe.
func (p ProbeSpec) Configured() bool {
	return strings.TrimSpace(p.Path) != ""
}

// Validate checks a probe's fields. An unconfigured probe is always valid —
// leaving the form blank must not be an error.
func (p ProbeSpec) Validate(kind string) error {
	if !p.Configured() {
		return nil
	}

	if !strings.HasPrefix(p.Path, "/") {
		return fmt.Errorf("%s probe path must start with /", kind)
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("%s probe port must be between 1 and 65535", kind)
	}
	if p.InitialDelaySeconds < 0 || p.TimeoutSeconds < 0 || p.PeriodSeconds < 0 || p.FailureThreshold < 0 {
		return fmt.Errorf("%s probe values must not be negative", kind)
	}
	// Kubernetes rejects a timeout longer than the period, but only at admission
	// time and with a message that does not name the probe.
	timeout := orDefault(p.TimeoutSeconds, defaultProbeTimeoutSeconds)
	period := orDefault(p.PeriodSeconds, defaultProbePeriodSeconds)
	if timeout > period {
		return fmt.Errorf("%s probe timeout (%ds) must not exceed its period (%ds)", kind, timeout, period)
	}
	return nil
}

// BuildProbe renders a Kubernetes probe, or nil when the user left it blank.
// containerPort is the fallback for an unset probe port.
func BuildProbe(spec ProbeSpec, containerPort int32) *corev1.Probe {
	if !spec.Configured() {
		return nil
	}

	port := spec.Port
	if port <= 0 {
		port = containerPort
	}

	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   spec.Path,
				Port:   intstr.FromInt32(port),
				Scheme: corev1.URISchemeHTTP,
			},
		},
		InitialDelaySeconds: spec.InitialDelaySeconds,
		TimeoutSeconds:      orDefault(spec.TimeoutSeconds, defaultProbeTimeoutSeconds),
		PeriodSeconds:       orDefault(spec.PeriodSeconds, defaultProbePeriodSeconds),
		FailureThreshold:    orDefault(spec.FailureThreshold, defaultProbeFailureThreshold),
	}
}

// probeToSpec converts a live probe back for display. Non-HTTP probes attached
// by hand report an empty spec rather than being misrepresented as HTTP ones.
func probeToSpec(probe *corev1.Probe) ProbeSpec {
	if probe == nil || probe.HTTPGet == nil {
		return ProbeSpec{}
	}
	return ProbeSpec{
		Path:                probe.HTTPGet.Path,
		Port:                int32(probe.HTTPGet.Port.IntValue()),
		InitialDelaySeconds: probe.InitialDelaySeconds,
		TimeoutSeconds:      probe.TimeoutSeconds,
		PeriodSeconds:       probe.PeriodSeconds,
		FailureThreshold:    probe.FailureThreshold,
	}
}

func orDefault(value, fallback int32) int32 {
	if value <= 0 {
		return fallback
	}
	return value
}
