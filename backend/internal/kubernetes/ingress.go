package kubernetes

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// DefaultIngressDomain is the suffix for generated hostnames.
const DefaultIngressDomain = "idp.local"

// DefaultIngressClass matches the ingress-nginx controller minikube enables
// with `minikube addons enable ingress`.
const DefaultIngressClass = "nginx"

// dnsLabelRegex matches one RFC 1123 DNS label, the unit a hostname is built
// from. Kubernetes rejects an Ingress whose host is not a valid DNS name, and
// the rejection message does not say which label was at fault.
var dnsLabelRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// IngressConfig holds the platform-wide ingress settings.
type IngressConfig struct {
	// Enabled turns automatic Ingress creation on. Disabled on clusters with no
	// ingress controller, where every Ingress would sit unrouted and misleading.
	Enabled bool
	// Domain is the suffix appended to <app>.<project>.
	Domain string
	// Class is the IngressClass name, e.g. "nginx".
	Class string
	// TLSSecretName, when set, is attached to every generated Ingress. Empty
	// serves plain HTTP, which is the honest default for a .local domain with
	// no certificate authority behind it.
	TLSSecretName string
}

// Normalize fills in defaults for unset fields.
func (c IngressConfig) Normalize() IngressConfig {
	c.Domain = strings.ToLower(strings.TrimSpace(c.Domain))
	c.Class = strings.TrimSpace(c.Class)
	if c.Domain == "" {
		c.Domain = DefaultIngressDomain
	}
	if c.Class == "" {
		c.Class = DefaultIngressClass
	}
	return c
}

// Scheme reports the URL scheme generated links should use.
func (c IngressConfig) Scheme() string {
	if c.TLSSecretName != "" {
		return "https"
	}
	return "http"
}

// IngressSpec describes the Ingress fronting one workload.
type IngressSpec struct {
	Namespace string
	Name      string
	// Host is the fully-qualified hostname to serve.
	Host string
	// ServicePort is the port on the workload's Service.
	ServicePort int32
	Labels      map[string]string
}

// BuildIngressHost renders <app>.<scope>.<domain>.
//
// scope is the project slug when the namespace belongs to a project, and the
// namespace name otherwise — an unattached namespace must still get a working
// URL rather than a malformed one containing an empty label.
func BuildIngressHost(app, scope, domain string) (string, error) {
	app = strings.ToLower(strings.TrimSpace(app))
	scope = strings.ToLower(strings.TrimSpace(scope))
	domain = strings.ToLower(strings.TrimSpace(domain))

	if domain == "" {
		domain = DefaultIngressDomain
	}
	if !dnsLabelRegex.MatchString(app) {
		return "", fmt.Errorf("cannot build hostname from app name %q", app)
	}
	if scope != "" && !dnsLabelRegex.MatchString(scope) {
		return "", fmt.Errorf("cannot build hostname from scope %q", scope)
	}

	host := app + "." + domain
	if scope != "" {
		host = app + "." + scope + "." + domain
	}
	return ValidateHostname(host)
}

// ValidateHostname checks a user-supplied hostname before it reaches the API
// server, so a typo produces a readable error instead of an admission failure.
func ValidateHostname(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimSuffix(host, ".")

	if host == "" {
		return "", fmt.Errorf("hostname is required")
	}
	if len(host) > 253 {
		return "", fmt.Errorf("hostname must be at most 253 characters")
	}
	// A scheme, port or path is a common paste accident, and the API server
	// rejects it with a message that never mentions the extra characters.
	if strings.ContainsAny(host, "/:@ ") {
		return "", fmt.Errorf("hostname %q must be a bare host, without scheme, port or path", raw)
	}

	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("hostname %q must contain at least one dot", raw)
	}
	for _, label := range labels {
		if len(label) > 63 {
			return "", fmt.Errorf("hostname label %q exceeds 63 characters", label)
		}
		if !dnsLabelRegex.MatchString(label) {
			return "", fmt.Errorf("invalid hostname label %q in %q", label, raw)
		}
	}
	return host, nil
}

// EnsureIngress creates or updates the Ingress for a workload.
func (c *Client) EnsureIngress(ctx context.Context, cfg IngressConfig, spec IngressSpec) error {
	cfg = cfg.Normalize()

	labels := map[string]string{
		"app":          spec.Name,
		LabelManagedBy: "true",
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}

	pathType := netv1.PathTypePrefix
	className := cfg.Class
	desired := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    labels,
		},
		Spec: netv1.IngressSpec{
			IngressClassName: &className,
			Rules: []netv1.IngressRule{
				{
					Host: spec.Host,
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: spec.Name,
											Port: netv1.ServiceBackendPort{Number: spec.ServicePort},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if cfg.TLSSecretName != "" {
		desired.Spec.TLS = []netv1.IngressTLS{
			{Hosts: []string{spec.Host}, SecretName: cfg.TLSSecretName},
		}
	}

	api := c.Clientset.NetworkingV1().Ingresses(spec.Namespace)
	_, err := api.Create(ctx, desired, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create ingress %s/%s: %w", spec.Namespace, spec.Name, err)
	}

	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, getErr := api.Get(ctx, spec.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Spec = desired.Spec
		existing.Labels = mergeLabels(existing.Labels, labels)
		_, upErr := api.Update(ctx, existing, metav1.UpdateOptions{})
		return upErr
	})
	if updateErr != nil {
		return fmt.Errorf("update ingress %s/%s: %w", spec.Namespace, spec.Name, updateErr)
	}
	return nil
}

// GetIngressHost returns the hostname an Ingress serves, or "" when there is
// none. A missing Ingress is not an error: workloads created before this
// feature, and those deployed with ingress disabled, simply have no URL.
func (c *Client) GetIngressHost(ctx context.Context, namespace, name string) string {
	ing, err := c.Clientset.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || len(ing.Spec.Rules) == 0 {
		return ""
	}
	return ing.Spec.Rules[0].Host
}

// DeleteIngress removes a workload's Ingress, ignoring an absent one.
func (c *Client) DeleteIngress(ctx context.Context, namespace, name string) error {
	err := c.Clientset.NetworkingV1().Ingresses(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete ingress %s/%s: %w", namespace, name, err)
	}
	return nil
}
