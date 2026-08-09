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
