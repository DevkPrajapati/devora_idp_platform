package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterNamespace is a live Kubernetes namespace, independent of the IDP
// tenant registry. kubectl get ns is the source of truth.
type ClusterNamespace struct {
	Name        string
	Phase       string
	CreatedAt   time.Time
	Labels      map[string]string
	Managed     bool
	Kind        string
	DisplayName string
}

// NamespaceResource is one object inside a namespace, flattened for a tree UI.
type NamespaceResource struct {
	Kind      string
	Name      string
	Status    string
	Detail    string
	CreatedAt time.Time
}

// ResourceGroup is a labeled bucket (Workloads, Networking, Config, Storage).
type ResourceGroup struct {
	Name  string
	Items []NamespaceResource
}

// NamespaceInventory is the live contents of one namespace.
type NamespaceInventory struct {
	Namespace      ClusterNamespace
	Groups         []ResourceGroup
	TotalResources int32
}

// ClassifyNamespaceKind mirrors how operators read `kubectl get ns`:
// platform-created tenants, reserved kube-* system namespaces, everything else.
func ClassifyNamespaceKind(name string, labels map[string]string) string {
	if labels["idp.platform/managed"] == "true" {
		return "tenant"
	}
	switch name {
	case "kube-system", "kube-public", "kube-node-lease":
		return "system"
	default:
		return "cluster"
	}
}

func clusterNamespaceFrom(ns corev1.Namespace) ClusterNamespace {
	labels := ns.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	display := ns.Annotations["idp.platform/display-name"]
	if display == "" {
		display = ns.Name
	}
	return ClusterNamespace{
		Name:        ns.Name,
		Phase:       string(ns.Status.Phase),
		CreatedAt:   ns.CreationTimestamp.Time,
		Labels:      labels,
		Managed:     labels["idp.platform/managed"] == "true",
		Kind:        ClassifyNamespaceKind(ns.Name, labels),
		DisplayName: display,
	}
}

// ListClusterNamespaces returns every namespace the API server knows about.
func (c *Client) ListClusterNamespaces(ctx context.Context) ([]ClusterNamespace, error) {
	return cacheDo(c, ctx, "cluster-namespaces", func() ([]ClusterNamespace, error) {
		return c.listClusterNamespacesUncached(ctx)
	})
}

func (c *Client) listClusterNamespacesUncached(ctx context.Context) ([]ClusterNamespace, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	out := make([]ClusterNamespace, 0, len(list.Items))
	for _, ns := range list.Items {
		out = append(out, clusterNamespaceFrom(ns))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// GetNamespaceResources lists the objects operators typically inspect inside a
// namespace. Individual kind failures are skipped so a missing CRD or a
// restricted verb cannot hide the rest of the tree.
func (c *Client) GetNamespaceResources(ctx context.Context, name string) (*NamespaceInventory, error) {
	return cacheDo(c, ctx, "ns-resources:"+name, func() (*NamespaceInventory, error) {
		return c.getNamespaceResourcesUncached(ctx, name)
	})
}

func (c *Client) getNamespaceResourcesUncached(ctx context.Context, name string) (*NamespaceInventory, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	ns, err := cs.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("get namespace: %w", err)
	}

	type collected struct {
		group string
		items []NamespaceResource
	}

	collectors := []func() collected{
		func() collected { return collected{"Workloads", c.listWorkloadResources(ctx, name)} },
		func() collected { return collected{"Networking", c.listNetworkingResources(ctx, name)} },
		func() collected { return collected{"Config", c.listConfigResources(ctx, name)} },
		func() collected { return collected{"Storage", c.listStorageResources(ctx, name)} },
	}

	results := make([]collected, len(collectors))
	var wg sync.WaitGroup
	for i, fn := range collectors {
		wg.Add(1)
		go func(i int, fn func() collected) {
			defer wg.Done()
			results[i] = fn()
		}(i, fn)
	}
	wg.Wait()

	groups := make([]ResourceGroup, 0, len(results))
	var total int32
	for _, r := range results {
		sortResources(r.items)
		groups = append(groups, ResourceGroup{Name: r.group, Items: r.items})
		total += int32(len(r.items))
	}

	return &NamespaceInventory{
		Namespace:      clusterNamespaceFrom(*ns),
		Groups:         groups,
		TotalResources: total,
	}, nil
}

func (c *Client) listWorkloadResources(ctx context.Context, namespace string) []NamespaceResource {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil
	}
	var items []NamespaceResource

	if list, err := cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, d := range list.Items {
			items = append(items, deploymentResource(d))
		}
	}
	if list, err := cs.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, s := range list.Items {
			wanted := int32(0)
			if s.Spec.Replicas != nil {
				wanted = *s.Spec.Replicas
			}
			items = append(items, NamespaceResource{
				Kind:      "StatefulSet",
				Name:      s.Name,
				Status:    replicaStatus(s.Status.ReadyReplicas, wanted),
				Detail:    fmt.Sprintf("%d/%d ready", s.Status.ReadyReplicas, wanted),
				CreatedAt: s.CreationTimestamp.Time,
			})
		}
	}
	if list, err := cs.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, d := range list.Items {
			items = append(items, NamespaceResource{
				Kind:      "DaemonSet",
				Name:      d.Name,
				Status:    replicaStatus(d.Status.NumberReady, d.Status.DesiredNumberScheduled),
				Detail:    fmt.Sprintf("%d/%d ready", d.Status.NumberReady, d.Status.DesiredNumberScheduled),
				CreatedAt: d.CreationTimestamp.Time,
			})
		}
	}
	if list, err := cs.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, r := range list.Items {
			wanted := int32(0)
			if r.Spec.Replicas != nil {
				wanted = *r.Spec.Replicas
			}
			items = append(items, NamespaceResource{
				Kind:      "ReplicaSet",
				Name:      r.Name,
				Status:    replicaStatus(r.Status.ReadyReplicas, wanted),
				Detail:    fmt.Sprintf("%d/%d ready", r.Status.ReadyReplicas, wanted),
				CreatedAt: r.CreationTimestamp.Time,
			})
		}
	}
	if list, err := cs.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, j := range list.Items {
			items = append(items, NamespaceResource{
				Kind:      "Job",
				Name:      j.Name,
				Status:    jobStatus(j),
				Detail:    fmt.Sprintf("%d succeeded · %d failed", j.Status.Succeeded, j.Status.Failed),
				CreatedAt: j.CreationTimestamp.Time,
			})
		}
	}
	if list, err := cs.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, j := range list.Items {
			suspended := j.Spec.Suspend != nil && *j.Spec.Suspend
			status := "Scheduled"
			if suspended {
				status = "Suspended"
			}
			items = append(items, NamespaceResource{
				Kind:      "CronJob",
				Name:      j.Name,
				Status:    status,
				Detail:    j.Spec.Schedule,
				CreatedAt: j.CreationTimestamp.Time,
			})
		}
	}
	if list, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, p := range list.Items {
			items = append(items, podResource(p))
		}
	}
	return items
}

func (c *Client) listNetworkingResources(ctx context.Context, namespace string) []NamespaceResource {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil
	}
	var items []NamespaceResource

	if list, err := cs.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, s := range list.Items {
			ports := make([]string, 0, len(s.Spec.Ports))
			for _, p := range s.Spec.Ports {
				ports = append(ports, fmt.Sprintf("%d", p.Port))
			}
			detail := string(s.Spec.Type)
			if s.Spec.ClusterIP != "" && s.Spec.ClusterIP != "None" {
				detail += " · " + s.Spec.ClusterIP
			}
			if len(ports) > 0 {
				detail += " · :" + strings.Join(ports, ",")
			}
			items = append(items, NamespaceResource{
				Kind:      "Service",
				Name:      s.Name,
				Status:    string(s.Spec.Type),
				Detail:    detail,
				CreatedAt: s.CreationTimestamp.Time,
			})
		}
	}
	if list, err := cs.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, ing := range list.Items {
			hosts := make([]string, 0, len(ing.Spec.Rules))
			for _, rule := range ing.Spec.Rules {
				if rule.Host != "" {
					hosts = append(hosts, rule.Host)
				}
			}
			detail := strings.Join(hosts, ", ")
			if detail == "" {
				detail = "no host"
			}
			status := "Pending"
			if len(ing.Status.LoadBalancer.Ingress) > 0 {
				lb := ing.Status.LoadBalancer.Ingress[0]
				if lb.IP != "" {
					status = lb.IP
				} else if lb.Hostname != "" {
					status = lb.Hostname
				} else {
					status = "Provisioned"
				}
			}
			items = append(items, NamespaceResource{
				Kind:      "Ingress",
				Name:      ing.Name,
				Status:    status,
				Detail:    detail,
				CreatedAt: ing.CreationTimestamp.Time,
			})
		}
	}
	return items
}

func (c *Client) listConfigResources(ctx context.Context, namespace string) []NamespaceResource {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil
	}
	var items []NamespaceResource

	if list, err := cs.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, cm := range list.Items {
			items = append(items, NamespaceResource{
				Kind:      "ConfigMap",
				Name:      cm.Name,
				Status:    "Active",
				Detail:    fmt.Sprintf("%d keys", len(cm.Data)+len(cm.BinaryData)),
				CreatedAt: cm.CreationTimestamp.Time,
			})
		}
	}
	if list, err := cs.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, sec := range list.Items {
			items = append(items, NamespaceResource{
				Kind:      "Secret",
				Name:      sec.Name,
				Status:    string(sec.Type),
				Detail:    string(sec.Type),
				CreatedAt: sec.CreationTimestamp.Time,
			})
		}
	}
	return items
}

func (c *Client) listStorageResources(ctx context.Context, namespace string) []NamespaceResource {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil
	}
	var items []NamespaceResource

	if list, err := cs.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, pvc := range list.Items {
			qty := ""
			if req := pvc.Spec.Resources.Requests.Storage(); req != nil {
				qty = req.String()
			}
			detail := string(pvc.Status.Phase)
			if qty != "" {
				detail += " · " + qty
			}
			items = append(items, NamespaceResource{
				Kind:      "PersistentVolumeClaim",
				Name:      pvc.Name,
				Status:    string(pvc.Status.Phase),
				Detail:    detail,
				CreatedAt: pvc.CreationTimestamp.Time,
			})
		}
	}
	return items
}

func deploymentResource(d appsv1.Deployment) NamespaceResource {
	wanted := int32(0)
	if d.Spec.Replicas != nil {
		wanted = *d.Spec.Replicas
	}
	return NamespaceResource{
		Kind:      "Deployment",
		Name:      d.Name,
		Status:    replicaStatus(d.Status.ReadyReplicas, wanted),
		Detail:    fmt.Sprintf("%d/%d ready", d.Status.ReadyReplicas, wanted),
		CreatedAt: d.CreationTimestamp.Time,
	}
}

func podResource(p corev1.Pod) NamespaceResource {
	var restarts int32
	var ready, total int
	for _, cs := range p.Status.ContainerStatuses {
		restarts += cs.RestartCount
		total++
		if cs.Ready {
			ready++
		}
	}
	if total == 0 {
		total = len(p.Spec.Containers)
	}

	parts := []string{fmt.Sprintf("%d/%d ready", ready, total)}
	if p.Spec.NodeName != "" {
		parts = append(parts, "node/"+p.Spec.NodeName)
	}
	if p.Status.PodIP != "" {
		parts = append(parts, p.Status.PodIP)
	}
	if restarts > 0 {
		parts = append(parts, fmt.Sprintf("%d restarts", restarts))
	}

	return NamespaceResource{
		Kind:      "Pod",
		Name:      p.Name,
		Status:    string(p.Status.Phase),
		Detail:    strings.Join(parts, " · "),
		CreatedAt: p.CreationTimestamp.Time,
	}
}

func replicaStatus(ready, wanted int32) string {
	if wanted == 0 {
		return "Scaled"
	}
	if ready >= wanted {
		return "Ready"
	}
	return "Progressing"
}

func jobStatus(j batchv1.Job) string {
	for _, cond := range j.Status.Conditions {
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			return "Complete"
		}
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return "Failed"
		}
	}
	return "Running"
}

func sortResources(items []NamespaceResource) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Name < items[j].Name
	})
}
