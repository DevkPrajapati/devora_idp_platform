package kubernetes

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterOverview holds aggregate cluster statistics.
type ClusterOverview struct {
	ClusterName     string
	Connected       bool
	NamespaceCount  int32
	DeploymentCount int32
	ServiceCount    int32
	PodCount        int32
	RunningPods     int32
	NodeCount       int32
	ReadyNodes      int32
}

// ClusterEvent represents a Kubernetes event.
type ClusterEvent struct {
	Type      string
	Reason    string
	Message   string
	Namespace string
	Object    string
	Timestamp time.Time
}

// GetOverview returns aggregate cluster statistics.
func (c *Client) GetOverview(ctx context.Context) (*ClusterOverview, error) {
	overview := &ClusterOverview{
		ClusterName: "kubernetes",
		Connected:   true,
	}

	nsList, err := c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	overview.NamespaceCount = int32(len(nsList.Items))

	deployments, err := c.Clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	overview.DeploymentCount = int32(len(deployments.Items))

	services, err := c.Clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	overview.ServiceCount = int32(len(services.Items))

	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	overview.PodCount = int32(len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			overview.RunningPods++
		}
	}

	nodes, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	overview.NodeCount = int32(len(nodes.Items))
	for _, node := range nodes.Items {
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				overview.ReadyNodes++
				break
			}
		}
	}

	return overview, nil
}

// ListEvents returns recent cluster events.
func (c *Client) ListEvents(ctx context.Context, namespace string, limit int32) ([]ClusterEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	opts := metav1.ListOptions{
		FieldSelector: "type!=Normal",
		Limit:         int64(limit),
	}

	var list *corev1.EventList
	var err error
	if namespace != "" {
		list, err = c.Clientset.CoreV1().Events(namespace).List(ctx, opts)
	} else {
		list, err = c.Clientset.CoreV1().Events("").List(ctx, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	events := make([]ClusterEvent, 0, len(list.Items))
	for _, e := range list.Items {
		events = append(events, ClusterEvent{
			Type:      e.Type,
			Reason:    e.Reason,
			Message:   e.Message,
			Namespace: e.Namespace,
			Object:    fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
			Timestamp: e.LastTimestamp.Time,
		})
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})

	return events, nil
}

// PodInfo holds pod metadata for our platform API.
type PodInfo struct {
	Name         string
	Namespace    string
	Status       string
	IP           string
	Node         string
	RestartCount int32
	CreatedAt    time.Time
}

// ServiceInfo holds service metadata for our platform API.
type ServiceInfo struct {
	Name       string
	Namespace  string
	Type       string
	ClusterIP  string
	ExternalIP string
	Ports      []int32
	CreatedAt  time.Time
}

// ListPods retrieves all pods in a namespace (or all if namespace is empty).
func (c *Client) ListPods(ctx context.Context, namespace string) ([]PodInfo, error) {
	opts := metav1.ListOptions{}
	list, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	pods := make([]PodInfo, 0, len(list.Items))
	for _, p := range list.Items {
		var restartCount int32
		for _, cs := range p.Status.ContainerStatuses {
			restartCount += cs.RestartCount
		}

		pods = append(pods, PodInfo{
			Name:         p.Name,
			Namespace:    p.Namespace,
			Status:       string(p.Status.Phase),
			IP:           p.Status.PodIP,
			Node:         p.Spec.NodeName,
			RestartCount: restartCount,
			CreatedAt:    p.CreationTimestamp.Time,
		})
	}
	return pods, nil
}

// ListServices retrieves all services in a namespace (or all if namespace is empty).
func (c *Client) ListServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	opts := metav1.ListOptions{}
	list, err := c.Clientset.CoreV1().Services(namespace).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	svcs := make([]ServiceInfo, 0, len(list.Items))
	for _, s := range list.Items {
		ports := make([]int32, 0, len(s.Spec.Ports))
		for _, port := range s.Spec.Ports {
			ports = append(ports, port.Port)
		}

		externalIP := ""
		if len(s.Status.LoadBalancer.Ingress) > 0 {
			if s.Status.LoadBalancer.Ingress[0].IP != "" {
				externalIP = s.Status.LoadBalancer.Ingress[0].IP
			} else {
				externalIP = s.Status.LoadBalancer.Ingress[0].Hostname
			}
		}

		svcs = append(svcs, ServiceInfo{
			Name:       s.Name,
			Namespace:  s.Namespace,
			Type:       string(s.Spec.Type),
			ClusterIP:  s.Spec.ClusterIP,
			ExternalIP: externalIP,
			Ports:      ports,
			CreatedAt:  s.CreationTimestamp.Time,
		})
	}
	return svcs, nil
}

// NodeInfo holds node metadata.
type NodeInfo struct {
	Name              string
	Status            string
	Role              string
	CPUCapacity       string
	MemoryCapacity    string
	CPUAllocatable    string
	MemoryAllocatable string
}

// ResourceMetrics holds cluster resource utilization.
type ResourceMetrics struct {
	CPURequests        string
	CPUCapacity        string
	CPUUsagePercent    int32
	MemoryRequests     string
	MemoryCapacity     string
	MemoryUsagePercent int32
}

// GetPodLogs retrieves container logs for a pod.
func (c *Client) GetPodLogs(ctx context.Context, namespace, podName string, tailLines int64) (string, error) {
	if tailLines <= 0 {
		tailLines = 100
	}

	opts := &corev1.PodLogOptions{
		TailLines: &tailLines,
	}

	req := c.Clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("stream pod logs: %w", err)
	}
	defer func() { _ = stream.Close() }()

	buf := new(strings.Builder)
	if _, err := io.Copy(buf, stream); err != nil {
		return "", fmt.Errorf("read pod logs: %w", err)
	}

	return buf.String(), nil
}

// ListNodes returns cluster node information.
func (c *Client) ListNodes(ctx context.Context) ([]NodeInfo, error) {
	list, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	nodes := make([]NodeInfo, 0, len(list.Items))
	for _, n := range list.Items {
		status := "NotReady"
		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				status = "Ready"
				break
			}
		}

		role := "worker"
		if _, ok := n.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "control-plane"
		}

		nodes = append(nodes, NodeInfo{
			Name:              n.Name,
			Status:            status,
			Role:              role,
			CPUCapacity:       n.Status.Capacity.Cpu().String(),
			MemoryCapacity:    n.Status.Capacity.Memory().String(),
			CPUAllocatable:    n.Status.Allocatable.Cpu().String(),
			MemoryAllocatable: n.Status.Allocatable.Memory().String(),
		})
	}
	return nodes, nil
}

// GetResourceMetrics calculates cluster resource utilization from node allocatable and pod requests.
func (c *Client) GetResourceMetrics(ctx context.Context) (*ResourceMetrics, error) {
	nodes, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	var totalCPU, totalMem int64
	for _, n := range nodes.Items {
		totalCPU += n.Status.Allocatable.Cpu().MilliValue()
		totalMem += n.Status.Allocatable.Memory().Value()
	}

	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	var usedCPU, usedMem int64
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, ctr := range p.Spec.Containers {
			if cpu := ctr.Resources.Requests.Cpu(); cpu != nil {
				usedCPU += cpu.MilliValue()
			}
			if mem := ctr.Resources.Requests.Memory(); mem != nil {
				usedMem += mem.Value()
			}
		}
	}

	cpuPct := int32(0)
	if totalCPU > 0 {
		cpuPct = int32(usedCPU * 100 / totalCPU)
	}
	memPct := int32(0)
	if totalMem > 0 {
		memPct = int32(usedMem * 100 / totalMem)
	}

	return &ResourceMetrics{
		CPURequests:        formatMilliCPU(usedCPU),
		CPUCapacity:        formatMilliCPU(totalCPU),
		CPUUsagePercent:    cpuPct,
		MemoryRequests:     formatBytes(usedMem),
		MemoryCapacity:     formatBytes(totalMem),
		MemoryUsagePercent: memPct,
	}, nil
}

func formatMilliCPU(milli int64) string {
	if milli >= 1000 {
		if milli%1000 == 0 {
			return fmt.Sprintf("%d", milli/1000)
		}
		return fmt.Sprintf("%.1f", float64(milli)/1000)
	}
	return fmt.Sprintf("%dm", milli)
}

func formatBytes(b int64) string {
	const (
		_  = iota
		KB = 1 << 10
		MB = 1 << 20
		GB = 1 << 30
	)
	switch {
	case b >= GB:
		if b%GB == 0 {
			return fmt.Sprintf("%dGi", b/GB)
		}
		return fmt.Sprintf("%.1fGi", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%dMi", b/MB)
	default:
		return fmt.Sprintf("%dKi", b/KB)
	}
}
