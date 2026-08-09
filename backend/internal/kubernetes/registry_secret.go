package kubernetes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// RegistrySecretPrefix namespaces the Secrets this platform owns. Every
// imagePullSecret carrying it is considered platform-managed and may be
// rewritten during reconciliation; anything else a user attached by hand is
// left alone.
const RegistrySecretPrefix = "idp-registry-"

// LabelManagedBy marks resources created by the platform.
const LabelManagedBy = "idp.platform/managed"

// LabelCredential records which stored credential a Secret was rendered from.
const LabelCredential = "idp.platform/registry-credential"

// LabelProject records the owning project slug.
const LabelProject = "idp.platform/project"

// dockerHubAuthKey is the key Docker Hub credentials must be filed under.
// Docker Hub is the one registry whose auth key is not its hostname: both the
// Docker CLI and the kubelet look for this legacy v1 URL, so a credential
// stored under "docker.io" silently fails to authenticate.
const dockerHubAuthKey = "https://index.docker.io/v1/"

// registryHostRegex accepts host[:port] — the form a Docker auth key takes for
// every registry other than Docker Hub.
var registryHostRegex = regexp.MustCompile(`^[a-zA-Z0-9]([-a-zA-Z0-9.]*[a-zA-Z0-9])?(:[0-9]{1,5})?$`)

// dockerHubAliases are the spellings users type for Docker Hub.
var dockerHubAliases = map[string]bool{
	"docker.io":               true,
	"index.docker.io":         true,
	"registry-1.docker.io":    true,
	"registry.hub.docker.com": true,
}

// RegistryCredential is a decrypted credential ready to be rendered into a
// Secret. It exists only in memory and must never be logged or serialised into
// an API response.
type RegistryCredential struct {
	// Name is the platform-level handle, e.g. "dockerhub".
	Name string
	// RegistryURL is whatever the user typed; normalise with NormalizeRegistryHost.
	RegistryURL string
	Username    string
	Password    string
	Email       string
	// ProjectSlug is recorded as a label for traceability.
	ProjectSlug string
}

// RegistrySecretName maps a credential handle to its Secret name.
func RegistrySecretName(credentialName string) string {
	return RegistrySecretPrefix + credentialName
}

// NormalizeRegistryHost reduces user input to the key Docker and the kubelet
// use to look up credentials. It accepts input with or without a scheme and
// with a trailing path, because users paste all three forms.
//
// The path is deliberately dropped: credential lookup is hierarchical, so
// "quay.io" already covers "quay.io/myorg/app", while a path in the key makes
// the entry miss for images pulled from a sibling org.
func NormalizeRegistryHost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("registry url is required")
	}

	if idx := strings.Index(value, "://"); idx >= 0 {
		scheme := strings.ToLower(value[:idx])
		if scheme != "http" && scheme != "https" {
			return "", fmt.Errorf("registry url scheme must be http or https, got %q", scheme)
		}
		value = value[idx+3:]
	}

	// Credentials embedded in the URL are a common paste accident and would
	// silently end up as part of the host key.
	if strings.Contains(value, "@") {
		return "", fmt.Errorf("registry url must not contain credentials")
	}

	host, _, _ := strings.Cut(value, "/")
	host = strings.ToLower(strings.TrimSpace(host))

	if dockerHubAliases[host] {
		return dockerHubAuthKey, nil
	}
	if !registryHostRegex.MatchString(host) {
		return "", fmt.Errorf("invalid registry host %q", host)
	}
	return host, nil
}

// dockerConfigEntry is one registry's credentials inside a docker config file.
type dockerConfigEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
	// Auth is base64(username:password). Older clients and some registries read
	// only this field, so it is always written alongside the split values.
	Auth string `json:"auth"`
}

type dockerConfig struct {
	Auths map[string]dockerConfigEntry `json:"auths"`
}

// BuildDockerConfigJSON renders the .dockerconfigjson payload for a credential.
func BuildDockerConfigJSON(cred RegistryCredential) ([]byte, error) {
	host, err := NormalizeRegistryHost(cred.RegistryURL)
	if err != nil {
		return nil, err
	}
	if cred.Username == "" {
		return nil, fmt.Errorf("registry username is required")
	}
	if cred.Password == "" {
		return nil, fmt.Errorf("registry password is required")
	}

	cfg := dockerConfig{
		Auths: map[string]dockerConfigEntry{
			host: {
				Username: cred.Username,
				Password: cred.Password,
				Email:    cred.Email,
				Auth:     base64.StdEncoding.EncodeToString([]byte(cred.Username + ":" + cred.Password)),
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal docker config: %w", err)
	}
	return data, nil
}

// EnsureRegistrySecret creates or updates the pull Secret for a credential in
// one namespace. It is idempotent so credential edits, namespace additions and
// pre-deploy reconciliation can all call it without special-casing.
func (c *Client) EnsureRegistrySecret(ctx context.Context, namespace string, cred RegistryCredential) error {
	payload, err := BuildDockerConfigJSON(cred)
	if err != nil {
		return err
	}

	name := RegistrySecretName(cred.Name)
	labels := map[string]string{
		LabelManagedBy:  "true",
		LabelCredential: cred.Name,
	}
	if cred.ProjectSlug != "" {
		labels[LabelProject] = cred.ProjectSlug
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{corev1.DockerConfigJsonKey: payload},
	}

	secrets := c.Clientset.CoreV1().Secrets(namespace)
	_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create registry secret %s/%s: %w", namespace, name, err)
	}

	// Update through a conflict retry: a concurrent credential edit for the
	// same project would otherwise fail one of the two writers outright.
	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := secrets.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Type = corev1.SecretTypeDockerConfigJson
		existing.Data = map[string][]byte{corev1.DockerConfigJsonKey: payload}
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		for k, v := range labels {
			existing.Labels[k] = v
		}
		_, upErr := secrets.Update(ctx, existing, metav1.UpdateOptions{})
		return upErr
	})
	if updateErr != nil {
		// A Secret's `type` field is immutable; if a Secret of another type
		// already squats the name, replace it rather than failing forever.
		if apierrors.IsInvalid(updateErr) {
			if delErr := secrets.Delete(ctx, name, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
				return fmt.Errorf("replace registry secret %s/%s: %w", namespace, name, delErr)
			}
			if _, createErr := secrets.Create(ctx, secret, metav1.CreateOptions{}); createErr != nil {
				return fmt.Errorf("recreate registry secret %s/%s: %w", namespace, name, createErr)
			}
			return nil
		}
		return fmt.Errorf("update registry secret %s/%s: %w", namespace, name, updateErr)
	}
	return nil
}

// DeleteRegistrySecret removes a credential's Secret from a namespace. An
// already-absent Secret is not an error, so deleting a credential that was
// never materialised in some namespace still succeeds.
func (c *Client) DeleteRegistrySecret(ctx context.Context, namespace, credentialName string) error {
	name := RegistrySecretName(credentialName)
	err := c.Clientset.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete registry secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ListManagedRegistrySecrets returns the platform-managed pull Secret names
// present in a namespace, sorted for deterministic output.
func (c *Client) ListManagedRegistrySecrets(ctx context.Context, namespace string) ([]string, error) {
	list, err := c.Clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: LabelManagedBy + "=true",
	})
	if err != nil {
		return nil, fmt.Errorf("list registry secrets: %w", err)
	}

	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		item := &list.Items[i]
		if item.Type == corev1.SecretTypeDockerConfigJson && strings.HasPrefix(item.Name, RegistrySecretPrefix) {
			names = append(names, item.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// CopyRegistrySecret replicates a dockerconfigjson Secret into another
// namespace, searching the namespaces the platform manages for the source.
//
// Build Jobs run in their own namespace and cannot read Secrets from the
// application namespaces where the registry feature materialised them, so the
// push credential has to be copied rather than referenced.
func (c *Client) CopyRegistrySecret(ctx context.Context, secretName, targetNamespace string) error {
	source, err := c.findRegistrySecret(ctx, secretName)
	if err != nil {
		return err
	}

	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: targetNamespace,
			Labels: map[string]string{
				LabelManagedBy:  "true",
				LabelCredential: source.Labels[LabelCredential],
			},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: source.Data[corev1.DockerConfigJsonKey],
		},
	}

	api := c.Clientset.CoreV1().Secrets(targetNamespace)
	_, createErr := api.Create(ctx, desired, metav1.CreateOptions{})
	if createErr == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("copy registry secret to %s: %w", targetNamespace, createErr)
	}

	// Refreshed on every build so a rotated credential reaches the builder
	// rather than the copy going stale.
	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := api.Get(ctx, secretName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Data = desired.Data
		existing.Type = corev1.SecretTypeDockerConfigJson
		existing.Labels = mergeLabels(existing.Labels, desired.Labels)
		_, upErr := api.Update(ctx, existing, metav1.UpdateOptions{})
		return upErr
	})
	if updateErr != nil {
		return fmt.Errorf("refresh registry secret in %s: %w", targetNamespace, updateErr)
	}
	return nil
}

// findRegistrySecret locates a managed pull Secret by name in any namespace.
func (c *Client) findRegistrySecret(ctx context.Context, secretName string) (*corev1.Secret, error) {
	list, err := c.Clientset.CoreV1().Secrets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		LabelSelector: LabelManagedBy + "=true",
	})
	if err != nil {
		return nil, fmt.Errorf("search for registry secret %q: %w", secretName, err)
	}

	for i := range list.Items {
		item := &list.Items[i]
		if item.Name == secretName && item.Type == corev1.SecretTypeDockerConfigJson {
			return item, nil
		}
	}
	return nil, fmt.Errorf(
		"secret %q not found; save the registry credential and attach a namespace to the project first", secretName)
}

// MergeImagePullSecrets returns the pull-secret list a pod template should
// carry: every name the platform manages, plus any entry a user added by hand.
// Managed entries that no longer exist are dropped, which is how a deleted
// credential stops being referenced.
func MergeImagePullSecrets(existing []corev1.LocalObjectReference, managed []string) []corev1.LocalObjectReference {
	merged := make([]corev1.LocalObjectReference, 0, len(existing)+len(managed))
	seen := make(map[string]bool, len(existing)+len(managed))

	for _, ref := range existing {
		if strings.HasPrefix(ref.Name, RegistrySecretPrefix) || seen[ref.Name] {
			continue
		}
		seen[ref.Name] = true
		merged = append(merged, ref)
	}
	for _, name := range managed {
		if seen[name] {
			continue
		}
		seen[name] = true
		merged = append(merged, corev1.LocalObjectReference{Name: name})
	}
	return merged
}

// SyncImagePullSecrets rewrites imagePullSecrets on every platform-managed
// Deployment in a namespace to match the current credential set.
//
// This is what makes credential changes take effect without recreating the
// namespace: a Deployment created before any credential existed carries no
// imagePullSecrets, and updating the Secret alone would never reach it.
// Returns the number of Deployments actually changed.
func (c *Client) SyncImagePullSecrets(ctx context.Context, namespace string, managed []string) (int, error) {
	deployments := c.Clientset.AppsV1().Deployments(namespace)
	list, err := deployments.List(ctx, metav1.ListOptions{LabelSelector: LabelManagedBy + "=true"})
	if err != nil {
		return 0, fmt.Errorf("list deployments for pull secret sync: %w", err)
	}

	updated := 0
	for i := range list.Items {
		name := list.Items[i].Name

		changed := false
		syncErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, getErr := deployments.Get(ctx, name, metav1.GetOptions{})
			if getErr != nil {
				return getErr
			}

			desired := MergeImagePullSecrets(current.Spec.Template.Spec.ImagePullSecrets, managed)
			if sameImagePullSecrets(current.Spec.Template.Spec.ImagePullSecrets, desired) {
				changed = false
				return nil
			}

			current.Spec.Template.Spec.ImagePullSecrets = desired
			if _, upErr := deployments.Update(ctx, current, metav1.UpdateOptions{}); upErr != nil {
				return upErr
			}
			changed = true
			return nil
		})
		if syncErr != nil {
			if apierrors.IsNotFound(syncErr) {
				continue // deleted while we were reconciling
			}
			return updated, fmt.Errorf("sync pull secrets on %s/%s: %w", namespace, name, syncErr)
		}
		if changed {
			updated++
		}
	}
	return updated, nil
}

func sameImagePullSecrets(a, b []corev1.LocalObjectReference) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

// applyImagePullSecrets attaches pull secrets to a Deployment being built.
func applyImagePullSecrets(deployment *appsv1.Deployment, managed []string) {
	if len(managed) == 0 {
		return
	}
	deployment.Spec.Template.Spec.ImagePullSecrets = MergeImagePullSecrets(
		deployment.Spec.Template.Spec.ImagePullSecrets, managed,
	)
}
