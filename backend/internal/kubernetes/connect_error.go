package kubernetes

import (
	"context"
	"errors"
	"net"
	"strings"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ConnectError maps Kubernetes transport failures to Unavailable instead of
// Internal, so the UI can distinguish "cluster down" from "server bug".
func ConnectError(err error) *connect.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeUnavailable, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return connect.NewError(connect.CodeUnavailable, err)
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "eof") ||
		strings.Contains(lower, "tls handshake") ||
		strings.Contains(lower, "no such host") {
		return connect.NewError(connect.CodeUnavailable, err)
	}
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) {
		return connect.NewError(connect.CodeUnavailable, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// IsClusterDown reports whether err means the API server is gone, not slow.
//
// Connection refused / reset / no route are the signatures of a stopped local
// cluster. A timeout is not: a loaded minikube routinely exceeds a short
// deadline, and treating that as "down" is what unbound the platform during
// ordinary slowness.
func IsClusterDown(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotConnected) {
		return true
	}
	lower := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection refused",
		"connection reset",
		"no such host",
		"no route to host",
		"network is unreachable",
		"tls: handshake timeout",
		"tls handshake timeout",
		"remote error: tls",
		"connect: operation timed out",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// HumanizeClusterError turns low-level transport errors into actionable text.
func HumanizeClusterError(reason string) string {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "eof"),
		strings.Contains(lower, "tls handshake"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "apiserver"),
		strings.Contains(lower, "no kubeconfig"):
		return "Kubernetes cluster is not reachable. Start Docker Desktop, then run: minikube start"
	default:
		return reason
	}
}
