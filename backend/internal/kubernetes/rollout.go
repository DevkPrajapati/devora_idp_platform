package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// annotationRevision is where the Deployment controller records which revision
// a ReplicaSet represents. It is the only link between a ReplicaSet and its
// place in the rollout history.
const annotationRevision = "deployment.kubernetes.io/revision"

// annotationChangeCause is the conventional "why" for a revision, set by
// `kubectl --record` and by this platform on rollback.
const annotationChangeCause = "kubernetes.io/change-cause"

// podTemplateHashLabel is added to a ReplicaSet's pod template by the
// Deployment controller. It must be stripped before that template is written
// back to a Deployment, or the controller sees a template whose hash does not
// match its own computation and churns creating ReplicaSets.
const podTemplateHashLabel = "pod-template-hash"

// RolloutInfo is one revision of a Deployment.
type RolloutInfo struct {
	Revision      int64
	CreatedAt     time.Time
	Image         string
	Replicas      int32
	ReadyReplicas int32
	Status        string
	ChangeCause   string
	Current       bool
}

// ListRollouts reconstructs a Deployment's revision history from the
// ReplicaSets Kubernetes retains, newest first.
//
// There is no separate history object in Kubernetes: `kubectl rollout history`
// reads exactly this. How far back it goes is governed by the Deployment's
// revisionHistoryLimit (default 10), not by anything the platform controls.
func (c *Client) ListRollouts(ctx context.Context, namespace, name string) ([]RolloutInfo, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	deployment, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	replicaSets, err := c.ownedReplicaSets(ctx, deployment)
	if err != nil {
		return nil, err
	}

	currentRevision := revisionOf(deployment.Annotations)

	rollouts := make([]RolloutInfo, 0, len(replicaSets))
	for i := range replicaSets {
		rs := &replicaSets[i]
		revision := revisionOf(rs.Annotations)
		if revision == 0 {
			// A ReplicaSet with no revision annotation is not part of the
			// rollout history; including it would produce a bogus "revision 0".
			continue
		}

		info := RolloutInfo{
			Revision:      revision,
			CreatedAt:     rs.CreationTimestamp.Time,
			ReadyReplicas: rs.Status.ReadyReplicas,
			ChangeCause:   rs.Annotations[annotationChangeCause],
			Current:       revision == currentRevision,
		}
		if rs.Spec.Replicas != nil {
			info.Replicas = *rs.Spec.Replicas
		}
		if len(rs.Spec.Template.Spec.Containers) > 0 {
			info.Image = rs.Spec.Template.Spec.Containers[0].Image
		}
		if info.Current {
			info.Status = "Active"
		} else {
			info.Status = "Superseded"
		}

		rollouts = append(rollouts, info)
	}

	sort.Slice(rollouts, func(i, j int) bool {
		return rollouts[i].Revision > rollouts[j].Revision
	})
	return rollouts, nil
}

// ownedReplicaSets returns the ReplicaSets belonging to a Deployment.
//
// Ownership is checked through ownerReferences rather than the label selector
// alone: a selector can legitimately match ReplicaSets from another Deployment
// in the same namespace, and rolling back onto one of those would swap in a
// completely unrelated pod template.
func (c *Client) ownedReplicaSets(ctx context.Context, deployment *appsv1.Deployment) ([]appsv1.ReplicaSet, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("build selector: %w", err)
	}

	list, err := cs.AppsV1().ReplicaSets(deployment.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list replicasets: %w", err)
	}

	owned := make([]appsv1.ReplicaSet, 0, len(list.Items))
	for i := range list.Items {
		for _, ref := range list.Items[i].OwnerReferences {
			if ref.UID == deployment.UID {
				owned = append(owned, list.Items[i])
				break
			}
		}
	}
	return owned, nil
}

// RollbackDeployment restores a Deployment's pod template from a previous
// revision, the same operation as `kubectl rollout undo`.
//
// revision 0 selects the most recent revision that is not the current one.
// Returns the revision that was rolled back to.
func (c *Client) RollbackDeployment(ctx context.Context, namespace, name string, revision int64) (int64, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return 0, csErr
	}
	api := cs.AppsV1().Deployments(namespace)

	deployment, err := api.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("get deployment: %w", err)
	}

	replicaSets, err := c.ownedReplicaSets(ctx, deployment)
	if err != nil {
		return 0, err
	}
	currentRevision := revisionOf(deployment.Annotations)

	target := selectRollbackTarget(replicaSets, revision, currentRevision)
	if target == nil {
		if revision > 0 {
			return 0, fmt.Errorf("revision %d not found in rollout history", revision)
		}
		return 0, fmt.Errorf("no previous revision to roll back to")
	}

	targetRevision := revisionOf(target.Annotations)
	if targetRevision == currentRevision {
		return 0, fmt.Errorf("revision %d is already active", targetRevision)
	}

	// Snapshotted before the retry loop: re-deriving it inside would read from
	// a Deployment this function may have already modified.
	restored := target.Spec.Template.DeepCopy()
	delete(restored.Labels, podTemplateHashLabel)

	rollbackErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, getErr := api.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}

		current.Spec.Template = *restored.DeepCopy()
		// Records why the revision changed, so the history explains itself
		// instead of showing an unexplained image reversal.
		current.Annotations = withChangeCause(current.Annotations,
			fmt.Sprintf("rollback to revision %d", targetRevision))

		_, upErr := api.Update(ctx, current, metav1.UpdateOptions{})
		return upErr
	})
	if rollbackErr != nil {
		return 0, fmt.Errorf("rollback deployment %s/%s: %w", namespace, name, rollbackErr)
	}

	return targetRevision, nil
}

// selectRollbackTarget picks the ReplicaSet to restore. An explicit revision
// must match exactly; revision 0 means the highest revision below the current
// one, which is what `kubectl rollout undo` does with no --to-revision.
func selectRollbackTarget(replicaSets []appsv1.ReplicaSet, wanted, current int64) *appsv1.ReplicaSet {
	var best *appsv1.ReplicaSet
	var bestRevision int64

	for i := range replicaSets {
		rs := &replicaSets[i]
		revision := revisionOf(rs.Annotations)
		if revision == 0 {
			continue
		}

		if wanted > 0 {
			if revision == wanted {
				return rs
			}
			continue
		}

		if revision < current && revision > bestRevision {
			best, bestRevision = rs, revision
		}
	}
	return best
}

func revisionOf(annotations map[string]string) int64 {
	raw, ok := annotations[annotationRevision]
	if !ok {
		return 0
	}
	revision, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return revision
}

func withChangeCause(annotations map[string]string, cause string) map[string]string {
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[annotationChangeCause] = cause
	return annotations
}
