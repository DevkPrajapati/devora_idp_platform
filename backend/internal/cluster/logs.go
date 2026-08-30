package cluster

import (
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const maxClusterLogLines = 4000

type storedLogLine struct {
	Source    string
	Timestamp time.Time
	Message   string
}

type clusterLogHub struct {
	mu   sync.Mutex
	bufs map[string]*clusterLogBuf
}

func newClusterLogHub() *clusterLogHub {
	return &clusterLogHub{bufs: make(map[string]*clusterLogBuf)}
}

type clusterLogBuf struct {
	mu    sync.Mutex
	lines []storedLogLine
	subs  map[chan storedLogLine]struct{}
}

func (h *clusterLogHub) buf(id string) *clusterLogBuf {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.bufs[id]
	if !ok {
		b = &clusterLogBuf{subs: make(map[chan storedLogLine]struct{})}
		h.bufs[id] = b
	}
	return b
}

func (h *clusterLogHub) Append(id, source, message string) {
	if id == "" || message == "" {
		return
	}
	line := storedLogLine{
		Source:    source,
		Timestamp: time.Now().UTC(),
		Message:   message,
	}
	b := h.buf(id)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
	if len(b.lines) > maxClusterLogLines {
		b.lines = b.lines[len(b.lines)-maxClusterLogLines:]
	}
	for ch := range b.subs {
		select {
		case ch <- line:
		default:
			// A slow viewer must not stall kind create.
		}
	}
}

func (h *clusterLogHub) Snapshot(id string, tail int) []storedLogLine {
	b := h.buf(id)
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := b.lines
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	out := make([]storedLogLine, len(lines))
	copy(out, lines)
	return out
}

func (h *clusterLogHub) Subscribe(id string) (snapshot []storedLogLine, ch chan storedLogLine, cancel func()) {
	b := h.buf(id)
	ch = make(chan storedLogLine, 256)
	b.mu.Lock()
	snapshot = append([]storedLogLine(nil), b.lines...)
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel = func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return snapshot, ch, cancel
}

func clusterIDString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	val, err := id.Value()
	if err != nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}
