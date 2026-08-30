package cluster

import "sync"

// jobSet prevents overlapping lifecycle operations on the same cluster.
//
// kind/minikube create, start, stop and delete take minutes. Running two of
// them against one profile at once leaves the cluster in a state neither
// command expected — which is how a delete-then-create reused the old profile.
type jobSet struct {
	mu    sync.Mutex
	owner map[string]string
}

func (j *jobSet) begin(id, op string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.owner == nil {
		j.owner = map[string]string{}
	}
	if _, busy := j.owner[id]; busy {
		return false
	}
	j.owner[id] = op
	return true
}

func (j *jobSet) end(id string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.owner, id)
}

func busyStatus(status string) bool {
	switch status {
	case statusProvisioning, statusStarting, statusStopping, statusDeleting:
		return true
	default:
		return false
	}
}
