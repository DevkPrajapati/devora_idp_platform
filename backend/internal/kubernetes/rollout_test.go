package kubernetes

import (
	"strconv"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func replicaSet(revision int64, image string) appsv1.ReplicaSet {
	annotations := map[string]string{}
	if revision > 0 {
		annotations[annotationRevision] = strconv.FormatInt(revision, 10)
	}
	return appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "app-" + strconv.FormatInt(revision, 10),
			Annotations: annotations,
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                "app",
						podTemplateHashLabel: "abc123",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: image}},
				},
			},
		},
	}
}

func imageOf(rs *appsv1.ReplicaSet) string {
	return rs.Spec.Template.Spec.Containers[0].Image
}

func TestSelectRollbackTargetWithExplicitRevision(t *testing.T) {
	sets := []appsv1.ReplicaSet{
		replicaSet(1, "acme/api:1.0"),
		replicaSet(2, "acme/api:1.1"),
		replicaSet(3, "acme/api:1.2"),
	}

	got := selectRollbackTarget(sets, 1, 3)
	if got == nil {
		t.Fatal("revision 1 not found")
	}
	if img := imageOf(got); img != "acme/api:1.0" {
		t.Errorf("selected image %q, want acme/api:1.0", img)
	}
}

func TestSelectRollbackTargetDefaultsToPreviousRevision(t *testing.T) {
	sets := []appsv1.ReplicaSet{
		replicaSet(1, "acme/api:1.0"),
		replicaSet(3, "acme/api:1.2"),
		replicaSet(2, "acme/api:1.1"),
	}

	// revision 0 means "undo", i.e. the highest revision below the current one.
	// Picking the numerically highest, or the first in list order, would both
	// be wrong here — the slice is deliberately unordered.
	got := selectRollbackTarget(sets, 0, 3)
	if got == nil {
		t.Fatal("no target selected")
	}
	if img := imageOf(got); img != "acme/api:1.1" {
		t.Errorf("undo selected %q, want the previous revision acme/api:1.1", img)
	}
}

func TestSelectRollbackTargetSkipsRevisionsAboveCurrent(t *testing.T) {
	// A revision above the current one can exist after a previous rollback:
	// rolling "back" to it would roll forward instead.
	sets := []appsv1.ReplicaSet{
		replicaSet(1, "acme/api:1.0"),
		replicaSet(2, "acme/api:1.1"),
		replicaSet(5, "acme/api:1.4"),
	}

	got := selectRollbackTarget(sets, 0, 2)
	if got == nil {
		t.Fatal("no target selected")
	}
	if img := imageOf(got); img != "acme/api:1.0" {
		t.Errorf("undo selected %q, want acme/api:1.0", img)
	}
}

func TestSelectRollbackTargetReturnsNilWhenNothingMatches(t *testing.T) {
	sets := []appsv1.ReplicaSet{replicaSet(1, "acme/api:1.0")}

	if got := selectRollbackTarget(sets, 99, 1); got != nil {
		t.Error("a revision that does not exist should select nothing")
	}
	// Only one revision, and it is current: there is nothing to undo to.
	if got := selectRollbackTarget(sets, 0, 1); got != nil {
		t.Error("undo with no earlier revision should select nothing")
	}
	if got := selectRollbackTarget(nil, 0, 1); got != nil {
		t.Error("empty history should select nothing")
	}
}

func TestSelectRollbackTargetIgnoresUnannotatedReplicaSets(t *testing.T) {
	// A ReplicaSet with no revision annotation is not part of the history.
	// Treating it as revision 0 would let it win the "previous revision" race
	// and roll back to an unrelated template.
	sets := []appsv1.ReplicaSet{
		replicaSet(0, "acme/orphan:1.0"),
		replicaSet(1, "acme/api:1.0"),
	}

	got := selectRollbackTarget(sets, 0, 2)
	if got == nil {
		t.Fatal("no target selected")
	}
	if img := imageOf(got); img != "acme/api:1.0" {
		t.Errorf("selected %q, want the annotated revision acme/api:1.0", img)
	}
}

// The pod-template-hash label is computed by the Deployment controller. Writing
// a template back with the old hash still on it makes the controller's own hash
// disagree with the label, and it churns creating ReplicaSets.
func TestRollbackStripsPodTemplateHash(t *testing.T) {
	target := replicaSet(1, "acme/api:1.0")

	restored := target.Spec.Template.DeepCopy()
	delete(restored.Labels, podTemplateHashLabel)

	if _, present := restored.Labels[podTemplateHashLabel]; present {
		t.Error("pod-template-hash survived into the restored template")
	}
	if restored.Labels["app"] != "app" {
		t.Error("stripping the hash removed the app label too")
	}
	// The source ReplicaSet must not be mutated — it is a live cluster object.
	if _, present := target.Spec.Template.Labels[podTemplateHashLabel]; !present {
		t.Error("the source ReplicaSet template was mutated")
	}
}

func TestRevisionOf(t *testing.T) {
	cases := map[string]struct {
		annotations map[string]string
		want        int64
	}{
		"valid":        {map[string]string{annotationRevision: "7"}, 7},
		"missing":      {map[string]string{}, 0},
		"nil map":      {nil, 0},
		"not a number": {map[string]string{annotationRevision: "abc"}, 0},
		"empty value":  {map[string]string{annotationRevision: ""}, 0},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			if got := revisionOf(tt.annotations); got != tt.want {
				t.Errorf("revisionOf = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWithChangeCause(t *testing.T) {
	got := withChangeCause(nil, "rollback to revision 2")
	if got[annotationChangeCause] != "rollback to revision 2" {
		t.Errorf("change cause not set on a nil map: %v", got)
	}

	existing := map[string]string{"keep": "me"}
	got = withChangeCause(existing, "rollback to revision 3")
	if got["keep"] != "me" {
		t.Error("existing annotations were dropped")
	}
	if got[annotationChangeCause] != "rollback to revision 3" {
		t.Error("change cause not overwritten")
	}
}
