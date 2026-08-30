package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// NamespaceConfig holds parameters for provisioning a tenant namespace.
type NamespaceConfig struct {
	Name        string
	DisplayName string
	OwnerID     string
	OwnerEmail  string
	Labels      map[string]string
	Annotations map[string]string
}

// CreateNamespace provisions a namespace with RBAC, quotas, and network policy.
func (c *Client) CreateNamespace(ctx context.Context, cfg NamespaceConfig) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	labels := map[string]string{
		"idp.platform/managed": "true",
		"idp.platform/owner":   cfg.OwnerID,
	}
	for k, v := range cfg.Labels {
		labels[k] = v
	}

	annotations := map[string]string{
		"idp.platform/display-name": cfg.DisplayName,
		"idp.platform/owner-email":  cfg.OwnerEmail,
	}
	for k, v := range cfg.Annotations {
		annotations[k] = v
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cfg.Name,
			Labels:      labels,
			Annotations: annotations,
		},
	}
	if _, err := cs.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create namespace: %w", err)
	}

	if err := c.applyResourceQuota(ctx, cfg.Name); err != nil {
		return err
	}
	if err := c.applyLimitRange(ctx, cfg.Name); err != nil {
		return err
	}
	if err := c.applyNetworkPolicy(ctx, cfg.Name); err != nil {
		return err
	}
	if err := c.applyOwnerRoleBinding(ctx, cfg.Name, cfg.OwnerID); err != nil {
		return err
	}

	return nil
}

func (c *Client) applyResourceQuota(ctx context.Context, namespace string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idp-default-quota",
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourcePods:                   resource.MustParse("50"),
				corev1.ResourceRequestsCPU:            resource.MustParse("10"),
				corev1.ResourceRequestsMemory:         resource.MustParse("20Gi"),
				corev1.ResourceLimitsCPU:              resource.MustParse("20"),
				corev1.ResourceLimitsMemory:           resource.MustParse("40Gi"),
				corev1.ResourcePersistentVolumeClaims: resource.MustParse("10"),
				corev1.ResourceServices:               resource.MustParse("20"),
			},
		},
	}
	_, err := cs.CoreV1().ResourceQuotas(namespace).Create(ctx, quota, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create resource quota: %w", err)
	}
	return nil
}

func (c *Client) applyLimitRange(ctx context.Context, namespace string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	limit := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idp-default-limits",
			Namespace: namespace,
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Max: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		},
	}
	_, err := cs.CoreV1().LimitRanges(namespace).Create(ctx, limit, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create limit range: %w", err)
	}
	return nil
}

func (c *Client) applyNetworkPolicy(ctx context.Context, namespace string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	policyType := networkingv1.PolicyTypeIngress
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idp-default-deny-ingress",
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{policyType},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{},
						},
					},
				},
			},
		},
	}
	_, err := cs.NetworkingV1().NetworkPolicies(namespace).Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create network policy: %w", err)
	}
	return nil
}

func (c *Client) applyOwnerRoleBinding(ctx context.Context, namespace, ownerID string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idp-owner-binding",
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				Name:     ownerID,
				APIGroup: rbacv1.GroupName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "edit",
		},
	}
	_, err := cs.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create role binding: %w", err)
	}
	return nil
}

// DeleteNamespace removes a namespace from the cluster.
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	if !c.Available() {
		return fmt.Errorf("kubernetes cluster not connected")
	}
	err := cs.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete namespace: %w", err)
	}
	return nil
}

// NamespaceExists checks if a namespace exists in the cluster.
func (c *Client) NamespaceExists(ctx context.Context, name string) (bool, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return false, csErr
	}
	_, err := cs.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, nil
	}
	return true, nil
}

// ListManagedNamespaces returns namespaces managed by the IDP platform.
func (c *Client) ListManagedNamespaces(ctx context.Context) ([]corev1.Namespace, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: "idp.platform/managed=true",
	})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return list.Items, nil
}

// Ping verifies cluster connectivity.
//
// Uses /version rather than listing namespaces: it is one small round trip and
// does not depend on Available(), so a bound-but-unreachable client can still
// be probed after a stop.
func (c *Client) Ping(ctx context.Context) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	_, err := cs.CoreV1().RESTClient().Get().AbsPath("/version").DoRaw(ctx)
	return err
}

// IntstrFromInt32 converts int32 to IntOrString for K8s APIs.
func IntstrFromInt32(val int32) intstr.IntOrString {
	return intstr.FromInt32(val)
}
