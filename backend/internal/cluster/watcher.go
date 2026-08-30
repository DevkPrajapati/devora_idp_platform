package cluster

import (
	"context"
	"sync"
	"time"

	db "github.com/idp/platform/backend/internal/database/sqlc"
	"github.com/idp/platform/backend/internal/kubernetes"
	"go.uber.org/zap"
)

const (
	// watchInterval is how often the active cluster is probed. Long enough that
	// the probe is not itself load on a small local cluster, short enough that
	// a user who just started their cluster does not sit and wait.
	watchInterval = 10 * time.Second
	// watchProbeTimeout bounds one probe.
	watchProbeTimeout = 8 * time.Second
	// statusWriteTimeout bounds recording the outcome. It is deliberately a
	// separate budget from the probe: sharing one meant a probe that timed out
	// left nothing for the database write, so the status the watcher had just
	// determined was never persisted.
	statusWriteTimeout = 5 * time.Second
	// failuresBeforeReconnect is how many consecutive failed probes justify
	// rebuilding the client. A single failure is usually a flapping kubelet or
	// a slow API server, and tearing down a working connection over one blip
	// would cause more outages than it fixes.
	failuresBeforeReconnect = 2
)

// watcher keeps the platform's Kubernetes connection matched to reality.
//
// Two failure modes made the platform need a restart to recover. A cluster that
// was not up when the backend started stayed disconnected forever, because the
// only reconnect attempt happened during bootstrap. And a local cluster that
// was restarted came back on a different API server port, so the client the hub
// held could never work again no matter how healthy the cluster was. Both are
// recoverable without operator involvement: re-resolve the kubeconfig from the
// authoritative source and rebind.
type watcher struct {
	svc *Service

	mu       sync.Mutex
	failures int
	// degraded records that the cluster is bound but failing its probe, so the
	// condition is logged on transition instead of once per tick.
	degraded bool

	stop chan struct{}
	done chan struct{}
}

// StartWatcher begins reconciling the active cluster connection in the
// background. Safe to call when the fleet is not configured, in which case it
// does nothing.
func (s *Service) StartWatcher() {
	if s.repo == nil || s.hub == nil || s.watch != nil {
		return
	}
	w := &watcher{
		svc:  s,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	s.watch = w
	go w.run()
	// First tick is immediate: waiting a full interval after startup left a
	// cluster that came up during boot disconnected for ten extra seconds.
	go w.tick()
}

// StopWatcher halts the reconcile loop and waits for it to exit.
func (s *Service) StopWatcher() {
	if s == nil || s.watch == nil {
		return
	}
	close(s.watch.stop)
	<-s.watch.done
	s.watch = nil
}

func (w *watcher) run() {
	defer close(w.done)
	ticker := time.NewTicker(watchInterval)
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

func (w *watcher) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), watchProbeTimeout)
	defer cancel()

	svc := w.svc
	active, err := svc.repo.GetActive(ctx)
	if err != nil || active == nil {
		// No cluster is selected. Nothing to keep alive; an admin activating one
		// binds it directly.
		w.reset()
		return
	}

	live := svc.hub.Live()
	if live != nil && live.Bound() {
		if err := live.Ping(ctx); err == nil {
			live.SetReachable(true)
			w.recovered(active)
			return
		} else if kubernetes.IsClusterDown(err) {
			live.SetReachable(false)
			w.reconnect(ctx, active)
			return
		}
	}

	if live != nil && !live.Bound() {
		w.reconnect(ctx, active)
		return
	}

	if w.record() < failuresBeforeReconnect {
		if live != nil {
			live.SetReachable(false)
		}
		return
	}
	w.reconnect(ctx, active)
}

// recovered clears the failure state after a successful probe and, if the
// cluster had been marked unhealthy, records that it is running again.
func (w *watcher) recovered(active *db.Cluster) {
	w.mu.Lock()
	wasDegraded := w.degraded
	w.failures = 0
	w.degraded = false
	w.mu.Unlock()

	svc := w.svc
	if wasDegraded && svc.logger != nil {
		svc.logger.Info("active cluster is healthy again", zap.String("cluster", active.Name))
	}
	if active.Status != statusRunning {
		w.setStatus(active, statusRunning, "")
	}
}

// setStatus persists a cluster status on its own context budget, so an outcome
// determined by a probe that exhausted its deadline is still recorded.
func (w *watcher) setStatus(active *db.Cluster, status, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), statusWriteTimeout)
	defer cancel()
	if _, err := w.svc.repo.SetStatus(ctx, active.ID, status, detail); err != nil && w.svc.logger != nil {
		w.svc.logger.Warn("could not record cluster status", zap.Error(err))
	}
}

// reconnect rebuilds the client for the active cluster from its authoritative
// kubeconfig and rebinds the hub if the result is reachable.
func (w *watcher) reconnect(ctx context.Context, active *db.Cluster) {
	svc := w.svc
	logger := svc.logger

	client, err := svc.clientFor(ctx, active)
	if err == nil {
		if pingErr := client.Ping(ctx); pingErr == nil {
			svc.hub.Bind(client)
			svc.adoptIdentity(ctx, active, client)
			w.mu.Lock()
			w.failures = 0
			w.degraded = false
			w.mu.Unlock()
			if logger != nil {
				logger.Info("reconnected to the active cluster", zap.String("cluster", active.Name))
			}
			svc.logLine(active.ID, "lifecycle", "platform reconnected to the cluster")
			if active.Status != statusRunning {
				w.setStatus(active, statusRunning, "")
			}
			return
		} else {
			err = pingErr
		}
	}

	w.markUnreachable(ctx, active, err)
}

// markUnreachable records why the cluster cannot be reached and decides whether
// to keep the connection bound.
//
// Unbinding is reserved for a cluster that is genuinely gone. It used to happen
// on any repeated probe failure, which made a slow API server — the normal state
// of a local cluster short on CPU — read as "kubernetes cluster not connected".
// Every service checks that flag before doing anything, so a few seconds of
// latency took the whole platform down for as long as the slowness lasted, and
// swapping the clientset out from under in-flight requests panicked them. A
// cluster that is merely slow stays bound: its calls then fail or succeed on
// their own merits, and reads backed by cache keep serving.
func (w *watcher) markUnreachable(ctx context.Context, active *db.Cluster, cause error) {
	svc := w.svc
	detail := "cluster is not reachable"
	if cause != nil {
		detail = cause.Error()
	}

	if live := svc.hub.Live(); live != nil {
		live.SetReachable(false)
	}

	if svc.clusterGone(ctx, active) {
		detail = "cluster no longer exists; it was deleted outside the platform"
		if svc.logger != nil {
			svc.logger.Warn("active cluster no longer exists",
				zap.String("cluster", active.Name))
		}
		svc.logLine(active.ID, "lifecycle", detail)
		if active.Status != statusStopped {
			w.setStatus(active, statusStopped, detail)
		}
		svc.hub.Bind(nil)
		return
	}

	if kubernetes.IsClusterDown(cause) {
		detail = "cluster is stopped or unreachable"
		if active.Status != statusStopped && active.Status != statusStarting && active.Status != statusStopping {
			w.setStatus(active, statusStopped, detail)
		}
		svc.hub.Bind(nil)
		if firstDown := w.markDegraded(); firstDown && svc.logger != nil {
			svc.logger.Warn("active cluster is down; disconnecting until it can be reached",
				zap.String("cluster", active.Name), zap.Error(cause))
		}
		return
	}

	w.mu.Lock()
	first := !w.degraded
	w.degraded = true
	w.mu.Unlock()

	// Logged once per episode: at a 10s tick a long slow patch would otherwise
	// bury everything else in the log.
	if first && svc.logger != nil {
		svc.logger.Warn("active cluster is degraded; keeping the connection bound",
			zap.String("cluster", active.Name), zap.Error(cause))
	}
	if active.Status != statusError {
		w.setStatus(active, statusError, detail)
	}
}

// clusterGone reports whether a local cluster's provisioner still knows about
// it. Only kind and minikube can answer; an imported cluster being unreachable
// says nothing about whether it exists.
func (s *Service) clusterGone(ctx context.Context, row *db.Cluster) bool {
	if s.provisioner == nil {
		return false
	}
	switch row.Provider {
	case kubernetes.ProviderMinikube:
		exists, err := s.provisioner.MinikubeProfileExists(ctx, row.Name)
		return err == nil && !exists
	case kubernetes.ProviderKind:
		exists, err := s.provisioner.KindClusterExists(ctx, row.Name)
		return err == nil && !exists
	default:
		return false
	}
}

func (w *watcher) record() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failures++
	return w.failures
}

func (w *watcher) markDegraded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	first := !w.degraded
	w.degraded = true
	return first
}

func (w *watcher) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failures = 0
	w.degraded = false
}
