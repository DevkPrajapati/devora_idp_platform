package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecResult is stdout/stderr from a one-shot pod command.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExecInPod runs command in the named container. stdin may be nil.
// The rest.Config timeout is cleared on a clone so long dumps are not cut off.
func (c *Client) ExecInPod(
	ctx context.Context,
	namespace, podName, container string,
	command []string,
	stdin []byte,
) (*ExecResult, error) {
	if c == nil || c.Clientset == nil || c.Config == nil {
		return nil, fmt.Errorf("kubernetes cluster not connected")
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	cfg := rest.CopyConfig(c.Config)
	cfg.Timeout = 0

	req := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	option := &corev1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdin:     len(stdin) > 0,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}
	req.VersionedParams(option, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("exec executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	var stdinReader io.Reader
	if len(stdin) > 0 {
		stdinReader = bytes.NewReader(stdin)
	}

	streamErr := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdinReader,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	result := &ExecResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}
	if streamErr != nil {
		// remotecommand surfaces exit codes as errors; still return captured
		// output so callers can include stderr in diagnostics.
		return result, fmt.Errorf("exec: %w: %s", streamErr, truncateErr(stderr.String()))
	}
	return result, nil
}

func truncateErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}
