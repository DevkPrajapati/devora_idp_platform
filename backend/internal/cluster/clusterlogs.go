package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	db "github.com/idp/platform/backend/internal/database/sqlc"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) ensureLogs() *clusterLogHub {
	if s.logs == nil {
		s.logs = newClusterLogHub()
	}
	return s.logs
}

func (s *Service) logLine(id pgtype.UUID, source, message string) {
	s.ensureLogs().Append(clusterIDString(id), source, strings.TrimRight(message, "\r\n"))
}

func (s *Service) withJobLogs(ctx context.Context, id pgtype.UUID, source string) context.Context {
	if source == "" {
		source = "provision"
	}
	return kubernetes.WithLogSink(ctx, func(line string) {
		s.logLine(id, source, line)
	})
}

// StreamClusterLogs replays captured provisioner/lifecycle output, then follows
// new lines, node logs, and Kubernetes events until the client disconnects.
func (s *Service) StreamClusterLogs(
	ctx context.Context,
	req *idpv1.StreamClusterLogsRequest,
	emit func(*idpv1.LogLine) error,
) error {
	row, err := s.requireCluster(ctx, req.GetId())
	if err != nil {
		return err
	}
	id := clusterIDString(row.ID)
	tail := int(req.GetTailLines())
	if tail <= 0 {
		tail = 200
	}
	if tail > 5000 {
		tail = 5000
	}

	if !req.GetFollow() {
		for _, line := range s.ensureLogs().Snapshot(id, tail) {
			if err := emit(storedToProto(line)); err != nil {
				return err
			}
		}
		return s.emitRuntimeSnapshot(ctx, row, emit)
	}

	snap, ch, cancel := s.ensureLogs().Subscribe(id)
	defer cancel()
	start := 0
	if len(snap) > tail {
		start = len(snap) - tail
	}
	for _, line := range snap[start:] {
		if err := emit(storedToProto(line)); err != nil {
			return err
		}
	}

	runtimeCtx, runtimeCancel := context.WithCancel(ctx)
	defer runtimeCancel()
	runtimeCh := make(chan *idpv1.LogLine, 128)
	go func() {
		defer close(runtimeCh)
		s.followRuntimeLogs(runtimeCtx, row, runtimeCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-ch:
			if !ok {
				ch = nil
				if runtimeCh == nil {
					return nil
				}
				continue
			}
			if err := emit(storedToProto(line)); err != nil {
				return err
			}
		case line, ok := <-runtimeCh:
			if !ok {
				runtimeCh = nil
				if ch == nil {
					return nil
				}
				continue
			}
			if err := emit(line); err != nil {
				return err
			}
		}
	}
}

func (s *Service) emitRuntimeSnapshot(ctx context.Context, row *db.Cluster, emit func(*idpv1.LogLine) error) error {
	if err := s.emitEventSnapshot(ctx, row, emit); err != nil {
		return err
	}
	if row.Status != statusRunning || s.provisioner == nil {
		return nil
	}
	if row.Provider != kubernetes.ProviderKind && row.Provider != kubernetes.ProviderMinikube {
		return nil
	}
	var emitErr error
	err := s.provisioner.StreamNodeLogs(ctx, row.Provider, row.Name, 100, false, func(line string) {
		if emitErr != nil {
			return
		}
		emitErr = emit(protoLine("node", time.Now().UTC(), line))
	})
	if emitErr != nil {
		return emitErr
	}
	if err != nil && ctx.Err() == nil {
		return emit(protoLine("node", time.Now().UTC(), err.Error()))
	}
	return nil
}

func (s *Service) emitEventSnapshot(ctx context.Context, row *db.Cluster, emit func(*idpv1.LogLine) error) error {
	client := s.clientForLogs(ctx, row)
	if client == nil {
		return nil
	}
	events, err := client.ListClusterEvents(ctx, 80)
	if err != nil {
		return nil
	}
	for _, e := range events {
		if err := emit(eventToLogLine(e)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) followRuntimeLogs(ctx context.Context, row *db.Cluster, out chan<- *idpv1.LogLine) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.pollClusterEvents(ctx, row, out)
	}()
	go func() {
		defer wg.Done()
		s.followNodeLogs(ctx, row, out)
	}()
	wg.Wait()
}

func (s *Service) followNodeLogs(ctx context.Context, row *db.Cluster, out chan<- *idpv1.LogLine) {
	if row.Provider != kubernetes.ProviderKind && row.Provider != kubernetes.ProviderMinikube {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for row.Status != statusRunning {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.repo == nil {
			return
		}
		current, err := s.repo.Get(ctx, row.ID)
		if err != nil {
			return
		}
		switch current.Status {
		case statusError, statusStopped, statusDeleting:
			return
		}
		row = current
	}
	if s.provisioner == nil {
		return
	}
	err := s.provisioner.StreamNodeLogs(ctx, row.Provider, row.Name, 150, true, func(line string) {
		sendLog(ctx, out, protoLine("node", time.Now().UTC(), line))
	})
	if err != nil && ctx.Err() == nil {
		sendLog(ctx, out, protoLine("node", time.Now().UTC(), err.Error()))
	}
}

func (s *Service) pollClusterEvents(ctx context.Context, row *db.Cluster, out chan<- *idpv1.LogLine) {
	seen := make(map[string]struct{})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	fetch := func() {
		current := row
		if s.repo != nil {
			if next, err := s.repo.Get(ctx, row.ID); err == nil {
				current = next
			}
		}
		client := s.clientForLogs(ctx, current)
		if client == nil {
			return
		}
		events, err := client.ListClusterEvents(ctx, 80)
		if err != nil {
			return
		}
		for _, e := range events {
			key := eventKey(e)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			sendLog(ctx, out, eventToLogLine(e))
		}
		if len(seen) > 4000 {
			seen = make(map[string]struct{})
		}
	}

	fetch()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetch()
		}
	}
}

func (s *Service) clientForLogs(ctx context.Context, row *db.Cluster) *kubernetes.Client {
	if row == nil {
		return nil
	}
	if row.IsActive && s.k8s != nil && s.k8s.Available() {
		return s.k8s
	}
	if row.Status != statusRunning || s.repo == nil {
		return nil
	}
	client, err := s.clientFor(ctx, row)
	if err != nil {
		return nil
	}
	return client
}

func sendLog(ctx context.Context, out chan<- *idpv1.LogLine, line *idpv1.LogLine) {
	select {
	case <-ctx.Done():
	case out <- line:
	}
}

func storedToProto(line storedLogLine) *idpv1.LogLine {
	return protoLine(line.Source, line.Timestamp, line.Message)
}

func protoLine(source string, ts time.Time, message string) *idpv1.LogLine {
	stamp := ""
	if !ts.IsZero() {
		stamp = ts.UTC().Format(time.RFC3339Nano)
	}
	return &idpv1.LogLine{
		PodName:   source,
		Timestamp: stamp,
		Message:   strings.TrimRight(message, "\r\n"),
	}
}

func eventToLogLine(e kubernetes.ClusterEvent) *idpv1.LogLine {
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	msg := fmt.Sprintf("[%s] %s %s: %s", e.Type, e.Object, e.Reason, e.Message)
	if e.Namespace != "" {
		msg = fmt.Sprintf("[%s] %s/%s %s: %s", e.Type, e.Namespace, e.Object, e.Reason, e.Message)
	}
	return protoLine("event", ts, msg)
}

func eventKey(e kubernetes.ClusterEvent) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s",
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		e.Namespace,
		e.Object,
		e.Reason,
		e.Message,
	)
}
