package dbbrowse

import (
	"context"
	"testing"

	"github.com/idp/platform/backend/internal/auth"
	"github.com/idp/platform/backend/internal/config"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListDatabasesUsesReboundClientset(t *testing.T) {
	hub := kubernetes.NewHub(nil, config.KubernetesConfig{})
	svc := NewService(hub.Live())
	ctx := auth.ContextWithUser(context.Background(), &auth.User{
		ID: "u1", Username: "admin", Roles: []auth.Role{auth.RoleAdmin},
	})

	disconnected, err := svc.ListDatabases(ctx, &idpv1.ListDatabasesRequest{})
	if err != nil {
		t.Fatalf("ListDatabases while unbound: %v", err)
	}
	if disconnected.Connected {
		t.Fatal("expected connected=false before Bind")
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mongo-0",
			Namespace: "shop",
			Labels:    map[string]string{"app": "mongo"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "mongodb",
				Image: "mongo:7",
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	next := &kubernetes.Client{Clientset: fake.NewSimpleClientset(pod)}
	next.SetReachable(true)
	hub.Bind(next)

	got, err := svc.ListDatabases(ctx, &idpv1.ListDatabasesRequest{})
	if err != nil {
		t.Fatalf("ListDatabases after Bind: %v", err)
	}
	if !got.Connected {
		t.Fatal("expected connected=true after the fleet rebound the live cluster")
	}
	if len(got.Instances) != 1 || got.Instances[0].Name != "mongo" {
		t.Fatalf("instances = %+v, want one mongo workload", got.Instances)
	}
}
