package cluster

import (
	"context"
	"errors"
	"testing"

	db "github.com/idp/platform/backend/internal/database/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func newClusterRow(name string) *db.Cluster {
	return &db.Cluster{
		ID:       pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Name:     name,
		Provider: "minikube",
		Status:   statusRunning,
	}
}

func TestClassifyIdentity(t *testing.T) {
	tests := []struct {
		name     string
		recorded string
		live     string
		want     identityVerdict
	}{
		{
			name:     "same cluster still answering",
			recorded: "uid-a",
			live:     "uid-a",
			want:     identityUnchanged,
		},
		{
			name:     "nothing recorded yet",
			recorded: "",
			live:     "uid-a",
			want:     identityFirstSeen,
		},
		{
			// The case that used to go unnoticed: a local cluster deleted and
			// recreated keeps its profile name and can reuse its API server
			// port, so only the UID reveals that the platform is now talking to
			// a different, empty cluster.
			name:     "a different cluster now answers",
			recorded: "uid-a",
			live:     "uid-b",
			want:     identityRebuilt,
		},
		{
			// An unreadable identity must not be mistaken for a rebuild; that
			// would retire live namespaces over a transient API failure.
			name:     "live identity unknown",
			recorded: "uid-a",
			live:     "",
			want:     identityUnchanged,
		},
		{
			name:     "neither known",
			recorded: "",
			live:     "",
			want:     identityUnchanged,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyIdentity(tc.recorded, tc.live); got != tc.want {
				t.Fatalf("classifyIdentity(%q, %q) = %v, want %v",
					tc.recorded, tc.live, got, tc.want)
			}
		})
	}
}

// fakeState records the retirement calls the rebuild path makes.
type fakeState struct {
	calledWith []string
	retire     []string
	err        error
}

func (f *fakeState) RetireForeignClusters(_ context.Context, clusterUID string) ([]string, error) {
	f.calledWith = append(f.calledWith, clusterUID)
	return f.retire, f.err
}

func (f *fakeState) RetireCluster(_ context.Context, clusterUID string) ([]string, error) {
	f.calledWith = append(f.calledWith, "cluster:"+clusterUID)
	return f.retire, f.err
}

func (f *fakeState) RetireUnprovenanced(_ context.Context) ([]string, error) {
	f.calledWith = append(f.calledWith, "unprovenanced")
	return nil, f.err
}

func TestPurgeRebuiltClusterStateRetiresAgainstTheCurrentCluster(t *testing.T) {
	state := &fakeState{retire: []string{"old-ns-a", "old-ns-b"}}
	svc := &Service{state: state, logs: newClusterLogHub()}

	svc.purgeRebuiltClusterState(context.Background(), newClusterRow("minikube"), "uid-old", "uid-new")

	if len(state.calledWith) != 2 {
		t.Fatalf("retire called %d times, want 2 (foreign + unprovenanced)", len(state.calledWith))
	}
	// Retirement is scoped by the surviving cluster's UID, so rows belonging to
	// it are kept and everything else is retired.
	if state.calledWith[0] != "uid-new" {
		t.Fatalf("retired against %q, want the current cluster uid", state.calledWith[0])
	}
	if state.calledWith[1] != "unprovenanced" {
		t.Fatalf("second retire = %q, want unprovenanced", state.calledWith[1])
	}
}

func TestPurgeRebuiltClusterStateToleratesRetirementFailure(t *testing.T) {
	state := &fakeState{err: errors.New("database is down")}
	svc := &Service{state: state, logs: newClusterLogHub()}

	// A failure to retire must not panic or abort the connect path: the cluster
	// is still usable, and the stale rows are re-checked on the next read.
	svc.purgeRebuiltClusterState(context.Background(), newClusterRow("minikube"), "uid-old", "uid-new")
}

func TestPurgeRebuiltClusterStateWithoutReconciler(t *testing.T) {
	svc := &Service{logs: newClusterLogHub()}
	svc.purgeRebuiltClusterState(context.Background(), newClusterRow("minikube"), "uid-old", "uid-new")
}
