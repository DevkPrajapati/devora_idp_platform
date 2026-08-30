package dbadmin

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCredentialsReadsMongoEnvFromSecret(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mongodb-abc", Namespace: "user-auth1"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "mongodb",
				Image: "mongo:7",
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "mongodb-secrets"},
					},
				}},
			}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mongodb-secrets", Namespace: "user-auth1"},
		Data: map[string][]byte{
			"MONGO_INITDB_ROOT_USERNAME": []byte("admin"),
			"MONGO_INITDB_ROOT_PASSWORD": []byte("admin"),
		},
	}

	d := NewDiscoverer(fake.NewSimpleClientset(pod, secret))
	creds, err := d.Credentials(context.Background(), Ref{
		Namespace: "user-auth1",
		PodName:   "mongodb-abc",
		Container: "mongodb",
	}, EngineMongoDB)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if creds.User != "admin" || creds.Password != "admin" {
		t.Fatalf("got user=%q password=%q, want admin/admin from envFrom", creds.User, creds.Password)
	}
}

func TestNamedEnvOverridesEnvFrom(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mongodb-abc", Namespace: "ns"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "mongodb",
				Image: "mongo:7",
				Env: []corev1.EnvVar{{
					Name:  "MONGO_INITDB_ROOT_USERNAME",
					Value: "inline",
				}},
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "mongodb-secrets"},
					},
				}},
			}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mongodb-secrets", Namespace: "ns"},
		Data: map[string][]byte{
			"MONGO_INITDB_ROOT_USERNAME": []byte("from-secret"),
			"MONGO_INITDB_ROOT_PASSWORD": []byte("pw"),
		},
	}

	d := NewDiscoverer(fake.NewSimpleClientset(pod, secret))
	creds, err := d.Credentials(context.Background(), Ref{
		Namespace: "ns",
		PodName:   "mongodb-abc",
		Container: "mongodb",
	}, EngineMongoDB)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if creds.User != "inline" {
		t.Fatalf("explicit env should win, got user=%q", creds.User)
	}
	if creds.Password != "pw" {
		t.Fatalf("password should still come from envFrom, got %q", creds.Password)
	}
}
