package cluster

import (
	"testing"

	"github.com/idp/platform/backend/internal/kubernetes"
)

func TestNeedsMoreCapacity(t *testing.T) {
	if needsMoreCapacity(nil) {
		t.Fatal("empty pod list does not need capacity")
	}
	if !needsMoreCapacity([]kubernetes.PodInfo{{SchedulingMessage: "0/1 nodes are available: 1 Too many pods."}}) {
		t.Fatal("too many pods must trigger a scale")
	}
	if !needsMoreCapacity([]kubernetes.PodInfo{{SchedulingMessage: "0/1 nodes are available: 1 Insufficient cpu"}}) {
		t.Fatal("insufficient cpu must trigger a scale")
	}
	if needsMoreCapacity([]kubernetes.PodInfo{{SchedulingMessage: "0/1 nodes are available: 1 node(s) had taint"}}) {
		t.Fatal("taints are not a capacity problem the local scaler can fix")
	}
}

func TestBusyStatus(t *testing.T) {
	if !busyStatus(statusStarting) || !busyStatus(statusStopping) || !busyStatus(statusDeleting) {
		t.Fatal("lifecycle in-flight statuses must be busy")
	}
	if busyStatus(statusRunning) || busyStatus(statusStopped) {
		t.Fatal("steady statuses must not be busy")
	}
}
