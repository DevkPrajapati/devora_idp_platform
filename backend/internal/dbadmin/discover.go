package dbadmin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Instance is one database workload found in the cluster.
type Instance struct {
	Namespace string `json:"namespace"`
	// Name is the pod's owning workload name, which is also the Service name
	// for anything the platform deployed.
	Name   string `json:"name"`
	Engine Engine `json:"engine"`
	// EngineName is the human label, so the UI does not map enum to text.
	EngineName string `json:"engineName"`
	Image      string `json:"image"`
	// PodName is the concrete pod, needed for exec and port-forward.
	PodName string `json:"podName"`
	// Container is the container inside the pod running the engine. A pod with
	// a sidecar has more than one, and exec must name the right one.
	Container string `json:"container"`
	Port      int32  `json:"port"`
	// Ready reports whether the pod is currently serving. An unready database
	// can be listed but not inspected.
	Ready bool `json:"ready"`
	// ServiceName is the in-cluster DNS name, empty when no Service selects
	// this pod.
	ServiceName string `json:"serviceName,omitempty"`
	// PersistentVolumeClaims are the PVCs backing this database. This is what
	// connects the feature to storage: a database with no PVC loses everything
	// on restart, which is worth showing.
	PersistentVolumeClaims []string `json:"persistentVolumeClaims"`
	// CredentialsResolved reports whether the platform found usable
	// credentials. False means the instance is listed but cannot be inspected,
	// which is a more useful answer than omitting it.
	CredentialsResolved bool   `json:"credentialsResolved"`
	CredentialsHint     string `json:"credentialsHint,omitempty"`
}

// Ref identifies one instance for a follow-up operation.
type Ref struct {
	Namespace string
	PodName   string
	Container string
	Port      int32
}

// Discoverer finds database workloads.
type Discoverer struct {
	clientset kubernetes.Interface
}

// NewDiscoverer creates a discoverer. Returns nil when no cluster is attached.
func NewDiscoverer(clientset kubernetes.Interface) *Discoverer {
	if clientset == nil {
		return nil
	}
	return &Discoverer{clientset: clientset}
}

// List returns every database workload in a namespace, or across all
// namespaces when namespace is empty.
func (d *Discoverer) List(ctx context.Context, namespace string) ([]Instance, error) {
	if d == nil {
		return nil, fmt.Errorf("kubernetes cluster not connected")
	}

	pods, err := d.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	// Fetched once per call rather than per pod: a namespace with fifty
	// databases would otherwise trigger fifty identical Service listings.
	services, err := d.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	// One row per workload (Deployment/StatefulSet), not per pod. Multiple
	// Mongo/Postgres pods behind one Service are not independent databases —
	// listing each pod made the UI open an empty replica while writes landed
	// on another.
	byWorkload := make(map[string]Instance)
	for i := range pods.Items {
		pod := &pods.Items[i]
		// A terminated pod's image still matches, but it has no server to talk
		// to and would appear as a permanently broken entry.
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		container, engine, found := matchContainer(pod)
		if !found {
			continue
		}

		instance := Instance{
			Namespace:              pod.Namespace,
			Name:                   workloadName(pod),
			Engine:                 engine,
			Image:                  container.Image,
			PodName:                pod.Name,
			Container:              container.Name,
			Ready:                  podReady(pod),
			PersistentVolumeClaims: claimsOf(pod),
		}
		if spec, ok := SpecFor(engine); ok {
			instance.EngineName = spec.DisplayName
			instance.Port = containerPort(container, spec.DefaultPort)
		}
		instance.ServiceName = serviceFor(pod, services.Items)

		// Resolved eagerly so the list can show which entries are actionable.
		// Only the presence of credentials is reported — never the values.
		ref := Ref{
			Namespace: instance.Namespace,
			PodName:   instance.PodName,
			Container: instance.Container,
			Port:      instance.Port,
		}
		if _, err := d.Credentials(ctx, ref, engine); err != nil {
			instance.CredentialsHint = err.Error()
		} else {
			instance.CredentialsResolved = true
		}

		key := instance.Namespace + "/" + instance.Name
		if existing, ok := byWorkload[key]; ok {
			byWorkload[key] = preferInstance(existing, instance)
			continue
		}
		byWorkload[key] = instance
	}

	instances := make([]Instance, 0, len(byWorkload))
	for _, instance := range byWorkload {
		instances = append(instances, instance)
	}

	// Stable output so the UI does not reshuffle rows between polls.
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Namespace != instances[j].Namespace {
			return instances[i].Namespace < instances[j].Namespace
		}
		return instances[i].Name < instances[j].Name
	})

	return instances, nil
}

// preferInstance keeps the more useful pod when several back the same workload.
func preferInstance(a, b Instance) Instance {
	if b.Ready && !a.Ready {
		return b
	}
	if a.Ready && !b.Ready {
		return a
	}
	if b.CredentialsResolved && !a.CredentialsResolved {
		return b
	}
	// Stable tie-break so the chosen pod does not flip between polls.
	if b.PodName < a.PodName {
		return b
	}
	return a
}

// Get returns one instance by namespace and workload name.
func (d *Discoverer) Get(ctx context.Context, namespace, name string) (*Instance, error) {
	instances, err := d.List(ctx, namespace)
	if err != nil {
		return nil, err
	}
	for i := range instances {
		if instances[i].Name == name || instances[i].PodName == name {
			return &instances[i], nil
		}
	}
	return nil, fmt.Errorf("no database workload %q in namespace %q", name, namespace)
}

// Credentials resolves connection credentials from the pod's environment.
//
// Values supplied inline are read directly; values coming from a secretKeyRef
// are followed into the Secret. This is why the platform needs no credential
// registration step — the pod already declares everything required, and reading
// it there means the platform cannot drift out of sync with a rotated password.
func (d *Discoverer) Credentials(ctx context.Context, ref Ref, engine Engine) (Credentials, error) {
	spec, ok := SpecFor(engine)
	if !ok {
		return Credentials{}, fmt.Errorf("unsupported engine %q", engine)
	}

	pod, err := d.clientset.CoreV1().Pods(ref.Namespace).Get(ctx, ref.PodName, metav1.GetOptions{})
	if err != nil {
		return Credentials{}, fmt.Errorf("read pod: %w", err)
	}

	container := containerNamed(pod, ref.Container)
	if container == nil {
		return Credentials{}, fmt.Errorf("container %q not found in pod %q", ref.Container, ref.PodName)
	}

	creds := Credentials{
		User:     d.envValue(ctx, pod.Namespace, container, spec.UserEnv),
		Password: d.envValue(ctx, pod.Namespace, container, spec.PasswordEnv),
		Database: d.envValue(ctx, pod.Namespace, container, spec.DatabaseEnv),
	}

	if creds.User == "" {
		creds.User = spec.DefaultUser
	}
	if creds.Database == "" {
		creds.Database = spec.DefaultDatabase
	}

	// Mongo without root credentials is a legitimate unauthenticated server;
	// the SQL engines always need a password in practice, and naming the
	// variables we looked for is far more actionable than a refused connection.
	if creds.Password == "" && engine != EngineMongoDB {
		return Credentials{}, fmt.Errorf(
			"no password found; expected one of %s in the pod environment",
			strings.Join(spec.PasswordEnv, ", "))
	}

	return creds, nil
}

// envValue returns the first candidate variable that has a value.
//
// Kubernetes applies envFrom first, then lets individual env entries override.
// Platform-managed workloads put MONGO_INITDB_* (and Postgres/MySQL analogues)
// in a Secret/ConfigMap referenced by envFrom, so skipping envFrom made inspect
// connect with no credentials while the server required auth.
func (d *Discoverer) envValue(
	ctx context.Context,
	namespace string,
	container *corev1.Container,
	candidates []string,
) string {
	for _, name := range candidates {
		if v := d.envFromNamed(ctx, namespace, container, name); v != "" {
			return v
		}
		if v := d.envFromRefs(ctx, namespace, container, name); v != "" {
			return v
		}
	}
	return ""
}

func (d *Discoverer) envFromNamed(
	ctx context.Context,
	namespace string,
	container *corev1.Container,
	name string,
) string {
	for i := range container.Env {
		env := &container.Env[i]
		if env.Name != name {
			continue
		}
		if env.Value != "" {
			return env.Value
		}
		if env.ValueFrom == nil {
			continue
		}
		if ref := env.ValueFrom.SecretKeyRef; ref != nil {
			if v := d.secretValue(ctx, namespace, ref.Name, ref.Key); v != "" {
				return v
			}
		}
		if ref := env.ValueFrom.ConfigMapKeyRef; ref != nil {
			if v := d.configMapValue(ctx, namespace, ref.Name, ref.Key); v != "" {
				return v
			}
		}
	}
	return ""
}

func (d *Discoverer) envFromRefs(
	ctx context.Context,
	namespace string,
	container *corev1.Container,
	name string,
) string {
	// Later envFrom sources override earlier ones for the same key.
	for i := len(container.EnvFrom) - 1; i >= 0; i-- {
		src := container.EnvFrom[i]
		key := name
		if src.Prefix != "" {
			if !strings.HasPrefix(name, src.Prefix) {
				continue
			}
			key = strings.TrimPrefix(name, src.Prefix)
		}
		if src.SecretRef != nil {
			if v := d.secretValue(ctx, namespace, src.SecretRef.Name, key); v != "" {
				return v
			}
		}
		if src.ConfigMapRef != nil {
			if v := d.configMapValue(ctx, namespace, src.ConfigMapRef.Name, key); v != "" {
				return v
			}
		}
	}
	return ""
}

func (d *Discoverer) secretValue(ctx context.Context, namespace, name, key string) string {
	secret, err := d.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	// Secret data arrives already base64-decoded by client-go.
	return string(secret.Data[key])
}

func (d *Discoverer) configMapValue(ctx context.Context, namespace, name, key string) string {
	cm, err := d.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	return cm.Data[key]
}

// matchContainer finds the first container in a pod running a known engine.
func matchContainer(pod *corev1.Pod) (*corev1.Container, Engine, bool) {
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		if engine, ok := EngineForImage(container.Image); ok {
			return container, engine, true
		}
	}
	return nil, "", false
}

func containerNamed(pod *corev1.Pod, name string) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

// containerPort prefers a port the container declares over the engine default,
// so a server moved off its standard port is still reachable.
func containerPort(container *corev1.Container, fallback int32) int32 {
	for _, port := range container.Ports {
		if port.ContainerPort > 0 {
			return port.ContainerPort
		}
	}
	return fallback
}

// workloadName strips the ReplicaSet and pod hash suffixes so the UI shows
// "postgres" rather than "postgres-7d9f8b6c4d-x2k9p".
func workloadName(pod *corev1.Pod) string {
	for _, owner := range pod.OwnerReferences {
		if owner.Controller == nil || !*owner.Controller {
			continue
		}
		switch owner.Kind {
		case "StatefulSet", "DaemonSet":
			return owner.Name
		case "ReplicaSet":
			// A ReplicaSet is named <deployment>-<hash>; trimming the last
			// segment recovers the Deployment name.
			if idx := strings.LastIndex(owner.Name, "-"); idx > 0 {
				return owner.Name[:idx]
			}
			return owner.Name
		}
	}
	if app := pod.Labels["app"]; app != "" {
		return app
	}
	return pod.Name
}

func podReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// claimsOf lists the PVCs a pod mounts — the durable storage behind the data.
func claimsOf(pod *corev1.Pod) []string {
	claims := make([]string, 0)
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			claims = append(claims, volume.PersistentVolumeClaim.ClaimName)
		}
	}
	return claims
}

// serviceFor returns the name of a Service whose selector matches the pod.
func serviceFor(pod *corev1.Pod, services []corev1.Service) string {
	for i := range services {
		selector := services[i].Spec.Selector
		if len(selector) == 0 {
			// A selectorless Service targets manual Endpoints; treating its
			// empty selector as a wildcard would match every pod.
			continue
		}
		matches := true
		for key, value := range selector {
			if pod.Labels[key] != value {
				matches = false
				break
			}
		}
		if matches {
			return services[i].Name
		}
	}
	return ""
}
