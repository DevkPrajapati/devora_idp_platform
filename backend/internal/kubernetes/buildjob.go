package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultKanikoImage builds images from inside the cluster without a Docker
// daemon. Kaniko is used rather than docker-in-docker because the latter needs
// a privileged container, which is a far larger blast radius for a build that
// runs arbitrary repository code.
const DefaultKanikoImage = "gcr.io/kaniko-project/executor:v1.23.2"

// LabelBuildID ties a Job back to its build row.
const LabelBuildID = "idp.platform/build-id"

// buildJobTTL lets Kubernetes garbage-collect finished build Jobs. Without it
// every build leaves a completed Job and its pod behind forever.
const buildJobTTL int32 = 3600

// buildActiveDeadline caps a build's wall-clock time. A build hanging on a
// network fetch would otherwise occupy cluster capacity indefinitely.
const buildActiveDeadline int64 = 30 * 60

// BuildJobSpec describes one image build.
type BuildJobSpec struct {
	// Namespace the build Job runs in. Kept separate from application
	// namespaces so build pods cannot reach application Secrets.
	Namespace string
	// JobName is the Kubernetes Job name, unique per build.
	JobName string
	// BuildID links the Job back to its database row.
	BuildID string

	CloneURL string
	Ref      string
	// GitProvider selects how GitToken is embedded in the clone URL (github,
	// gitlab, bitbucket, generic).
	GitProvider string
	// GitToken authenticates a private clone. Empty for public repositories.
	GitToken string

	DockerfilePath string
	BuildContext   string
	// Destination is the fully-qualified image reference including tag.
	Destination string
	// RegistrySecretName is the dockerconfigjson Secret used for the push,
	// created by the registry credentials feature.
	RegistrySecretName string

	KanikoImage string
	Resources   ResourceSpec
}

// gitTokenSecretName holds the clone token for one build.
func gitTokenSecretName(jobName string) string { return jobName + "-git" }

// BuildCloneURL injects a token into an HTTPS clone URL.
//
// Kaniko's git context reads credentials from the URL's userinfo. The assembled
// URL is therefore only ever stored in a Secret and injected as an environment
// variable — never written into the Job spec, where `kubectl get job -o yaml`
// would expose the token.
func BuildCloneURL(rawURL, token, provider string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("clone url is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid clone url: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("clone url must use https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("clone url has no host")
	}
	// A URL that already carries credentials would have them silently
	// overwritten, or worse, persisted in the database in the clear.
	if parsed.User != nil {
		return "", fmt.Errorf("clone url must not contain credentials; supply a token instead")
	}

	if token == "" {
		return parsed.String(), nil
	}
	user, pass := gitCredentialsForProvider(provider, token)
	parsed.User = url.UserPassword(user, pass)
	return parsed.String(), nil
}

// gitCredentialsForProvider returns the userinfo Kaniko's git context expects.
func gitCredentialsForProvider(provider, token string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gitlab":
		return "oauth2", token
	case "bitbucket":
		return "x-token-auth", token
	default:
		// GitHub classic (ghp_) and fine-grained (github_pat_) PATs both
		// authenticate as username x-access-token with the token as password.
		// Putting the PAT in the username slot (old x-oauth-basic pattern)
		// fails for fine-grained tokens and shows up as "clone failed".
		return "x-access-token", token
	}
}

// NormalizeGitRef turns a branch name into the full ref Kaniko expects.
func NormalizeGitRef(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "refs/heads/main"
	}
	if strings.HasPrefix(branch, "refs/") {
		return branch
	}
	return "refs/heads/" + branch
}

// CreateBuildJob creates the git-token Secret and the Kaniko Job.
func (c *Client) CreateBuildJob(ctx context.Context, spec BuildJobSpec) error {
	if spec.KanikoImage == "" {
		spec.KanikoImage = DefaultKanikoImage
	}

	labels := map[string]string{
		LabelManagedBy: "true",
		LabelBuildID:   spec.BuildID,
		"app":          "idp-build",
	}

	// The token lives in its own Secret, deleted alongside the Job, so it does
	// not linger after the build finishes. Kaniko reads GIT_USERNAME and
	// GIT_PASSWORD for git contexts — more reliable than embedding creds in the URL.
	if spec.GitToken != "" {
		user, pass := gitCredentialsForProvider(spec.GitProvider, spec.GitToken)
		// Keep clone-url credential-free. Kaniko authenticates via
		// GIT_USERNAME / GIT_PASSWORD. Embedding a fine-grained PAT in the
		// URL can break when the token needs percent-encoding, and duplicates
		// auth with the env vars.
		publicPath, err := BuildCloneURL(spec.CloneURL, "", spec.GitProvider)
		if err != nil {
			return err
		}
		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gitTokenSecretName(spec.JobName),
				Namespace: spec.Namespace,
				Labels:    labels,
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"clone-url":    stripScheme(publicPath),
				"git-username": user,
				"git-password": pass,
			},
		}
		if _, err := c.Clientset.CoreV1().Secrets(spec.Namespace).
			Create(ctx, tokenSecret, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create git token secret: %w", err)
		}
	}

	job, err := buildKanikoJob(spec, labels)
	if err != nil {
		return err
	}

	if _, err := c.Clientset.BatchV1().Jobs(spec.Namespace).
		Create(ctx, job, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil // idempotent: the reconciler may retry after a restart
		}
		_ = c.deleteGitTokenSecret(ctx, spec.Namespace, spec.JobName)
		return fmt.Errorf("create build job: %w", err)
	}
	return nil
}

func buildKanikoJob(spec BuildJobSpec, labels map[string]string) (*batchv1.Job, error) {
	if spec.Destination == "" {
		return nil, fmt.Errorf("build destination is required")
	}
	public, err := BuildCloneURL(spec.CloneURL, "", spec.GitProvider)
	if err != nil {
		return nil, err
	}

	dockerfile := spec.DockerfilePath
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	buildContext := strings.TrimPrefix(strings.TrimSpace(spec.BuildContext), "./")

	args := []string{
		// $(CLONE_URL) is expanded by the kubelet from the env var below, so a
		// token-bearing URL never appears in the Job spec itself.
		"--context=git://$(CLONE_URL)#" + NormalizeGitRef(spec.Ref),
		"--dockerfile=" + dockerfile,
		"--destination=" + spec.Destination,
		// Layer caching across builds; without it every build re-runs the full
		// dependency install.
		"--cache=true",
		"--verbosity=info",
	}
	if buildContext != "" && buildContext != "." {
		args = append(args, "--context-sub-path="+buildContext)
	}

	env := []corev1.EnvVar{}
	if spec.GitToken != "" {
		secretRef := gitTokenSecretName(spec.JobName)
		env = append(env,
			corev1.EnvVar{
				Name: "CLONE_URL",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretRef},
						Key:                  "clone-url",
					},
				},
			},
			corev1.EnvVar{
				Name: "GIT_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretRef},
						Key:                  "git-username",
					},
				},
			},
			corev1.EnvVar{
				Name: "GIT_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretRef},
						Key:                  "git-password",
					},
				},
			},
		)
	} else {
		// Public clone: the URL is not sensitive, but using the same env var
		// keeps the args identical between the two paths.
		env = append(env, corev1.EnvVar{Name: "CLONE_URL", Value: stripScheme(public)})
	}

	volumes := []corev1.Volume{}
	mounts := []corev1.VolumeMount{}
	if spec.RegistrySecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "docker-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: spec.RegistrySecretName,
					// Kaniko reads /kaniko/.docker/config.json; the Secret's
					// key is .dockerconfigjson, so it is remapped here.
					Items: []corev1.KeyToPath{
						{Key: corev1.DockerConfigJsonKey, Path: "config.json"},
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "docker-config",
			MountPath: "/kaniko/.docker",
			ReadOnly:  true,
		})
	}

	backoffLimit := int32(0) // retries are explicit platform actions, not silent
	ttl := buildJobTTL
	deadline := buildActiveDeadline

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.JobName,
			Namespace: spec.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			ActiveDeadlineSeconds:   &deadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:         "kaniko",
							Image:        spec.KanikoImage,
							Args:         args,
							Env:          env,
							VolumeMounts: mounts,
							Resources:    BuildResourceRequirements(spec.Resources),
						},
					},
					Volumes: volumes,
				},
			},
		},
	}, nil
}

// stripScheme removes the scheme for Kaniko's git:// context prefix.
func stripScheme(raw string) string {
	if idx := strings.Index(raw, "://"); idx >= 0 {
		return raw[idx+3:]
	}
	return raw
}

// BuildJobStatus reports how a build Job is progressing.
type BuildJobStatus struct {
	// Finished is true once the Job has succeeded or failed for good.
	Finished  bool
	Succeeded bool
	// Reason explains a failure, empty on success.
	Reason string
	// PodName is where the build's logs live.
	PodName string
	// Missing is true when the Job no longer exists — usually TTL collection.
	Missing bool
}

// GetBuildJobStatus inspects a build Job.
func (c *Client) GetBuildJobStatus(ctx context.Context, namespace, jobName string) (BuildJobStatus, error) {
	job, err := c.Clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return BuildJobStatus{Missing: true, Finished: true, Reason: "build job no longer exists"}, nil
	}
	if err != nil {
		return BuildJobStatus{}, fmt.Errorf("get build job: %w", err)
	}

	status := BuildJobStatus{PodName: c.buildPodName(ctx, namespace, jobName)}

	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			status.Finished, status.Succeeded = true, true
			return status, nil
		case batchv1.JobFailed:
			status.Finished = true
			// The condition's message is often just "BackoffLimitExceeded",
			// which says nothing about what went wrong inside the build.
			status.Reason = condition.Message
			if status.Reason == "" {
				status.Reason = condition.Reason
			}
			if detail := c.buildFailureDetail(ctx, namespace, status.PodName); detail != "" {
				status.Reason = strings.TrimSpace(status.Reason + ": " + detail)
			}
			return status, nil
		}
	}

	if job.Status.Succeeded > 0 {
		status.Finished, status.Succeeded = true, true
	}
	return status, nil
}

// buildPodName finds the pod backing a Job, which is where its logs are.
func (c *Client) buildPodName(ctx context.Context, namespace, jobName string) string {
	pods, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	return pods.Items[0].Name
}

// buildFailureDetail returns the container's terminated reason, which carries
// the actual cause when a build fails.
func (c *Client) buildFailureDetail(ctx context.Context, namespace, podName string) string {
	if podName == "" {
		return ""
	}
	if line := c.kanikoErrorLine(ctx, namespace, podName); line != "" {
		return humanizeKanikoError(line)
	}
	pod, err := c.Clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			if t.Message != "" {
				return humanizeKanikoError(t.Message)
			}
			return fmt.Sprintf("%s (exit code %d)", t.Reason, t.ExitCode)
		}
	}
	return ""
}

// kanikoErrorLine scans recent logs for the first Kaniko error line. The last
// line is often executor help text, which is useless in the UI.
func (c *Client) kanikoErrorLine(ctx context.Context, namespace, pod string) string {
	tail := int64(200)
	stream, err := c.Clientset.CoreV1().Pods(namespace).
		GetLogs(pod, &corev1.PodLogOptions{TailLines: &tail}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer func() { _ = stream.Close() }()

	data, err := io.ReadAll(io.LimitReader(stream, 256*1024))
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Error:") {
			if len(line) > 300 {
				line = line[:300] + "..."
			}
			return line
		}
	}
	return ""
}

func humanizeKanikoError(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "authorization failed"):
		return "Git clone failed: repository is private or the access token is missing/invalid. Edit the repository and add a valid GitHub PAT with repo read access."
	default:
		return strings.TrimPrefix(line, "Error: ")
	}
}

// DeleteBuildJob removes a Job, its pods and its git token Secret.
func (c *Client) DeleteBuildJob(ctx context.Context, namespace, jobName string) error {
	policy := metav1.DeletePropagationBackground
	err := c.Clientset.BatchV1().Jobs(namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		// Without background propagation the Job is removed while its pods
		// linger as orphans.
		PropagationPolicy: &policy,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete build job: %w", err)
	}
	return c.deleteGitTokenSecret(ctx, namespace, jobName)
}

func (c *Client) deleteGitTokenSecret(ctx context.Context, namespace, jobName string) error {
	err := c.Clientset.CoreV1().Secrets(namespace).
		Delete(ctx, gitTokenSecretName(jobName), metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete git token secret: %w", err)
	}
	return nil
}

// EnsureBuildNamespace creates the namespace build Jobs run in.
func (c *Client) EnsureBuildNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{LabelManagedBy: "true"},
		},
	}
	_, err := c.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create build namespace: %w", err)
	}
	return nil
}

// SetDeploymentImage points a Deployment at a new image, the deploy step of the
// pipeline. Returns false when the image is already current, so an unchanged
// rebuild does not roll pods for nothing.
func (c *Client) SetDeploymentImage(ctx context.Context, namespace, name, image string) (bool, error) {
	api := c.Clientset.AppsV1().Deployments(namespace)

	deployment, err := api.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return false, fmt.Errorf("deployment %s/%s has no containers", namespace, name)
	}
	if deployment.Spec.Template.Spec.Containers[0].Image == image {
		return false, nil
	}

	deployment.Spec.Template.Spec.Containers[0].Image = image
	deployment.Annotations = withChangeCause(deployment.Annotations, "deployed image "+image)

	if _, err := api.Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("update deployment image: %w", err)
	}
	return true, nil
}
