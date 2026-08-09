package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// ForwardedPort is a short-lived localhost tunnel into a pod container port.
// Call Close when the caller is done inspecting the target.
type ForwardedPort struct {
	LocalPort uint16
	stopCh    chan struct{}
}

// Close tears down the port-forward. Safe to call more than once.
func (f *ForwardedPort) Close() {
	if f == nil || f.stopCh == nil {
		return
	}
	select {
	case <-f.stopCh:
	default:
		close(f.stopCh)
	}
	f.stopCh = nil
}

// PortForwardPod opens a localhost forward to podName:remotePort and waits
// until it is ready or the context is cancelled.
//
// The returned forward must be closed by the caller. The rest.Config timeout
// used for ordinary API calls is cleared on a clone so the SPDY stream is not
// killed mid-forward.
func (c *Client) PortForwardPod(
	ctx context.Context,
	namespace, podName string,
	remotePort int32,
) (*ForwardedPort, error) {
	if c == nil || c.Clientset == nil || c.Config == nil {
		return nil, fmt.Errorf("kubernetes cluster not connected")
	}
	if remotePort <= 0 {
		return nil, fmt.Errorf("remote port must be positive")
	}

	cfg := rest.CopyConfig(c.Config)
	// Ordinary API calls use a short Timeout; a live port-forward must not.
	cfg.Timeout = 0

	req := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("port-forward transport: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)

	fw, err := portforward.NewOnAddresses(
		dialer,
		[]string{"127.0.0.1"},
		[]string{fmt.Sprintf("0:%d", remotePort)},
		stopCh,
		readyCh,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		return nil, fmt.Errorf("create port-forward: %w", err)
	}

	go func() {
		if err := fw.ForwardPorts(); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	select {
	case <-readyCh:
	case err := <-errCh:
		close(stopCh)
		return nil, fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		close(stopCh)
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		close(stopCh)
		return nil, fmt.Errorf("port-forward timed out waiting to become ready")
	}

	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		return nil, fmt.Errorf("port-forward did not publish a local port")
	}

	return &ForwardedPort{LocalPort: ports[0].Local, stopCh: stopCh}, nil
}
