package cluster

import (
	"context"
	"time"

	db "github.com/idp/platform/backend/internal/database/sqlc"
	"github.com/idp/platform/backend/internal/kubernetes"
	"go.uber.org/zap"
)

// identityTimeout bounds the single namespace GET that fingerprints a cluster.
// It is deliberately short: this runs on the connect path, and a cluster that
// cannot answer one GET is not one the platform should adopt state from.
const identityTimeout = 6 * time.Second

// StateReconciler retires platform state describing a cluster that no longer
// exists. Implemented by the namespace repository; declared as an interface so
// the cluster package does not depend on tenant storage.
type StateReconciler interface {
	RetireForeignClusters(ctx context.Context, clusterUID string) ([]string, error)
	RetireCluster(ctx context.Context, clusterUID string) ([]string, error)
	RetireUnprovenanced(ctx context.Context) ([]string, error)
}

// identityVerdict is what comparing a recorded cluster identity against the
// live one implies for the platform's records.
type identityVerdict int

const (
	// identityUnchanged means the same cluster is still answering.
	identityUnchanged identityVerdict = iota
	// identityFirstSeen means nothing was recorded before, so no state can have
	// come from a different cluster.
	identityFirstSeen
	// identityRebuilt means a different cluster now answers for this row, and
	// everything the platform derived from the old one is void.
	identityRebuilt
)

// classifyIdentity decides what a recorded-versus-live identity comparison
// means. Split out from the surrounding I/O because the branch that matters —
// telling "first connect" apart from "the cluster was replaced" — is the one
// worth pinning down in tests: treating a rebuild as a first connect is what
// let a destroyed cluster's namespaces keep being served as live.
func classifyIdentity(recorded, live string) identityVerdict {
	switch {
	case live == "" || recorded == live:
		return identityUnchanged
	case recorded == "":
		return identityFirstSeen
	default:
		return identityRebuilt
	}
}

// adoptIdentity fingerprints the cluster behind client and reconciles the
// platform's record of it.
//
// This is the fix for deleting a cluster and later starting a new one under the
// same name: the fleet row, its namespaces and everything else keyed to that
// row used to be carried over wholesale, because name and server URL are the
// only things the platform compared and a recreated local cluster reuses both.
// Comparing the kube-system UID instead makes a rebuild detectable, and a
// detected rebuild retires the state that died with the old cluster instead of
// presenting it as live.
//
// Failure to read the identity is not fatal. The cluster is still usable; the
// platform simply cannot prove which one it is, so it leaves the recorded
// identity untouched rather than overwriting it with a guess.
func (s *Service) adoptIdentity(ctx context.Context, row *db.Cluster, client *kubernetes.Client) {
	if row == nil || client == nil {
		return
	}

	uidCtx, cancel := context.WithTimeout(ctx, identityTimeout)
	defer cancel()
	uid, err := client.ClusterUID(uidCtx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("could not read cluster identity",
				zap.String("cluster", row.Name), zap.Error(err))
		}
		return
	}

	previous := ""
	if row.ClusterUid != nil {
		previous = *row.ClusterUid
	}

	verdict := classifyIdentity(previous, uid)
	if verdict == identityUnchanged {
		return
	}

	if _, err := s.repo.SetIdentity(ctx, row.ID, uid); err != nil {
		if s.logger != nil {
			s.logger.Warn("could not record cluster identity",
				zap.String("cluster", row.Name), zap.Error(err))
		}
		return
	}

	if verdict == identityFirstSeen {
		if s.logger != nil {
			s.logger.Info("cluster identity recorded",
				zap.String("cluster", row.Name), zap.String("cluster_uid", uid))
		}
		return
	}

	s.purgeRebuiltClusterState(ctx, row, previous, uid)
}

// repairProviders reclassifies rows that were seeded as "imported" but are
// really local clusters this host owns.
//
// Earlier releases recorded the environment cluster as imported regardless of
// which tool created it. An imported cluster is treated as compute the platform
// does not own, so stop only disconnected it, restart only re-pinged it, and
// delete only removed the database row — the real cluster kept running with all
// of its namespaces and pods, which is why deleting a cluster appeared to do
// nothing. Reclassifying is safe only when the local tool confirms a cluster of
// exactly that name exists, so an unrelated remote cluster is never adopted.
func (s *Service) repairProviders(ctx context.Context) {
	if s.repo == nil || s.provisioner == nil {
		return
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		return
	}
	for i := range rows {
		row := &rows[i]
		if row.Provider != kubernetes.ProviderImported {
			continue
		}
		provider := s.detectLocalProvider(ctx, row.Name)
		if provider == "" {
			continue
		}
		if _, err := s.repo.SetProvider(ctx, row.ID, provider); err != nil {
			if s.logger != nil {
				s.logger.Warn("could not reclassify cluster provider",
					zap.String("cluster", row.Name), zap.Error(err))
			}
			continue
		}
		if s.logger != nil {
			s.logger.Info("reclassified locally provisioned cluster",
				zap.String("cluster", row.Name),
				zap.String("provider", provider),
				zap.String("was", kubernetes.ProviderImported))
		}
		s.logLine(row.ID, "lifecycle",
			"reclassified as a "+provider+" cluster; start, stop and delete now act on the real cluster")
		s.record(ctx, "cluster.reclassify", row.Name, "success", map[string]any{
			"provider": provider,
			"was":      kubernetes.ProviderImported,
		})
	}
}

// detectLocalProvider returns the local tool that owns a cluster of this exact
// name, or empty when no local tool does.
func (s *Service) detectLocalProvider(ctx context.Context, name string) string {
	if exists, err := s.provisioner.MinikubeProfileExists(ctx, name); err == nil && exists {
		return kubernetes.ProviderMinikube
	}
	if exists, err := s.provisioner.KindClusterExists(ctx, name); err == nil && exists {
		return kubernetes.ProviderKind
	}
	return ""
}

// purgeRebuiltClusterState retires records belonging to the cluster that used
// to answer for this fleet row.
func (s *Service) purgeRebuiltClusterState(ctx context.Context, row *db.Cluster, previous, current string) {
	if s.logger != nil {
		s.logger.Warn("cluster was rebuilt; retiring state from the previous cluster",
			zap.String("cluster", row.Name),
			zap.String("previous_cluster_uid", previous),
			zap.String("cluster_uid", current))
	}
	s.logLine(row.ID, "lifecycle",
		"cluster identity changed: this is a new cluster, retiring records from the previous one")

	retired := []string{}
	if s.state != nil {
		names, err := s.state.RetireForeignClusters(ctx, current)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("could not retire namespaces from the previous cluster", zap.Error(err))
			}
		} else {
			retired = append(retired, names...)
		}
		// Rows created before identity tracking have no UID. On a rebuild they
		// cannot belong to the new cluster — leaving them is what kept deleted
		// namespaces visible after minikube delete && start.
		unprovenanced, err := s.state.RetireUnprovenanced(ctx)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("could not retire unprovenanced namespaces", zap.Error(err))
			}
		} else {
			retired = append(retired, unprovenanced...)
		}
	}

	for _, name := range retired {
		s.logLine(row.ID, "lifecycle", "retired namespace record "+name+" (belonged to the previous cluster)")
	}

	s.record(ctx, "cluster.rebuild_detected", row.Name, "success", map[string]any{
		"previous_cluster_uid": previous,
		"cluster_uid":          current,
		"retired_namespaces":   retired,
	})
}
