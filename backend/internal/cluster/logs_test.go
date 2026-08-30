package cluster

import (
	"testing"
	"time"
)

func TestClusterLogHubSubscribeReceivesAppend(t *testing.T) {
	h := newClusterLogHub()
	h.Append("c1", "provision", "queued")

	snap, ch, cancel := h.Subscribe("c1")
	defer cancel()
	if len(snap) != 1 || snap[0].Message != "queued" {
		t.Fatalf("snapshot = %#v", snap)
	}

	h.Append("c1", "provision", "creating")
	select {
	case line := <-ch:
		if line.Message != "creating" {
			t.Fatalf("got %q", line.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive append")
	}
}

func TestClusterLogHubSnapshotTails(t *testing.T) {
	h := newClusterLogHub()
	for i := 0; i < 5; i++ {
		h.Append("c1", "provision", string(rune('a'+i)))
	}
	got := h.Snapshot("c1", 2)
	if len(got) != 2 || got[0].Message != "d" || got[1].Message != "e" {
		t.Fatalf("got %#v", got)
	}
}
