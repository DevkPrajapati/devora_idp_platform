// Package metrics keeps a short rolling window of cluster resource usage so
// the UI can draw real trend lines.
//
// The alternative — having the browser accumulate samples from its own polling
// — was rejected because the series would reset on every page load and differ
// between users looking at the same cluster. Sampling server-side gives one
// shared history that survives a refresh.
//
// This is deliberately in-memory and bounded, not a time-series database. It
// answers "what has the last hour looked like", which is what the dashboard
// asks. Anything longer belongs in Prometheus.
package metrics

import (
	"context"
	"sync"
	"time"
)

const (
	// Capacity is how many samples the ring holds. At the default interval
	// this is roughly one hour of history.
	Capacity = 120
	// DefaultInterval is how often the cluster is sampled.
	DefaultInterval = 30 * time.Second
)

// Sample is one point on the trend lines.
type Sample struct {
	// At is the sample time in Unix seconds. Seconds rather than RFC 3339
	// because the only consumer is a chart x-axis.
	At int64 `json:"t"`
	// CPUPercent and MemoryPercent are whole percentages, 0-100.
	CPUPercent    int32 `json:"cpu"`
	MemoryPercent int32 `json:"mem"`
}

// Source supplies one reading. Satisfied by the cluster service.
type Source interface {
	ResourceUsage(ctx context.Context) (cpuPercent, memoryPercent int32, err error)
}

// History is a fixed-size ring of samples, safe for concurrent use.
type History struct {
	mu      sync.RWMutex
	samples []Sample
	// next is the write cursor; the ring is full once count reaches Capacity.
	next  int
	count int
	now   func() time.Time
}

// New returns an empty history.
func New() *History {
	return &History{
		samples: make([]Sample, Capacity),
		now:     time.Now,
	}
}

// Add records one sample, overwriting the oldest once full.
func (h *History) Add(cpuPercent, memoryPercent int32) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.samples[h.next] = Sample{
		At:            h.now().Unix(),
		CPUPercent:    clampPercent(cpuPercent),
		MemoryPercent: clampPercent(memoryPercent),
	}
	h.next = (h.next + 1) % Capacity
	if h.count < Capacity {
		h.count++
	}
}

// Snapshot returns the samples oldest-first.
//
// Returns an empty slice rather than nil so the JSON encoder emits [] instead
// of null — a null would make the chart component branch on a second empty
// case for no reason.
func (h *History) Snapshot() []Sample {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]Sample, 0, h.count)
	if h.count == 0 {
		return out
	}

	// Once the ring has wrapped, the oldest entry sits at the write cursor.
	start := 0
	if h.count == Capacity {
		start = h.next
	}
	for i := 0; i < h.count; i++ {
		out = append(out, h.samples[(start+i)%Capacity])
	}
	return out
}

// Run samples until the context is cancelled.
//
// A failed reading is skipped rather than recorded as zero: a transient API
// error is not the same as a cluster that suddenly dropped to 0% CPU, and
// plotting it as such would invent an outage.
func (h *History) Run(ctx context.Context, source Source, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}

	// Seeded immediately so the first page load after startup has a point to
	// draw instead of an empty chart.
	h.sample(ctx, source)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sample(ctx, source)
		}
	}
}

func (h *History) sample(ctx context.Context, source Source) {
	// Bounded so one hung API call cannot stall the whole sampling loop.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cpu, mem, err := source.ResourceUsage(ctx)
	if err != nil {
		return
	}
	h.Add(cpu, mem)
}

// clampPercent guards the chart against a source that reports out of range,
// which would otherwise stretch the y-axis and flatten every real value.
func clampPercent(v int32) int32 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}
