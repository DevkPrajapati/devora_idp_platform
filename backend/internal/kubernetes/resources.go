package kubernetes

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ResourceSpec holds CPU and memory requests/limits as Kubernetes quantity
// strings ("250m", "512Mi").
//
// Strings rather than numbers throughout: Kubernetes' units are not expressible
// as plain integers, and converting to a number and back is where rounding bugs
// come from. The strings are parsed once, here, to validate them.
type ResourceSpec struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

// Empty reports whether nothing was specified.
func (r ResourceSpec) Empty() bool {
	return strings.TrimSpace(r.CPURequest) == "" &&
		strings.TrimSpace(r.CPULimit) == "" &&
		strings.TrimSpace(r.MemoryRequest) == "" &&
		strings.TrimSpace(r.MemoryLimit) == ""
}

// Validate parses every set field and checks request <= limit.
//
// Kubernetes rejects a request above its limit, but only at admission and with
// a message that does not say which resource was at fault.
func (r ResourceSpec) Validate() error {
	cpuRequest, err := parseQuantity(r.CPURequest, "cpu request")
	if err != nil {
		return err
	}
	cpuLimit, err := parseQuantity(r.CPULimit, "cpu limit")
	if err != nil {
		return err
	}
	memRequest, err := parseQuantity(r.MemoryRequest, "memory request")
	if err != nil {
		return err
	}
	memLimit, err := parseQuantity(r.MemoryLimit, "memory limit")
	if err != nil {
		return err
	}

	if cpuRequest != nil && cpuLimit != nil && cpuRequest.Cmp(*cpuLimit) > 0 {
		return fmt.Errorf("cpu request (%s) must not exceed its limit (%s)", r.CPURequest, r.CPULimit)
	}
	if memRequest != nil && memLimit != nil && memRequest.Cmp(*memLimit) > 0 {
		return fmt.Errorf("memory request (%s) must not exceed its limit (%s)", r.MemoryRequest, r.MemoryLimit)
	}
	return nil
}

// BuildResourceRequirements renders the container's resources. Unset fields are
// left absent rather than defaulted to zero: an explicit zero limit means
// "unlimited" to Kubernetes, and a zero request would schedule the pod anywhere
// regardless of what it needs. Absent lets a namespace LimitRange apply instead.
func BuildResourceRequirements(spec ResourceSpec) corev1.ResourceRequirements {
	requirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}

	set := func(list corev1.ResourceList, name corev1.ResourceName, value string) {
		quantity, err := parseQuantity(value, "")
		if err != nil || quantity == nil {
			return
		}
		list[name] = *quantity
	}

	set(requirements.Requests, corev1.ResourceCPU, spec.CPURequest)
	set(requirements.Requests, corev1.ResourceMemory, spec.MemoryRequest)
	set(requirements.Limits, corev1.ResourceCPU, spec.CPULimit)
	set(requirements.Limits, corev1.ResourceMemory, spec.MemoryLimit)

	return requirements
}

// resourceSpecFrom reads a container's resources back for display.
func resourceSpecFrom(requirements corev1.ResourceRequirements) ResourceSpec {
	spec := ResourceSpec{}
	if q, ok := requirements.Requests[corev1.ResourceCPU]; ok {
		spec.CPURequest = q.String()
	}
	if q, ok := requirements.Requests[corev1.ResourceMemory]; ok {
		spec.MemoryRequest = q.String()
	}
	if q, ok := requirements.Limits[corev1.ResourceCPU]; ok {
		spec.CPULimit = q.String()
	}
	if q, ok := requirements.Limits[corev1.ResourceMemory]; ok {
		spec.MemoryLimit = q.String()
	}
	return spec
}

// parseQuantity returns nil for an empty value, so callers can distinguish
// "not set" from "set to zero".
func parseQuantity(value, field string) (*resource.Quantity, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		if field == "" {
			return nil, err
		}
		return nil, fmt.Errorf(
			"invalid %s %q: use a Kubernetes quantity such as 250m, 1, 512Mi or 1Gi", field, value)
	}
	if quantity.Sign() < 0 {
		if field == "" {
			return nil, fmt.Errorf("negative quantity")
		}
		return nil, fmt.Errorf("%s must not be negative", field)
	}
	return &quantity, nil
}
