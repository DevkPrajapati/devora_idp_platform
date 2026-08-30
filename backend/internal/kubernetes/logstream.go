package kubernetes

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// maxLogLineBytes caps a single line. A container writing a megabyte without a
// newline would otherwise grow the scanner's buffer until the backend runs out
// of memory — one misbehaving workload taking down the platform.
const maxLogLineBytes = 256 << 10

// defaultTailLines is the backlog replayed when a client does not ask for one.
// Enough to show why a pod is unhappy without flooding the browser on connect.
const defaultTailLines int64 = 200

// maxTailLines bounds the replay a client can request.
const maxTailLines int64 = 5000

// LogLine is one line of container output.
type LogLine struct {
	PodName string
	// Timestamp as emitted by the kubelet, RFC 3339 with nanoseconds. Empty
	// when the line arrived without a parseable timestamp.
	Timestamp string
	Message   string
}

// LogStreamOptions selects what to stream.
type LogStreamOptions struct {
	Namespace string
	PodName   string
	Container string
	TailLines int64
	// Follow keeps the stream open, emitting lines as the container writes
	// them. When false the stream ends after the existing backlog.
	Follow bool
}

const followRetry = 1500 * time.Millisecond

// StreamPodLogs sends each log line to emit until the context is cancelled,
// the container exits, or emit returns an error.
//
// Follow stays open across crash-loops: when the current container is waiting
// to start, the last crash's logs are dumped (Previous) and the stream retries
// instead of ending. That is what the deploy viewer needs — the interesting
// output is often from a container that already died.
func (c *Client) StreamPodLogs(ctx context.Context, opts LogStreamOptions, emit func(LogLine) error) error {
	tail := opts.TailLines
	if tail <= 0 {
		tail = defaultTailLines
	}
	if tail > maxTailLines {
		tail = maxTailLines
	}

	if !opts.Follow {
		err := c.streamOnce(ctx, opts, tail, false, false, nil, emit)
		if err != nil && isWaitingForLogs(err) {
			return c.streamOnce(ctx, opts, tail, false, true, nil, emit)
		}
		return err
	}

	dumpedPrevious := false
	var since *metav1.Time
	for {
		if ctx.Err() != nil {
			return nil
		}

		err := c.streamOnce(ctx, opts, tail, true, false, since, func(line LogLine) error {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, line.Timestamp); parseErr == nil {
				next := metav1.NewTime(parsed.Add(time.Nanosecond))
				since = &next
			}
			return emit(line)
		})
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			dumpedPrevious = false
			if err := waitFollowRetry(ctx); err != nil {
				return nil
			}
			continue
		}
		if isWaitingForLogs(err) {
			if !dumpedPrevious {
				_ = c.streamOnce(ctx, opts, tail, false, true, nil, emit)
				dumpedPrevious = true
			}
			if err := waitFollowRetry(ctx); err != nil {
				return nil
			}
			continue
		}
		if isTransientLogStreamError(err) {
			if err := waitFollowRetry(ctx); err != nil {
				return nil
			}
			continue
		}
		return err
	}
}

func waitFollowRetry(ctx context.Context) error {
	timer := time.NewTimer(followRetry)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) streamOnce(ctx context.Context, opts LogStreamOptions, tail int64, follow, previous bool, since *metav1.Time, emit func(LogLine) error) error {
	podLogOpts := &corev1.PodLogOptions{
		Container:  opts.Container,
		Follow:     follow,
		Previous:   previous,
		Timestamps: true,
	}
	if since != nil {
		podLogOpts.SinceTime = since
	} else {
		podLogOpts.TailLines = &tail
	}

	clientset, err := c.logStreamClientset()
	if err != nil {
		return err
	}

	stream, err := clientset.CoreV1().Pods(opts.Namespace).
		GetLogs(opts.PodName, podLogOpts).Stream(ctx)
	if err != nil {
		return fmt.Errorf("open log stream for %s/%s: %w", opts.Namespace, opts.PodName, err)
	}
	defer func() { _ = stream.Close() }()

	return scanLogStream(ctx, opts.PodName, stream, emit)
}

func scanLogStream(ctx context.Context, podName string, stream io.Reader, emit func(LogLine) error) error {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLogLineBytes)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		if err := emit(ParseLogLine(podName, scanner.Text())); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if strings.Contains(err.Error(), "token too long") {
			return fmt.Errorf("pod %s wrote a log line over %d KiB", podName, maxLogLineBytes>>10)
		}
		return fmt.Errorf("read log stream: %w", err)
	}
	return nil
}

// isWaitingForLogs reports kubelet errors from GetLogs while the container is
// not running. Follow cannot attach in that state; Previous still can.
func isWaitingForLogs(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "waiting to start") ||
		strings.Contains(lower, "containernotfound") ||
		strings.Contains(lower, "container not found") ||
		strings.Contains(lower, "containercreating") ||
		strings.Contains(lower, "podinitializing") ||
		strings.Contains(lower, "crashloopbackoff")
}

// isTransientLogStreamError is an idle-or-dropped kubelet follow. npm install
// and similar steps can sit quiet for a minute; treating that as fatal closed
// the Connect stream and left the UI stuck on "Reconnecting".
func isTransientLogStreamError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	needles := []string{
		"connection reset",
		"broken pipe",
		"unexpected eof",
		"i/o timeout",
		"timeout awaiting",
		"http2: stream closed",
		"transport is closing",
		"use of closed network connection",
		"connection refused",
		"eof",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

var (
	ansiCSI    = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	ansiOSC    = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	orphanSGR  = regexp.MustCompile(`\[[0-9;]{1,4}m`)
	ansiEscape = regexp.MustCompile(`\x1b.`)
)

func stripANSI(s string) string {
	s = ansiOSC.ReplaceAllString(s, "")
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiEscape.ReplaceAllString(s, "")
	// ESC already eaten by a previous hop leaves "[37mDEBU [0m".
	return orphanSGR.ReplaceAllString(s, "")
}

// ParseLogLine splits a kubelet-timestamped line into its parts.
//
// With Timestamps: true the kubelet prefixes each line with an RFC 3339 time
// and a single space. A line without that prefix is passed through whole rather
// than having its first word silently eaten as a timestamp.
func ParseLogLine(podName, raw string) LogLine {
	line := strings.TrimRight(raw, "\r")

	timestamp, message, found := strings.Cut(line, " ")
	if !found || !looksLikeTimestamp(timestamp) {
		return LogLine{PodName: podName, Message: stripANSI(line)}
	}
	return LogLine{PodName: podName, Timestamp: timestamp, Message: stripANSI(message)}
}

// looksLikeTimestamp does a shape check rather than a full parse. The kubelet's
// format is fixed, and parsing every line of a high-volume stream to discover
// that would cost more than it proves.
func looksLikeTimestamp(token string) bool {
	// Shortest RFC 3339 the kubelet emits: 2026-07-28T10:15:00Z
	if len(token) < 20 || len(token) > 40 {
		return false
	}
	if token[4] != '-' || token[7] != '-' || token[10] != 'T' {
		return false
	}
	last := token[len(token)-1]
	return last == 'Z' || strings.ContainsAny(token[10:], "+-")
}

func (c *Client) logStreamClientset() (kubernetes.Interface, error) {
	if c == nil {
		return nil, fmt.Errorf("kubernetes cluster not connected")
	}
	c.mu.RLock()
	if c.streamCS != nil {
		cs := c.streamCS
		c.mu.RUnlock()
		return cs, nil
	}
	cfg := c.Config
	c.mu.RUnlock()
	if cfg == nil {
		return nil, fmt.Errorf("kubernetes cluster not connected")
	}

	copied := rest.CopyConfig(cfg)
	copied.Timeout = 0
	clientset, err := kubernetes.NewForConfig(copied)
	if err != nil {
		return nil, fmt.Errorf("log-stream clientset: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.streamCS != nil {
		return c.streamCS, nil
	}
	c.streamCS = clientset
	return clientset, nil
}

// ListPodNamesForApp returns the pods backing a deployment, used by the UI to
// offer a per-pod filter.
func (c *Client) ListPodNamesForApp(ctx context.Context, namespace, app string) ([]string, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return nil, csErr
	}
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + app,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods for %s: %w", app, err)
	}

	names := make([]string, 0, len(pods.Items))
	for i := range pods.Items {
		names = append(names, pods.Items[i].Name)
	}
	return names, nil
}
