package kubernetes

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultCPUTarget int32 = 70

// AutoscalingSpec is a HorizontalPodAutoscaler. MaxReplicas=0 means "no HPA".
type AutoscalingSpec struct {
	MinReplicas  int32
	MaxReplicas  int32
	CPUTarget    int32
	MemoryTarget int32
}

func (s AutoscalingSpec) Enabled() bool {
	return s.MaxReplicas > 1
}

func (s AutoscalingSpec) normalize() AutoscalingSpec {
	out := s
	if out.MinReplicas <= 0 {
		out.MinReplicas = 1
	}
	if out.MaxReplicas < out.MinReplicas {
		out.MaxReplicas = out.MinReplicas
	}
	if out.CPUTarget <= 0 && out.MemoryTarget <= 0 {
		out.CPUTarget = defaultCPUTarget
	}
	return out
}

// EnsureRequestsForHPA fills CPU/memory requests when autoscaling is on and
// the user left them blank. HPA cannot compute utilisation without a request.
func EnsureRequestsForHPA(resources ResourceSpec, spec AutoscalingSpec) ResourceSpec {
	if !spec.Enabled() {
		return resources
	}
	if resources.CPURequest == "" {
		resources.CPURequest = "100m"
	}
	if resources.MemoryRequest == "" {
		resources.MemoryRequest = "128Mi"
	}
	return resources
}

// ApplyAutoscaling creates, updates, or removes the HPA for a deployment.
func (c *Client) ApplyAutoscaling(ctx context.Context, namespace, name string, spec AutoscalingSpec) error {
	cs, err := c.cs()
	if err != nil {
		return err
	}
	hpas := cs.AutoscalingV2().HorizontalPodAutoscalers(namespace)
	if !spec.Enabled() {
		err := hpas.Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("remove hpa: %w", err)
		}
		return nil
	}
	spec = spec.normalize()
	desired := hpaFor(namespace, name, spec)
	existing, err := hpas.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = hpas.Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create hpa: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get hpa: %w", err)
	}
	desired.ResourceVersion = existing.ResourceVersion
	_, err = hpas.Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update hpa: %w", err)
	}
	return nil
}

// GetAutoscaling reads the HPA attached to a deployment, if any.
func (c *Client) GetAutoscaling(ctx context.Context, namespace, name string) AutoscalingSpec {
	cs, err := c.cs()
	if err != nil {
		return AutoscalingSpec{}
	}
	hpa, err := cs.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return AutoscalingSpec{}
	}
	out := AutoscalingSpec{MaxReplicas: hpa.Spec.MaxReplicas}
	if hpa.Spec.MinReplicas != nil {
		out.MinReplicas = *hpa.Spec.MinReplicas
	}
	for _, m := range hpa.Spec.Metrics {
		if m.Type != autoscalingv2.ResourceMetricSourceType || m.Resource == nil || m.Resource.Target.AverageUtilization == nil {
			continue
		}
		switch m.Resource.Name {
		case corev1.ResourceCPU:
			out.CPUTarget = *m.Resource.Target.AverageUtilization
		case corev1.ResourceMemory:
			out.MemoryTarget = *m.Resource.Target.AverageUtilization
		}
	}
	return out
}

func hpaFor(namespace, name string, spec AutoscalingSpec) *autoscalingv2.HorizontalPodAutoscaler {
	min := spec.MinReplicas
	metrics := make([]autoscalingv2.MetricSpec, 0, 2)
	if spec.CPUTarget > 0 {
		cpu := spec.CPUTarget
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &cpu,
				},
			},
		})
	}
	if spec.MemoryTarget > 0 {
		mem := spec.MemoryTarget
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceMemory,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &mem,
				},
			},
		})
	}
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":                  name,
				"idp.platform/managed": "true",
			},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       name,
			},
			MinReplicas: &min,
			MaxReplicas: spec.MaxReplicas,
			Metrics:     metrics,
		},
	}
}
