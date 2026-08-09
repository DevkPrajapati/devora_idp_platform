package metrics

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSnapshotIsEmptyBeforeAnySample(t *testing.T) {
	h := New()

	got := h.Snapshot()
	if got == nil {
		t.Fatal("Snapshot() returned nil; the JSON encoder would emit null instead of []")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestSnapshotReturnsSamplesOldestFirst(t *testing.T) {
	h := New()
	h.Add(10, 20)
	h.Add(30, 40)
	h.Add(50, 60)

	got := h.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	wantCPU := []int32{10, 30, 50}
	for i, want := range wantCPU {
		if got[i].CPUPercent != want {
			t.Errorf("sample %d CPU = %d, want %d", i, got[i].CPUPercent, want)
		}
	}
}

// The ring must drop the oldest entry rather than grow or start overwriting
// the newest, which would make the chart jump backwards in time.
func TestRingWrapsAndKeepsMostRecent(t *testing.T) {
	h := New()
	for i := 0; i < Capacity+10; i++ {
		h.Add(int32(i%100), 0)
	}

	got := h.Snapshot()
	if len(got) != Capacity {
		t.Fatalf("len = %d, want %d", len(got), Capacity)
	}

	// After Capacity+10 writes the window starts at write 10.
	if want := int32(10 % 100); got[0].CPUPercent != want {
		t.Errorf("oldest CPU = %d, want %d", got[0].CPUPercent, want)
	}
	if want := int32((Capacity + 9) % 100); got[len(got)-1].CPUPercent != want {
		t.Errorf("newest CPU = %d, want %d", got[len(got)-1].CPUPercent, want)
	}

	// Timestamps must be non-decreasing, or the x-axis is meaningless.
	for i := 1; i < len(got); i++ {
		if got[i].At < got[i-1].At {
			t.Fatalf("sample %d went backwards in time", i)
		}
	}
}

// An out-of-range reading would otherwise stretch the y-axis and visually
// flatten every real value.
func TestPercentagesAreClamped(t *testing.T) {
	h := New()
	h.Add(-5, 250)

	got := h.Snapshot()
	if got[0].CPUPercent != 0 {
		t.Errorf("CPU = %d, want 0", got[0].CPUPercent)
	}
	if got[0].MemoryPercent != 100 {
		t.Errorf("memory = %d, want 100", got[0].MemoryPercent)
	}
}

type stubSource struct {
	cpu, mem int32
	err      error
	calls    int
}

func (s *stubSource) ResourceUsage(context.Context) (int32, int32, error) {
	s.calls++
	return s.cpu, s.mem, s.err
}

// Run seeds immediately so the first page load after startup has a point to
// draw instead of an empty chart.
func TestRunSamplesImmediately(t *testing.T) {
	h := New()
	source := &stubSource{cpu: 42, mem: 31}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		h.Run(ctx, source, time.Hour) // long interval: only the seed should land
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for len(h.Snapshot()) == 0 {
		select {
		case <-deadline:
			t.Fatal("Run did not record a seed sample")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	got := h.Snapshot()
	if got[0].CPUPercent != 42 || got[0].MemoryPercent != 31 {
		t.Errorf("seed = %+v, want cpu=42 mem=31", got[0])
	}

	cancel()
	<-done
}

// A transient API error is not a cluster that dropped to 0%. Recording it as a
// zero would invent an outage on the chart.
func TestFailedReadingIsSkippedNotZeroed(t *testing.T) {
	h := New()
	source := &stubSource{err: errors.New("api server unreachable")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go h.Run(ctx, source, time.Hour)

	time.Sleep(100 * time.Millisecond)
	cancel()

	if got := h.Snapshot(); len(got) != 0 {
		t.Errorf("recorded %d samples from a failing source, want 0", len(got))
	}
	if source.calls == 0 {
		t.Error("source was never called")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	h := New()
	source := &stubSource{cpu: 1, mem: 1}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx, source, 10*time.Millisecond)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestConcurrentAddAndSnapshot(t *testing.T) {
	h := New()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 500; i++ {
			h.Add(int32(i%100), int32(i%50))
		}
		close(done)
	}()

	for i := 0; i < 500; i++ {
		_ = h.Snapshot()
	}
	<-done
}
