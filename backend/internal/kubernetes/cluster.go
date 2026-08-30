package kubernetes

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
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
	return cacheDo(c, ctx, "overview", func() (*ClusterOverview, error) {
		return c.getOverviewUncached(ctx)
	})
}

func (c *Client) getOverviewUncached(ctx context.Context) (*ClusterOverview, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	name := c.Name
	if name == "" {
		name = "kubernetes"
	}
	overview := &ClusterOverview{
		ClusterName: name,
		Connected:   true,
	}

	var (
		nsErr, deployErr, svcErr, podErr, nodeErr error
		namespaces                                []ClusterNamespace
		pods                                      []PodInfo
		svcs                                      []ServiceInfo
		nodes                                     []NodeInfo
		deployCount                               int32
	)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		namespaces, nsErr = c.ListClusterNamespaces(ctx)
	}()
	go func() {
		defer wg.Done()
		pods, podErr = c.ListPods(ctx, "", "")
	}()
	go func() {
		defer wg.Done()
		svcs, svcErr = c.ListServices(ctx, "")
	}()
	go func() {
		defer wg.Done()
		nodes, nodeErr = c.ListNodes(ctx)
	}()
	go func() {
		defer wg.Done()
		deployments, err := cs.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
		if err != nil {
			deployErr = fmt.Errorf("list deployments: %w", err)
			return
		}
		deployCount = int32(len(deployments.Items))
	}()
	wg.Wait()

	if err := firstErr(nsErr, deployErr, svcErr, podErr, nodeErr); err != nil {
		return nil, err
	}

	overview.NamespaceCount = int32(len(namespaces))
	overview.DeploymentCount = deployCount
	overview.ServiceCount = int32(len(svcs))
	overview.PodCount = int32(len(pods))
	for _, pod := range pods {
		if pod.Status == string(corev1.PodRunning) {
			overview.RunningPods++
		}
	}
	overview.NodeCount = int32(len(nodes))
	for _, node := range nodes {
		if node.Status == "Ready" {
			overview.ReadyNodes++
		}
	}

	return overview, nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ListEvents returns recent warning/error cluster events.
func (c *Client) ListEvents(ctx context.Context, namespace string, limit int32) ([]ClusterEvent, error) {
	return c.listEvents(ctx, namespace, limit, true)
}

// ListClusterEvents returns recent events of every type, oldest first, for
// the cluster log stream.
func (c *Client) ListClusterEvents(ctx context.Context, limit int32) ([]ClusterEvent, error) {
	events, err := c.listEvents(ctx, "", limit, false)
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events, nil
}

func (c *Client) listEvents(ctx context.Context, namespace string, limit int32, warningsOnly bool) ([]ClusterEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	kind := "all"
	if warningsOnly {
		kind = "warn"
	}
	key := fmt.Sprintf("events:%s:%d:%s", namespace, limit, kind)
	return cacheDo(c, ctx, key, func() ([]ClusterEvent, error) {
		return c.listEventsUncached(ctx, namespace, limit, warningsOnly)
	})
}

func (c *Client) listEventsUncached(ctx context.Context, namespace string, limit int32, warningsOnly bool) ([]ClusterEvent, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}

	opts := metav1.ListOptions{
		Limit: int64(limit),
	}
	if warningsOnly {
		opts.FieldSelector = "type!=Normal"
	}

	var list *corev1.EventList
	var err error
	if namespace != "" {
		list, err = cs.CoreV1().Events(namespace).List(ctx, opts)
	} else {
		list, err = cs.CoreV1().Events("").List(ctx, opts)
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
	Containers   []ContainerInfo
	Phase        string
	Reason       string
	Message      string
	Ready        bool
	QOSClass     string
	// SchedulingMessage carries the scheduler's explanation when a pod is not
	// placed on any node. This is where a node at its pod ceiling shows up.
	SchedulingMessage string
}

// ContainerInfo is one container's state within a pod.
type ContainerInfo struct {
	Name                  string
	Image                 string
	Ready                 bool
	State                 string
	Reason                string
	Message               string
	RestartCount          int32
	CPURequest            string
	MemoryRequest         string
	CPULimit              string
	MemoryLimit           string
	HasLivenessProbe      bool
	HasReadinessProbe     bool
	HasStartupProbe       bool
	StartedAt             time.Time
	LastExitCode          int32
	LastTerminationReason string
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
// When app is non-empty, only pods labelled app=<app> are returned.
func (c *Client) ListPods(ctx context.Context, namespace, app string) ([]PodInfo, error) {
	app = strings.TrimSpace(app)
	key := "pods:" + namespace + ":" + app
	return cacheDo(c, ctx, key, func() ([]PodInfo, error) {
		return c.listPodsUncached(ctx, namespace, app)
	})
}

func (c *Client) listPodsUncached(ctx context.Context, namespace, app string) ([]PodInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	opts := metav1.ListOptions{}
	if app != "" {
		opts.LabelSelector = "app=" + app
	}
	list, err := cs.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	pods := make([]PodInfo, 0, len(list.Items))
	for i := range list.Items {
		pods = append(pods, podInfoFrom(&list.Items[i]))
	}
	return pods, nil
}

func podInfoFrom(p *corev1.Pod) PodInfo {
	// Spec holds requests, limits and probes; status holds state and restarts.
	// They are matched by container name because neither list is ordered.
	specs := make(map[string]*corev1.Container, len(p.Spec.Containers)+len(p.Spec.InitContainers))
	for i := range p.Spec.Containers {
		specs[p.Spec.Containers[i].Name] = &p.Spec.Containers[i]
	}
	for i := range p.Spec.InitContainers {
		specs[p.Spec.InitContainers[i].Name] = &p.Spec.InitContainers[i]
	}

	statuses := append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...)
	statuses = append(statuses, p.Status.ContainerStatuses...)

	var restartCount int32
	containers := make([]ContainerInfo, 0, len(statuses))
	allReady := len(statuses) > 0
	for i := range statuses {
		cs := &statuses[i]
		restartCount += cs.RestartCount
		if !cs.Ready {
			allReady = false
		}
		containers = append(containers, containerInfoFrom(cs, specs[cs.Name]))
	}

	// A pod whose containers were never created has no status to read, so its
	// spec is reported instead. Otherwise a pod stuck Pending would show no
	// containers at all, which reads as an empty pod rather than a blocked one.
	if len(statuses) == 0 {
		for i := range p.Spec.Containers {
			spec := &p.Spec.Containers[i]
			containers = append(containers, ContainerInfo{
				Name:              spec.Name,
				Image:             spec.Image,
				State:             "Waiting",
				Reason:            "NotCreated",
				CPURequest:        quantityString(spec.Resources.Requests, corev1.ResourceCPU),
				MemoryRequest:     quantityString(spec.Resources.Requests, corev1.ResourceMemory),
				CPULimit:          quantityString(spec.Resources.Limits, corev1.ResourceCPU),
				MemoryLimit:       quantityString(spec.Resources.Limits, corev1.ResourceMemory),
				HasLivenessProbe:  spec.LivenessProbe != nil,
				HasReadinessProbe: spec.ReadinessProbe != nil,
				HasStartupProbe:   spec.StartupProbe != nil,
			})
		}
		allReady = false
	}

	return PodInfo{
		Name:              p.Name,
		Namespace:         p.Namespace,
		Status:            podDisplayStatus(p, containers),
		IP:                p.Status.PodIP,
		Node:              p.Spec.NodeName,
		RestartCount:      restartCount,
		CreatedAt:         p.CreationTimestamp.Time,
		Containers:        containers,
		Phase:             string(p.Status.Phase),
		Reason:            p.Status.Reason,
		Message:           p.Status.Message,
		Ready:             allReady,
		QOSClass:          string(p.Status.QOSClass),
		SchedulingMessage: schedulingMessage(p),
	}
}

func containerInfoFrom(cs *corev1.ContainerStatus, spec *corev1.Container) ContainerInfo {
	out := ContainerInfo{
		Name:         cs.Name,
		Image:        cs.Image,
		Ready:        cs.Ready,
		RestartCount: cs.RestartCount,
	}

	switch {
	case cs.State.Running != nil:
		out.State = "Running"
		out.StartedAt = cs.State.Running.StartedAt.Time
	case cs.State.Waiting != nil:
		out.State = "Waiting"
		out.Reason = cs.State.Waiting.Reason
		out.Message = cs.State.Waiting.Message
	case cs.State.Terminated != nil:
		out.State = "Terminated"
		out.Reason = cs.State.Terminated.Reason
		out.Message = cs.State.Terminated.Message
		out.StartedAt = cs.State.Terminated.StartedAt.Time
		out.LastExitCode = cs.State.Terminated.ExitCode
	default:
		out.State = "Unknown"
	}

	// The previous termination is what explains a restart loop: a container
	// that is Running right now but restarted 27 times was OOMKilled or exited
	// non-zero, and only LastTerminationState still says so.
	if t := cs.LastTerminationState.Terminated; t != nil {
		out.LastExitCode = t.ExitCode
		out.LastTerminationReason = t.Reason
	}

	if spec != nil {
		out.Image = spec.Image
		out.CPURequest = quantityString(spec.Resources.Requests, corev1.ResourceCPU)
		out.MemoryRequest = quantityString(spec.Resources.Requests, corev1.ResourceMemory)
		out.CPULimit = quantityString(spec.Resources.Limits, corev1.ResourceCPU)
		out.MemoryLimit = quantityString(spec.Resources.Limits, corev1.ResourceMemory)
		out.HasLivenessProbe = spec.LivenessProbe != nil
		out.HasReadinessProbe = spec.ReadinessProbe != nil
		out.HasStartupProbe = spec.StartupProbe != nil
	}
	return out
}

// podDisplayStatus reports what is actually wrong with a pod.
//
// The phase alone is misleading: a pod whose only container sits in
// CrashLoopBackOff or ImagePullBackOff still reports phase Running, so a list
// keyed on phase showed a broken workload as healthy.
func podDisplayStatus(p *corev1.Pod, containers []ContainerInfo) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	if p.Status.Reason != "" && p.Status.Phase == corev1.PodFailed {
		return p.Status.Reason
	}
	for _, c := range containers {
		if c.State == "Waiting" && c.Reason != "" && c.Reason != "ContainerCreating" && c.Reason != "PodInitializing" {
			return c.Reason
		}
	}
	return string(p.Status.Phase)
}

// schedulingMessage returns why an unscheduled pod has not been placed.
//
// This is the signal for a cluster that has run out of room: the scheduler
// records "Insufficient cpu", "Insufficient memory" or "too many pods" on the
// PodScheduled condition, and without surfacing it a pod simply sits Pending
// with no visible explanation.
func schedulingMessage(p *corev1.Pod) string {
	if p.Spec.NodeName != "" {
		return ""
	}
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status != corev1.ConditionTrue {
			if cond.Message != "" {
				return cond.Message
			}
			return cond.Reason
		}
	}
	return ""
}

func quantityString(list corev1.ResourceList, name corev1.ResourceName) string {
	q, ok := list[name]
	if !ok {
		return ""
	}
	return q.String()
}

// ListServices retrieves all services in a namespace (or all if namespace is empty).
func (c *Client) ListServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	return cacheDo(c, ctx, "services:"+namespace, func() ([]ServiceInfo, error) {
		return c.listServicesUncached(ctx, namespace)
	})
}

func (c *Client) listServicesUncached(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	opts := metav1.ListOptions{}
	list, err := cs.CoreV1().Services(namespace).List(ctx, opts)
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
	// PodCapacity is a hard per-node ceiling, separate from CPU and memory.
	// Reaching it makes the scheduler refuse pods with "too many pods" however
	// idle the node looks, which is why it is reported alongside the others.
	PodCapacity int32
	PodCount    int32
	// Requests, not live usage, are what the scheduler compares against
	// allocatable when deciding whether another pod fits here.
	CPURequests           string
	MemoryRequests        string
	CPURequestsPercent    int32
	MemoryRequestsPercent int32
	PodsPercent           int32
	KubeletVersion        string
	Unschedulable         bool
	PressureConditions    []string
	CreatedAt             time.Time
	StatusMessage         string
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
		TailLines:  &tailLines,
		Timestamps: false,
	}

	clientset, err := c.logStreamClientset()
	if err != nil {
		return "", err
	}

	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		if isWaitingForLogs(err) {
			opts.Previous = true
			stream, err = clientset.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
		}
		if err != nil {
			return "", fmt.Errorf("stream pod logs: %w", err)
		}
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
	return cacheDo(c, ctx, "nodes", func() ([]NodeInfo, error) {
		return c.listNodesUncached(ctx)
	})
}

func (c *Client) listNodesUncached(ctx context.Context) ([]NodeInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	// Pods are fetched once and bucketed by node rather than listed per node:
	// the per-node counts and request sums are the numbers that explain a
	// scheduling failure, and a list-per-node would multiply API calls by the
	// size of the cluster.
	usage := c.nodeUsage(ctx)

	nodes := make([]NodeInfo, 0, len(list.Items))
	for i := range list.Items {
		n := &list.Items[i]
		nodes = append(nodes, nodeInfoFrom(n, usage[n.Name]))
	}
	return nodes, nil
}

// nodeLoad is the scheduled load on one node.
type nodeLoad struct {
	pods     int32
	cpuMilli int64
	memBytes int64
}

// nodeUsage sums the requests of running pods per node. A failure returns an
// empty map: capacity figures are still worth reporting without the usage
// overlay, so this must not fail the whole node listing.
func (c *Client) nodeUsage(ctx context.Context) map[string]nodeLoad {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil
	}
	out := map[string]nodeLoad{}
	pods, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		// Terminated pods hold no resources and are not counted against a
		// node's pod ceiling, so including them would overstate the load.
		FieldSelector: "status.phase!=Succeeded,status.phase!=Failed",
	})
	if err != nil {
		return out
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Spec.NodeName == "" {
			continue
		}
		load := out[p.Spec.NodeName]
		load.pods++
		for j := range p.Spec.Containers {
			req := p.Spec.Containers[j].Resources.Requests
			if q, ok := req[corev1.ResourceCPU]; ok {
				load.cpuMilli += q.MilliValue()
			}
			if q, ok := req[corev1.ResourceMemory]; ok {
				load.memBytes += q.Value()
			}
		}
		out[p.Spec.NodeName] = load
	}
	return out
}

func nodeInfoFrom(n *corev1.Node, load nodeLoad) NodeInfo {
	status := "NotReady"
	statusMessage := ""
	pressure := []string{}
	for i := range n.Status.Conditions {
		cond := &n.Status.Conditions[i]
		switch cond.Type {
		case corev1.NodeReady:
			if cond.Status == corev1.ConditionTrue {
				status = "Ready"
			} else {
				// The kubelet's own explanation. "Kubelet stopped posting node
				// status" is the signature of a node starved of CPU, which is
				// otherwise indistinguishable from a crashed one.
				statusMessage = cond.Message
			}
		case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure, corev1.NodeNetworkUnavailable:
			if cond.Status == corev1.ConditionTrue {
				pressure = append(pressure, string(cond.Type))
			}
		}
	}

	role := "worker"
	if _, ok := n.Labels["node-role.kubernetes.io/control-plane"]; ok {
		role = "control-plane"
	} else if _, ok := n.Labels["node-role.kubernetes.io/master"]; ok {
		role = "control-plane"
	}

	cpuAllocMilli := n.Status.Allocatable.Cpu().MilliValue()
	memAllocBytes := n.Status.Allocatable.Memory().Value()
	podCapacity := int32(n.Status.Allocatable.Pods().Value())

	return NodeInfo{
		Name:                  n.Name,
		Status:                status,
		StatusMessage:         statusMessage,
		Role:                  role,
		CPUCapacity:           n.Status.Capacity.Cpu().String(),
		MemoryCapacity:        n.Status.Capacity.Memory().String(),
		CPUAllocatable:        n.Status.Allocatable.Cpu().String(),
		MemoryAllocatable:     n.Status.Allocatable.Memory().String(),
		PodCapacity:           podCapacity,
		PodCount:              load.pods,
		CPURequests:           formatMilliCPU(load.cpuMilli),
		MemoryRequests:        formatBytes(load.memBytes),
		CPURequestsPercent:    percentOf(load.cpuMilli, cpuAllocMilli),
		MemoryRequestsPercent: percentOf(load.memBytes, memAllocBytes),
		PodsPercent:           percentOf(int64(load.pods), int64(podCapacity)),
		KubeletVersion:        n.Status.NodeInfo.KubeletVersion,
		Unschedulable:         n.Spec.Unschedulable,
		PressureConditions:    pressure,
		CreatedAt:             n.CreationTimestamp.Time,
	}
}

func percentOf(used, total int64) int32 {
	if total <= 0 {
		return 0
	}
	return int32(used * 100 / total)
}

// GetResourceMetrics calculates cluster resource utilization from node allocatable and pod requests.
func (c *Client) GetResourceMetrics(ctx context.Context) (*ResourceMetrics, error) {
	return cacheDo(c, ctx, "metrics", func() (*ResourceMetrics, error) {
		return c.getResourceMetricsUncached(ctx)
	})
}

func (c *Client) getResourceMetricsUncached(ctx context.Context) (*ResourceMetrics, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	var totalCPU, totalMem int64
	for _, n := range nodes.Items {
		totalCPU += n.Status.Allocatable.Cpu().MilliValue()
		totalMem += n.Status.Allocatable.Memory().Value()
	}

	pods, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
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
