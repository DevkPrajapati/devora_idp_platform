package kubernetes

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAppLocalHost(t *testing.T) {
	got := AppLocalHost("user-auth1", "user-web")
	if got != "user-auth1--user-web.localhost" {
		t.Fatalf("AppLocalHost = %q", got)
	}
	if requestHostName("user-auth1--user-web.localhost:8090") != "user-auth1--user-web.localhost" {
		t.Fatalf("requestHostName dropped the wrong part")
	}
	if requestListenPort("localhost:8090") != "8090" {
		t.Fatalf("requestListenPort = %q", requestListenPort("localhost:8090"))
	}
}

func TestStickyLocalPortIsStable(t *testing.T) {
	a := stickyLocalPort("user-menagement/user-web")
	b := stickyLocalPort("user-menagement/user-web")
	if a != b {
		t.Fatalf("same key produced different ports: %d vs %d", a, b)
	}
	if a < stickyPortBase || a >= stickyPortBase+stickyPortCount {
		t.Fatalf("port %d outside sticky range", a)
	}

	other := stickyLocalPort("user-menagement/user-api")
	if other == a {
		t.Fatalf("different workloads unexpectedly shared port %d", a)
	}
}

func TestNewestReadyPodNamePicksLatest(t *testing.T) {
	older := metav1.NewTime(time.Unix(1_700_000_000, 0))
	newer := metav1.NewTime(time.Unix(1_700_000_100, 0))
	ready := []corev1.PodCondition{{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	}}

	got := newestReadyPodName([]corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "user-web-old", CreationTimestamp: older},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: ready},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "user-web-new", CreationTimestamp: newer},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: ready},
		},
	})
	if got != "user-web-new" {
		t.Fatalf("newestReadyPodName = %q, want user-web-new", got)
	}
}

func TestNewestReadyPodNameSkipsTerminating(t *testing.T) {
	now := metav1.Now()
	ready := []corev1.PodCondition{{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	}}

	got := newestReadyPodName([]corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "terminating", DeletionTimestamp: &now, CreationTimestamp: now},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: ready},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "live", CreationTimestamp: metav1.NewTime(now.Add(-time.Minute))},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: ready},
		},
	})
	if got != "live" {
		t.Fatalf("newestReadyPodName = %q, want live", got)
	}
}

func TestApplyAppProxyCacheHeadersForHTML(t *testing.T) {
	req := &http.Request{URL: &url.URL{Path: "/"}}
	resp := &http.Response{
		Request: req,
		Header:  make(http.Header),
	}
	resp.Header.Set("Content-Type", "text/html")
	resp.Header.Set("ETag", `"abc"`)
	if err := applyAppProxyCacheHeaders(resp); err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if resp.Header.Get("ETag") != "" {
		t.Fatal("expected ETag to be stripped from HTML")
	}
}

func TestApplyAppProxyCacheHeadersLeavesAssets(t *testing.T) {
	req := &http.Request{URL: &url.URL{Path: "/assets/app.js"}}
	resp := &http.Response{
		Request: req,
		Header:  make(http.Header),
	}
	resp.Header.Set("Content-Type", "application/javascript")
	resp.Header.Set("ETag", `"js"`)
	if err := applyAppProxyCacheHeaders(resp); err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Cache-Control") != "" {
		t.Fatal("did not expect Cache-Control on JS assets")
	}
	if resp.Header.Get("ETag") != `"js"` {
		t.Fatal("JS ETag should stay")
	}
}
