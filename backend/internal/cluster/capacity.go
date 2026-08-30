package cluster

import (
	"context"
	"strings"
	"time"

	"github.com/idp/platform/backend/internal/kubernetes"
	"go.uber.org/zap"
)

const (
	capacityInterval = 30 * time.Second
	maxLocalNodes    = 3
	capacityCooldown = 2 * time.Minute
)

// capacityWatch adds a node when pods cannot be scheduled for lack of capacity.
//
// Minikube has no cluster-autoscaler. The industry equivalent on a cloud
// provider is a node group that grows when the scheduler reports Insufficient
// cpu/memory or Too many pods. Here the same signal drives `minikube node add`,
// capped so a local machine cannot be asked for an unbounded number of VMs.
type capacityWatch struct {
	svc     *Service
	stop    chan struct{}
	done    chan struct{}
	lastAdd time.Time
}

func (s *Service) StartCapacityWatch() {
	if s.repo == nil || s.hub == nil || s.capacity != nil {
		return
	}
	w := &capacityWatch{
		svc:  s,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	s.capacity = w
	go w.run()
}

func (s *Service) StopCapacityWatch() {
	if s == nil || s.capacity == nil {
		return
	}
	close(s.capacity.stop)
	<-s.capacity.done
	s.capacity = nil
}

func (w *capacityWatch) run() {
	defer close(w.done)
	ticker := time.NewTicker(capacityInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

func (w *capacityWatch) tick() {
	svc := w.svc
	if svc.k8s == nil || !svc.k8s.Available() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pods, err := svc.k8s.ListPods(ctx, "", "")
	if err != nil {
		return
	}
	if !needsMoreCapacity(pods) {
		return
	}

	active, err := svc.repo.GetActive(ctx)
	if err != nil || active == nil {
		return
	}
	if active.Provider != kubernetes.ProviderMinikube {
		return
	}
	if active.NodeCount >= maxLocalNodes {
		if svc.logger != nil {
			svc.logger.Warn("pods are unschedulable but local node cap is reached",
				zap.String("cluster", active.Name),
				zap.Int32("nodes", active.NodeCount),
				zap.Int("cap", maxLocalNodes))
		}
		return
	}
	if !w.lastAdd.IsZero() && time.Since(w.lastAdd) < capacityCooldown {
		return
	}

	svc.logLine(active.ID, "lifecycle", "adding a node: pods cannot be scheduled on the current capacity")
	if err := svc.provisioner.AddMinikubeNode(ctx, active.Name); err != nil {
		if svc.logger != nil {
			svc.logger.Warn("could not add a minikube node", zap.Error(err))
		}
		svc.logLine(active.ID, "lifecycle", "node add failed: "+err.Error())
		return
	}
	w.lastAdd = time.Now()
	svc.logLine(active.ID, "lifecycle", "node added; waiting for kubelet to become Ready")
}

func needsMoreCapacity(pods []kubernetes.PodInfo) bool {
	for _, p := range pods {
		msg := strings.ToLower(p.SchedulingMessage)
		if msg == "" {
			continue
		}
		if strings.Contains(msg, "insufficient cpu") ||
			strings.Contains(msg, "insufficient memory") ||
			strings.Contains(msg, "too many pods") ||
			strings.Contains(msg, "didn't have enough resource") {
			return true
		}
	}
	return false
}
