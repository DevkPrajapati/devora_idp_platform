package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ServiceTypeClusterIP exposes a workload only inside the cluster.
const ServiceTypeClusterIP = "ClusterIP"

// ServiceTypeNodePort exposes a workload on a port of every cluster node.
const ServiceTypeNodePort = "NodePort"

// WorkloadServiceSpec describes the Service fronting a deployment.
type WorkloadServiceSpec struct {
	Namespace   string
	Name        string
	Port        int32
	ServiceType string
	Labels      map[string]string
}

// WorkloadServiceInfo reports how a deployment can be reached.
type WorkloadServiceInfo struct {
	Type           string
	Port           int32
	NodePort       int32
	ClusterIP      string
	ClusterAddress string
}

// NormalizeServiceType maps user input to a supported Service type,
// falling back to ClusterIP for anything unrecognised.
func NormalizeServiceType(serviceType string) string {
	if serviceType == ServiceTypeNodePort {
		return ServiceTypeNodePort
	}
	return ServiceTypeClusterIP
}

// CreateWorkloadService creates the Service that makes a deployment reachable.
// Without it a Deployment has no ClusterIP, no DNS name and no node port, so
// nothing can route traffic to its pods.
func (c *Client) CreateWorkloadService(ctx context.Context, spec WorkloadServiceSpec) (*WorkloadServiceInfo, error) {
	serviceType := NormalizeServiceType(spec.ServiceType)

	labels := map[string]string{
		"app":                  spec.Name,
		"idp.platform/managed": "true",
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceType(serviceType),
			Selector: map[string]string{"app": spec.Name},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       spec.Port,
					TargetPort: intstr.FromInt32(spec.Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	created, err := c.Clientset.CoreV1().Services(spec.Namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}

	return toWorkloadServiceInfo(created), nil
}

// GetWorkloadService returns connection details for a deployment's Service.
// A missing Service is not an error: deployments created before services were
// introduced simply have no routing information to report.
func (c *Client) GetWorkloadService(ctx context.Context, namespace, name string) *WorkloadServiceInfo {
	service, err := c.Clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	return toWorkloadServiceInfo(service)
}

// DeleteWorkloadService removes a deployment's Service, ignoring an already
// absent one so deleting a pre-existing deployment still succeeds.
func (c *Client) DeleteWorkloadService(ctx context.Context, namespace, name string) error {
	err := c.Clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func toWorkloadServiceInfo(s *corev1.Service) *WorkloadServiceInfo {
	info := &WorkloadServiceInfo{
		Type:      string(s.Spec.Type),
		ClusterIP: s.Spec.ClusterIP,
	}

	if len(s.Spec.Ports) > 0 {
		info.Port = s.Spec.Ports[0].Port
		info.NodePort = s.Spec.Ports[0].NodePort
	}

	info.ClusterAddress = fmt.Sprintf("%s.%s.svc.cluster.local:%d", s.Name, s.Namespace, info.Port)
	return info
}
