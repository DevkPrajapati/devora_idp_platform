package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// AnnotationConfigChecksum carries a hash of the workload's configuration.
//
// Kubernetes does not restart pods when a ConfigMap or Secret changes, and
// envFrom values are only read at container start. Without this annotation on
// the pod template, saving new configuration would report success while every
// running container kept serving the old values. Changing the annotation
// changes the pod template, which is what actually triggers the rollout.
const AnnotationConfigChecksum = "idp.platform/config-checksum"

// envKeyRegex is the C_IDENTIFIER form Kubernetes requires for envFrom keys.
//
// ConfigMaps accept a wider key charset (dots and dashes), but a key that is
// not a valid identifier is *silently skipped* when injected through envFrom —
// the pod starts without the variable and only an event records why. Rejecting
// such keys up front turns a silent misconfiguration into a validation error.
var envKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// maxConfigBytes leaves headroom under the 1 MiB etcd object limit. Exceeding
// it fails the API call with a message that does not say which object was too
// big, so the check happens here instead.
const maxConfigBytes = 512 << 10

// WorkloadConfigMapName is the ConfigMap holding a workload's non-sensitive env.
func WorkloadConfigMapName(app string) string { return app + "-config" }

// WorkloadSecretName is the Secret holding a workload's sensitive env.
func WorkloadSecretName(app string) string { return app + "-secrets" }

// ValidateEnvKey reports whether a variable name can be injected via envFrom.
func ValidateEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("variable name is required")
	}
	if !envKeyRegex.MatchString(key) {
		return fmt.Errorf(
			"invalid variable name %q: use letters, digits and underscore, not starting with a digit", key)
	}
	return nil
}

// ValidateEnvMap validates every key and the combined payload size.
func ValidateEnvMap(vars map[string]string, kind string) error {
	total := 0
	for k, v := range vars {
		if err := ValidateEnvKey(k); err != nil {
			return err
		}
		total += len(k) + len(v)
	}
	if total > maxConfigBytes {
		return fmt.Errorf("%s exceeds the %d KiB limit", kind, maxConfigBytes>>10)
	}
	return nil
}

// ConfigChecksum hashes a workload's full configuration. Both maps feed the
// same hash so a change to either rolls the deployment.
//
// Secret values are hashed, never stored: the digest is one-way, so the
// annotation reveals nothing while still changing whenever a secret changes.
func ConfigChecksum(configVars, secretVars map[string]string) string {
	h := sha256.New()
	for _, m := range []map[string]string{configVars, secretVars} {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		// Map iteration order is random; an unsorted hash would differ between
		// two identical configurations and roll pods on every save.
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = fmt.Fprintf(h, "%s=%s\x00", k, m[k])
		}
		h.Write([]byte("\x01"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func workloadConfigLabels(app string) map[string]string {
	return map[string]string{
		"app":          app,
		LabelManagedBy: "true",
	}
}

// EnsureWorkloadConfig creates or replaces both configuration objects for a
// workload. Both are always written, even when empty, so envFrom always has
// something to reference and their lifecycle matches the Deployment's.
func (c *Client) EnsureWorkloadConfig(ctx context.Context, namespace, app string, configVars, secretVars map[string]string) error {
	if err := c.ensureConfigMap(ctx, namespace, app, configVars); err != nil {
		return err
	}
	return c.ensureSecret(ctx, namespace, app, secretVars)
}

func (c *Client) ensureConfigMap(ctx context.Context, namespace, app string, data map[string]string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	name := WorkloadConfigMapName(app)
	if data == nil {
		data = map[string]string{}
	}

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    workloadConfigLabels(app),
		},
		Data: data,
	}

	api := cs.CoreV1().ConfigMaps(namespace)
	_, err := api.Create(ctx, desired, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create configmap %s/%s: %w", namespace, name, err)
	}

	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := api.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Data = data
		existing.Labels = mergeLabels(existing.Labels, workloadConfigLabels(app))
		_, upErr := api.Update(ctx, existing, metav1.UpdateOptions{})
		return upErr
	})
	if updateErr != nil {
		return fmt.Errorf("update configmap %s/%s: %w", namespace, name, updateErr)
	}
	return nil
}

func (c *Client) ensureSecret(ctx context.Context, namespace, app string, data map[string]string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	name := WorkloadSecretName(app)
	if data == nil {
		data = map[string]string{}
	}

	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    workloadConfigLabels(app),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: data,
	}

	api := cs.CoreV1().Secrets(namespace)
	_, err := api.Create(ctx, desired, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create secret %s/%s: %w", namespace, name, err)
	}

	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := api.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		// Data and StringData both present would have StringData win for
		// overlapping keys while stale Data survived for the rest.
		existing.Data = nil
		existing.StringData = data
		existing.Labels = mergeLabels(existing.Labels, workloadConfigLabels(app))
		_, upErr := api.Update(ctx, existing, metav1.UpdateOptions{})
		return upErr
	})
	if updateErr != nil {
		return fmt.Errorf("update secret %s/%s: %w", namespace, name, updateErr)
	}
	return nil
}

// MergeWorkloadSecret applies a partial update: `set` adds or overwrites keys,
// `remove` deletes them, and every other key keeps its current value.
//
// A full replace is not usable here because the client is never given the
// existing values, so it cannot echo them back — it would wipe every secret the
// user did not retype.
func (c *Client) MergeWorkloadSecret(ctx context.Context, namespace, app string, set map[string]string, remove []string) (map[string]string, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	name := WorkloadSecretName(app)
	api := cs.CoreV1().Secrets(namespace)

	var merged map[string]string
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := api.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			// Nothing to merge into; the caller's `set` is the whole content.
			merged = copyMap(set)
			return c.ensureSecret(ctx, namespace, app, merged)
		}
		if getErr != nil {
			return getErr
		}

		next := make(map[string]string, len(existing.Data)+len(set))
		for k, v := range existing.Data {
			next[k] = string(v)
		}
		for k, v := range set {
			next[k] = v
		}
		for _, k := range remove {
			delete(next, k)
		}

		existing.Data = nil
		existing.StringData = next
		existing.Type = corev1.SecretTypeOpaque
		existing.Labels = mergeLabels(existing.Labels, workloadConfigLabels(app))
		if _, upErr := api.Update(ctx, existing, metav1.UpdateOptions{}); upErr != nil {
			return upErr
		}
		merged = next
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("merge secret %s/%s: %w", namespace, name, err)
	}
	return merged, nil
}

// GetWorkloadConfig returns the non-sensitive values and the sensitive key
// names. Secret values are never part of the result.
func (c *Client) GetWorkloadConfig(ctx context.Context, namespace, app string) (configVars map[string]string, secretKeys []string, err error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, nil, csErr
	}
	configVars = make(map[string]string)
	secretKeys = []string{}

	cm, cmErr := cs.CoreV1().ConfigMaps(namespace).Get(ctx, WorkloadConfigMapName(app), metav1.GetOptions{})
	if cmErr != nil && !apierrors.IsNotFound(cmErr) {
		return nil, nil, fmt.Errorf("get configmap: %w", cmErr)
	}
	if cmErr == nil {
		for k, v := range cm.Data {
			configVars[k] = v
		}
	}

	secret, secretErr := cs.CoreV1().Secrets(namespace).Get(ctx, WorkloadSecretName(app), metav1.GetOptions{})
	if secretErr != nil && !apierrors.IsNotFound(secretErr) {
		return nil, nil, fmt.Errorf("get secret: %w", secretErr)
	}
	if secretErr == nil {
		for k := range secret.Data {
			secretKeys = append(secretKeys, k)
		}
		sort.Strings(secretKeys)
	}

	return configVars, secretKeys, nil
}

// readSecretValues returns the current secret values for checksum computation
// only. Unexported so nothing outside this package can reach the values.
func (c *Client) readSecretValues(ctx context.Context, namespace, app string) map[string]string {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil
	}
	secret, err := cs.CoreV1().Secrets(namespace).Get(ctx, WorkloadSecretName(app), metav1.GetOptions{})
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		out[k] = string(v)
	}
	return out
}

// DeleteWorkloadConfig removes both configuration objects. Absent objects are
// not an error, so deleting a deployment created before this feature works.
func (c *Client) DeleteWorkloadConfig(ctx context.Context, namespace, app string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	cmErr := cs.CoreV1().ConfigMaps(namespace).
		Delete(ctx, WorkloadConfigMapName(app), metav1.DeleteOptions{})
	if cmErr != nil && !apierrors.IsNotFound(cmErr) {
		return fmt.Errorf("delete configmap: %w", cmErr)
	}

	secretErr := cs.CoreV1().Secrets(namespace).
		Delete(ctx, WorkloadSecretName(app), metav1.DeleteOptions{})
	if secretErr != nil && !apierrors.IsNotFound(secretErr) {
		return fmt.Errorf("delete secret: %w", secretErr)
	}
	return nil
}

// ApplyConfigChecksum stamps the pod template so a configuration change rolls
// the deployment, and reports whether anything actually changed.
func (c *Client) ApplyConfigChecksum(ctx context.Context, namespace, app string, configVars map[string]string) (bool, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return false, csErr
	}
	checksum := ConfigChecksum(configVars, c.readSecretValues(ctx, namespace, app))

	api := cs.AppsV1().Deployments(namespace)
	changed := false

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, getErr := api.Get(ctx, app, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if current.Spec.Template.Annotations == nil {
			current.Spec.Template.Annotations = map[string]string{}
		}
		if current.Spec.Template.Annotations[AnnotationConfigChecksum] == checksum {
			changed = false
			return nil
		}
		current.Spec.Template.Annotations[AnnotationConfigChecksum] = checksum
		if _, upErr := api.Update(ctx, current, metav1.UpdateOptions{}); upErr != nil {
			return upErr
		}
		changed = true
		return nil
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("stamp config checksum on %s/%s: %w", namespace, app, err)
	}
	return changed, nil
}

// workloadEnvFrom wires the two configuration objects into a container.
//
// Both are marked optional so a hand-deleted ConfigMap or Secret does not wedge
// every pod in CreateContainerConfigError; the workload starts without those
// variables and the cause is visible in the pod's events.
func workloadEnvFrom(app string) []corev1.EnvFromSource {
	optional := true
	return []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: WorkloadConfigMapName(app)},
				Optional:             &optional,
			},
		},
		{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: WorkloadSecretName(app)},
				Optional:             &optional,
			},
		},
	}
}

func mergeLabels(existing, desired map[string]string) map[string]string {
	if existing == nil {
		existing = map[string]string{}
	}
	for k, v := range desired {
		existing[k] = v
	}
	return existing
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
