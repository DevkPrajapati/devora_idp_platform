package kubernetes

import "testing"

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
