package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodInfoFromCrashLoopReportsReasonNotPhase(t *testing.T) {
	// A container in CrashLoopBackOff leaves the pod phase at Running, which is
	// why the display status must come from the container state.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "team-a"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{{
				Name:  "api",
				Image: "api:v2",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
				},
				LivenessProbe:  &corev1.Probe{},
				ReadinessProbe: &corev1.Probe{},
			}},
		},
		Status: corev1.PodStatus{
			Phase:    corev1.PodRunning,
			QOSClass: corev1.PodQOSBurstable,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "api",
				Image:        "api:v2",
				Ready:        false,
				RestartCount: 27,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason:  "CrashLoopBackOff",
					Message: "back-off 5m0s restarting failed container",
				}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 137,
					Reason:   "OOMKilled",
				}},
			}},
		},
	}

	got := podInfoFrom(pod)

	if got.Status != "CrashLoopBackOff" {
		t.Errorf("status = %q, want CrashLoopBackOff", got.Status)
	}
	if got.Phase != "Running" {
		t.Errorf("phase = %q, want Running (raw phase must be preserved)", got.Phase)
	}
	if got.Ready {
		t.Error("pod reported ready with a non-ready container")
	}
	if got.RestartCount != 27 {
		t.Errorf("restartCount = %d, want 27", got.RestartCount)
	}
	if len(got.Containers) != 1 {
		t.Fatalf("containers = %d, want 1", len(got.Containers))
	}
	c := got.Containers[0]
	if c.LastTerminationReason != "OOMKilled" || c.LastExitCode != 137 {
		t.Errorf("last termination = %q/%d, want OOMKilled/137", c.LastTerminationReason, c.LastExitCode)
	}
	if c.CPURequest != "100m" || c.MemoryRequest != "128Mi" || c.CPULimit != "500m" {
		t.Errorf("resources = %q/%q/%q", c.CPURequest, c.MemoryRequest, c.CPULimit)
	}
	if c.MemoryLimit != "" {
		t.Errorf("memory limit = %q, want empty when unset", c.MemoryLimit)
	}
	if !c.HasLivenessProbe || !c.HasReadinessProbe || c.HasStartupProbe {
		t.Errorf("probes = %v/%v/%v", c.HasLivenessProbe, c.HasReadinessProbe, c.HasStartupProbe)
	}
}

func TestPodInfoFromUnscheduledSurfacesSchedulingReason(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "team-a"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "web", Image: "web:v1"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:    corev1.PodScheduled,
				Status:  corev1.ConditionFalse,
				Reason:  "Unschedulable",
				Message: "0/1 nodes are available: 1 Too many pods.",
			}},
		},
	}

	got := podInfoFrom(pod)

	if got.SchedulingMessage != "0/1 nodes are available: 1 Too many pods." {
		t.Errorf("schedulingMessage = %q", got.SchedulingMessage)
	}
	// Containers were never created, so the spec stands in for the status.
	if len(got.Containers) != 1 || got.Containers[0].Reason != "NotCreated" {
		t.Fatalf("containers = %+v, want one synthesized from spec", got.Containers)
	}
	if got.Ready {
		t.Error("unscheduled pod reported ready")
	}
}

func TestPodInfoFromTerminatingBeatsPhase(t *testing.T) {
	now := metav1.Now()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "gone", DeletionTimestamp: &now},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "app",
				Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}},
			}},
		},
	}

	if got := podInfoFrom(pod).Status; got != "Terminating" {
		t.Errorf("status = %q, want Terminating", got)
	}
}

func TestPodInfoFromContainerCreatingIsNotAnError(t *testing.T) {
	// ContainerCreating and PodInitializing are normal startup states; treating
	// them as the display status would flag every fresh pod as broken.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "new"},
		Spec:       corev1.PodSpec{NodeName: "node-1", Containers: []corev1.Container{{Name: "app"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "app",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
			}},
		},
	}

	if got := podInfoFrom(pod).Status; got != "Pending" {
		t.Errorf("status = %q, want Pending", got)
	}
}

func TestNodeInfoFromReportsSaturation(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "minikube",
			Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.31.0"},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Message: "Kubelet stopped posting node status."},
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
			},
		},
	}

	got := nodeInfoFrom(node, nodeLoad{pods: 55, cpuMilli: 1000, memBytes: 2 << 30})

	if got.Status != "NotReady" {
		t.Errorf("status = %q, want NotReady", got.Status)
	}
	if got.StatusMessage != "Kubelet stopped posting node status." {
		t.Errorf("statusMessage = %q", got.StatusMessage)
	}
	if got.Role != "control-plane" {
		t.Errorf("role = %q, want control-plane", got.Role)
	}
	if got.PodCount != 55 || got.PodCapacity != 110 || got.PodsPercent != 50 {
		t.Errorf("pods = %d/%d (%d%%), want 55/110 (50%%)", got.PodCount, got.PodCapacity, got.PodsPercent)
	}
	if got.CPURequestsPercent != 50 || got.MemoryRequestsPercent != 50 {
		t.Errorf("requests = %d%% cpu / %d%% mem, want 50/50", got.CPURequestsPercent, got.MemoryRequestsPercent)
	}
	if len(got.PressureConditions) != 1 || got.PressureConditions[0] != "MemoryPressure" {
		t.Errorf("pressure = %v, want [MemoryPressure]", got.PressureConditions)
	}
	if got.KubeletVersion != "v1.31.0" {
		t.Errorf("kubeletVersion = %q", got.KubeletVersion)
	}
}

func TestNodeInfoFromReadyWorkerHasNoMessage(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("110")},
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}

	got := nodeInfoFrom(node, nodeLoad{})

	if got.Status != "Ready" || got.StatusMessage != "" {
		t.Errorf("status = %q/%q, want Ready with no message", got.Status, got.StatusMessage)
	}
	if got.Role != "worker" {
		t.Errorf("role = %q, want worker", got.Role)
	}
	if len(got.PressureConditions) != 0 {
		t.Errorf("pressure = %v, want empty", got.PressureConditions)
	}
}

func TestPercentOfZeroCapacity(t *testing.T) {
	if got := percentOf(5, 0); got != 0 {
		t.Errorf("percentOf(5, 0) = %d, want 0", got)
	}
}
