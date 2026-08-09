package kubernetes

import (
	"bufio"
	"context"
	"fmt"
	"strings"

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

// StreamPodLogs sends each log line to emit until the context is cancelled,
// the container exits, or emit returns an error.
//
// Lines are delivered one at a time rather than buffered: the point of the
// feature is that a developer watching a failing pod sees output as it happens,
// and any batching would reintroduce the delay this replaces.
//
// An error from emit stops the stream and is returned, which is how a
// disconnected client tears the whole pipeline down instead of leaving a
// goroutine reading logs nobody receives.
func (c *Client) StreamPodLogs(ctx context.Context, opts LogStreamOptions, emit func(LogLine) error) error {
	tail := opts.TailLines
	if tail <= 0 {
		tail = defaultTailLines
	}
	if tail > maxTailLines {
		tail = maxTailLines
	}

	podLogOpts := &corev1.PodLogOptions{
		Container: opts.Container,
		Follow:    opts.Follow,
		TailLines: &tail,
		// Timestamps are requested from the kubelet rather than stamped on
		// arrival: a line's own time is when the container wrote it, which is
		// what a developer correlating an incident actually needs.
		Timestamps: true,
	}

	// The shared clientset is built with a short rest.Config.Timeout so unary
	// API calls fail fast. That timeout also covers the whole GetLogs HTTP
	// request — including Follow — so a live tail would die after a few
	// seconds and the UI would spin on "reconnecting…". Use a zero-timeout
	// client for this stream only.
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

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLogLineBytes)

	for scanner.Scan() {
		// Checked every line so a cancelled request stops promptly instead of
		// blocking until the container next writes something.
		if ctx.Err() != nil {
			return nil
		}

		if err := emit(ParseLogLine(opts.PodName, scanner.Text())); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		// A cancelled context surfaces here as a read error on a closed body.
		// That is the client leaving, not a failure worth reporting.
		if ctx.Err() != nil {
			return nil
		}
		// An over-long line is the one scanner error worth naming, since
		// "token too long" tells the user nothing about which pod misbehaved.
		if strings.Contains(err.Error(), "token too long") {
			return fmt.Errorf("pod %s wrote a log line over %d KiB", opts.PodName, maxLogLineBytes>>10)
		}
		return fmt.Errorf("read log stream: %w", err)
	}
	return nil
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
		return LogLine{PodName: podName, Message: line}
	}
	return LogLine{PodName: podName, Timestamp: timestamp, Message: message}
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
	if c == nil || c.Config == nil {
		return nil, fmt.Errorf("kubernetes cluster not connected")
	}
	cfg := rest.CopyConfig(c.Config)
	cfg.Timeout = 0
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("log-stream clientset: %w", err)
	}
	return clientset, nil
}

// ListPodNamesForApp returns the pods backing a deployment, used by the UI to
// offer a per-pod filter.
func (c *Client) ListPodNamesForApp(ctx context.Context, namespace, app string) ([]string, error) {
	pods, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
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
