package kubernetes

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnsurePVC creates a ReadWriteOnce claim when it does not already exist.
// size must be a Kubernetes quantity (e.g. "5Gi"). An empty storageClass uses
// the cluster default provisioner.
func (c *Client) EnsurePVC(
	ctx context.Context,
	namespace, name, size string,
	labels map[string]string,
) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	size = strings.TrimSpace(size)
	if size == "" {
		return fmt.Errorf("storage size is required")
	}
	qty, err := resource.ParseQuantity(size)
	if err != nil {
		return fmt.Errorf("invalid storage size %q: %w", size, err)
	}

	_, err = cs.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("get pvc: %w", err)
	}

	pvcLabels := map[string]string{
		"idp.platform/managed": "true",
	}
	for k, v := range labels {
		pvcLabels[k] = v
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    pvcLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: qty,
				},
			},
		},
	}

	if _, err := cs.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create pvc: %w", err)
	}
	return nil
}

// DeletePVC removes a claim. Missing claims are ignored.
func (c *Client) DeletePVC(ctx context.Context, namespace, name string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	err := cs.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete pvc: %w", err)
	}
	return nil
}
