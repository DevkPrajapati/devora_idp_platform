package kubernetes

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultContainerPort is used when a deployment request omits a port.
const DefaultContainerPort int32 = 80

// DeploymentSpec holds parameters for creating a deployment.
type DeploymentSpec struct {
	Namespace   string
	Name        string
	Image       string
	Replicas    int32
	Port        int32
	ServiceType string
	// ConfigVars are non-sensitive values, rendered into a ConfigMap.
	ConfigVars map[string]string
	// SecretVars are sensitive values, rendered into a Secret. They are never
	// written into the pod spec, which is the entire point of splitting them
	// out: `kubectl get deployment -o yaml` must not reveal a password.
	SecretVars map[string]string
	Labels     map[string]string
	// ImagePullSecrets names the dockerconfigjson Secrets the kubelet should
	// authenticate with. Empty for public images.
	ImagePullSecrets []string
	// Probes are omitted from the pod spec entirely when left unconfigured.
	ReadinessProbe ProbeSpec
	LivenessProbe  ProbeSpec
	// Resources left empty are absent from the container, so a namespace
	// LimitRange still applies.
	Resources ResourceSpec
	// PersistentVolumeClaim, when set with MountPath, mounts that claim into
	// the container. Used for database data directories.
	PersistentVolumeClaim string
	MountPath             string
	Autoscaling           AutoscalingSpec
}

// DeploymentInfo holds deployment status information.
type DeploymentInfo struct {
	Name              string
	Namespace         string
	Image             string
	Replicas          int32
	ReadyReplicas     int32
	AvailableReplicas int32
	Status            string
	StatusReason      string
	Port              int32
	ServiceType       string
	NodePort          int32
	ClusterIP         string
	ClusterAddress    string
	// EnvVars holds the non-sensitive configuration read back from the
	// workload's ConfigMap.
	EnvVars          map[string]string
	ConfigMapName    string
	SecretName       string
	SecretKeys       []string
	ImagePullSecrets []string
	ReadinessProbe   ProbeSpec
	LivenessProbe    ProbeSpec
	Resources        ResourceSpec
	IngressHost      string
	URL              string
	CreatedAt        time.Time
	Autoscaling      AutoscalingSpec
}

// CreateDeployment creates a new deployment in the specified namespace.
func (c *Client) CreateDeployment(ctx context.Context, spec DeploymentSpec) (*DeploymentInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	replicas := spec.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	port := spec.Port
	if port <= 0 {
		port = DefaultContainerPort
	}

	labels := map[string]string{
		"app":                  spec.Name,
		"idp.platform/managed": "true",
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}

	// Configuration objects are written before the Deployment so the kubelet
	// finds them on the very first pod start; a Deployment created first would
	// briefly run without its configuration.
	if err := c.EnsureWorkloadConfig(ctx, spec.Namespace, spec.Name, spec.ConfigVars, spec.SecretVars); err != nil {
		return nil, err
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": spec.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						AnnotationConfigChecksum: ConfigChecksum(spec.ConfigVars, spec.SecretVars),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  spec.Name,
							Image: spec.Image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: port, Protocol: corev1.ProtocolTCP},
							},
							// envFrom, not env: values live in the ConfigMap and
							// Secret, so the pod spec carries no secret material.
							EnvFrom: workloadEnvFrom(spec.Name),
							// Nil when unconfigured, which omits the field from
							// the pod spec rather than sending an empty probe.
							ReadinessProbe: BuildProbe(spec.ReadinessProbe, port),
							LivenessProbe:  BuildProbe(spec.LivenessProbe, port),
							Resources:      BuildResourceRequirements(spec.Resources),
						},
					},
				},
			},
		},
	}

	applyPersistentVolume(deployment, spec.PersistentVolumeClaim, spec.MountPath)

	// Attached before the Create call: adding pull secrets afterwards would let
	// the kubelet start pulling a private image unauthenticated first, which
	// backs off for minutes before the corrected spec is retried.
	applyImagePullSecrets(deployment, spec.ImagePullSecrets)

	created, err := cs.AppsV1().Deployments(spec.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		// Otherwise a failed create (duplicate name, quota) leaves a ConfigMap
		// and Secret behind that nothing owns and nothing will clean up.
		_ = c.DeleteWorkloadConfig(ctx, spec.Namespace, spec.Name)
		if spec.PersistentVolumeClaim != "" {
			_ = c.DeletePVC(ctx, spec.Namespace, spec.PersistentVolumeClaim)
		}
		return nil, fmt.Errorf("create deployment: %w", err)
	}

	if spec.Autoscaling.Enabled() {
		if err := c.ApplyAutoscaling(ctx, spec.Namespace, spec.Name, spec.Autoscaling); err != nil {
			_ = c.DeleteDeployment(ctx, spec.Namespace, spec.Name)
			return nil, err
		}
	}

	info := toDeploymentInfo(created)
	info.EnvVars = copyMap(spec.ConfigVars)
	info.SecretKeys = sortedKeys(spec.SecretVars)
	info.Autoscaling = spec.Autoscaling
	return info, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GetDeployment retrieves a deployment by name.
func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*DeploymentInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	deployment, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	info := toDeploymentInfo(deployment)
	c.EnrichDeployment(ctx, info)
	return info, nil
}

// EnrichDeployment fills in routing and failure details that need extra API
// calls, so callers report how a workload is reached and why it is unhealthy.
func (c *Client) EnrichDeployment(ctx context.Context, info *DeploymentInfo) {
	if svc := c.GetWorkloadService(ctx, info.Namespace, info.Name); svc != nil {
		info.ServiceType = svc.Type
		info.NodePort = svc.NodePort
		info.ClusterIP = svc.ClusterIP
		info.ClusterAddress = svc.ClusterAddress
		if info.Port == 0 {
			info.Port = svc.Port
		}
	}

	if host := c.GetIngressHost(ctx, info.Namespace, info.Name); host != "" {
		info.IngressHost = host
		info.URL = c.Ingress.Scheme() + "://" + host
	}

	// Configuration now lives outside the pod spec, so it has to be read back
	// from the ConfigMap; toDeploymentInfo alone can no longer see it.
	if configVars, secretKeys, err := c.GetWorkloadConfig(ctx, info.Namespace, info.Name); err == nil {
		for k, v := range configVars {
			info.EnvVars[k] = v
		}
		info.SecretKeys = secretKeys
	}

	if info.Status != "Running" {
		info.StatusReason = c.podStatusReason(ctx, info.Namespace, info.Name)
	}
	info.Autoscaling = c.GetAutoscaling(ctx, info.Namespace, info.Name)
}

// ListDeployments lists deployments in a namespace.
func (c *Client) ListDeployments(ctx context.Context, namespace string) ([]DeploymentInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "idp.platform/managed=true",
	})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}

	result := make([]DeploymentInfo, 0, len(list.Items))
	for i := range list.Items {
		info := toDeploymentInfo(&list.Items[i])
		c.EnrichDeployment(ctx, info)
		result = append(result, *info)
	}
	return result, nil
}

// ScaleDeployment updates the replica count.
func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) (*DeploymentInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	deployment, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	deployment.Spec.Replicas = &replicas
	updated, err := cs.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("scale deployment: %w", err)
	}
	return toDeploymentInfo(updated), nil
}

// RestartDeployment triggers a rolling restart.
func (c *Client) RestartDeployment(ctx context.Context, namespace, name string) (*DeploymentInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	deployment, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	updated, err := cs.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("restart deployment: %w", err)
	}
	return toDeploymentInfo(updated), nil
}

// DeleteDeployment removes a deployment.
func (c *Client) DeleteDeployment(ctx context.Context, namespace, name string) error {
	cs, csErr := c.cs()
	if csErr != nil {
		return csErr
	}
	err := cs.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete deployment: %w", err)
	}
	_ = c.ApplyAutoscaling(ctx, namespace, name, AutoscalingSpec{})
	return nil
}

func toDeploymentInfo(d *appsv1.Deployment) *DeploymentInfo {
	info := &DeploymentInfo{
		Name:              d.Name,
		Namespace:         d.Namespace,
		ReadyReplicas:     d.Status.ReadyReplicas,
		AvailableReplicas: d.Status.AvailableReplicas,
		Status:            deploymentStatus(d),
		EnvVars:           make(map[string]string),
		SecretKeys:        []string{},
		ConfigMapName:     WorkloadConfigMapName(d.Name),
		SecretName:        WorkloadSecretName(d.Name),
		CreatedAt:         d.CreationTimestamp.Time,
	}

	if d.Spec.Replicas != nil {
		info.Replicas = *d.Spec.Replicas
	}

	for _, ref := range d.Spec.Template.Spec.ImagePullSecrets {
		info.ImagePullSecrets = append(info.ImagePullSecrets, ref.Name)
	}

	if len(d.Spec.Template.Spec.Containers) > 0 {
		container := d.Spec.Template.Spec.Containers[0]
		info.Image = container.Image
		// Inline env only appears on deployments created before configuration
		// was split out. Reading it keeps those visible in the UI until they
		// are next updated, at which point they move into the ConfigMap.
		for _, e := range container.Env {
			if e.ValueFrom == nil {
				info.EnvVars[e.Name] = e.Value
			}
		}
		info.ReadinessProbe = probeToSpec(container.ReadinessProbe)
		info.LivenessProbe = probeToSpec(container.LivenessProbe)
		info.Resources = resourceSpecFrom(container.Resources)
		if len(container.Ports) > 0 {
			info.Port = container.Ports[0].ContainerPort
		}
	}

	return info
}

// lastLogLine returns the final non-empty log line of a container, which for a
// crash-looping pod is almost always the error that killed it. Failures are
// swallowed: this is diagnostic detail, never a reason to fail the request.
func (c *Client) lastLogLine(ctx context.Context, namespace, pod string) string {
	cs, csErr := c.cs()
	if csErr != nil {
		return ""
	}
	tail := int64(10)
	stream, err := cs.CoreV1().Pods(namespace).
		GetLogs(pod, &corev1.PodLogOptions{TailLines: &tail}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer func() { _ = stream.Close() }()

	data, err := io.ReadAll(io.LimitReader(stream, 8192))
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			if len(line) > 300 {
				line = line[:300] + "..."
			}
			return line
		}
	}
	return ""
}

// podStatusReason reports why a deployment's pods are not running, e.g.
// ImagePullBackOff for a mistyped image. The Deployment object alone cannot
// express this: it is accepted by the API server long before any image is
// pulled, so a broken image otherwise looks like a healthy deployment.
func (c *Client) podStatusReason(ctx context.Context, namespace, name string) string {
	cs, csErr := c.cs()
	if csErr != nil {
		return ""
	}
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + name,
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}

	for i := range pods.Items {
		for _, cs := range pods.Items[i].Status.ContainerStatuses {
			if cs.Ready {
				continue
			}
			if w := cs.State.Waiting; w != nil && w.Reason != "" {
				if w.Message != "" {
					return w.Reason + ": " + w.Message
				}
				return w.Reason
			}
			if t := cs.State.Terminated; t != nil && t.Reason != "" {
				// A bare "Error" is not actionable. The container's own last log
				// line carries the real cause (config error, missing upstream,
				// bad env var), so surface it alongside the exit code.
				reason := fmt.Sprintf("%s (exit code %d)", t.Reason, t.ExitCode)
				if detail := c.lastLogLine(ctx, namespace, pods.Items[i].Name); detail != "" {
					return reason + ": " + detail
				}
				return reason
			}
		}
	}

	return ""
}

func deploymentStatus(d *appsv1.Deployment) string {
	if d.Status.AvailableReplicas >= d.Status.Replicas && d.Status.Replicas > 0 {
		return "Running"
	}
	if d.Status.UnavailableReplicas > 0 {
		return "Progressing"
	}
	if d.Status.Replicas == 0 {
		return "ScaledToZero"
	}
	return "Pending"
}

func applyPersistentVolume(deployment *appsv1.Deployment, pvcName, mountPath string) {
	if deployment == nil || pvcName == "" || mountPath == "" {
		return
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return
	}

	volumeName := "data"
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
			},
		},
	})
	container := &deployment.Spec.Template.Spec.Containers[0]
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
	})
}

// AttachPVCToDeployment ensures a Deployment mounts the given claim, forces
// replicas to 1 (RWO), and triggers a rollout. Idempotent when already mounted.
func (c *Client) AttachPVCToDeployment(
	ctx context.Context,
	namespace, name, pvcName, mountPath string,
) (patched bool, err error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return false, csErr
	}

	deployment, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get deployment: %w", err)
	}

	changed := false
	one := int32(1)
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		deployment.Spec.Replicas = &one
		changed = true
	}

	alreadyMounted := false
	for _, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvcName {
			alreadyMounted = true
			break
		}
	}
	if !alreadyMounted {
		applyPersistentVolume(deployment, pvcName, mountPath)
		changed = true
	}

	if !changed {
		return false, nil
	}

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["idp.platform/persistence"] = time.Now().UTC().Format(time.RFC3339)

	if _, err := cs.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("update deployment: %w", err)
	}
	return true, nil
}
