package kubernetes

import (
	"errors"
	"testing"
)

func TestAvailableOnEmptyClient(t *testing.T) {
	var c *Client
	if c.Available() || c.Bound() {
		t.Fatal("nil client must be neither bound nor available")
	}

	empty := &Client{}
	if empty.Available() || empty.Bound() {
		t.Fatal("zero client must be neither bound nor available")
	}

	empty.SetReachable(true)
	if empty.Available() || empty.Bound() {
		t.Fatal("reachable without a clientset is still disconnected")
	}

	empty.Bind(nil)
	if empty.Bound() || empty.Available() {
		t.Fatal("Bind(nil) must disconnect")
	}
}

func TestIsClusterDown(t *testing.T) {
	if IsClusterDown(nil) {
		t.Fatal("nil is not down")
	}
	if !IsClusterDown(ErrNotConnected) {
		t.Fatal("ErrNotConnected is down")
	}
	if !IsClusterDown(errors.New("dial tcp 127.0.0.1:8443: connect: connection refused")) {
		t.Fatal("connection refused is down")
	}
	if IsClusterDown(errors.New("context deadline exceeded")) {
		t.Fatal("a timeout is slowness, not a down cluster")
	}
}
