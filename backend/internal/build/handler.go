package build

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"go.uber.org/zap"
)

// WebhookPath is the prefix git providers post to.
const WebhookPath = "/webhooks/git"

// maxWebhookBody caps how much of a delivery is read. Payloads are a few KiB;
// anything larger is either hostile or a provider bug, and reading it unbounded
// lets an unauthenticated caller exhaust backend memory.
const maxWebhookBody = 1 << 20

// Handler implements the Connect RPC BuildService.
type Handler struct {
	service *Service
}

// NewHandler creates a new build RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SaveGitRepository(
	ctx context.Context,
	req *connect.Request[idpv1.SaveGitRepositoryRequest],
) (*connect.Response[idpv1.SaveGitRepositoryResponse], error) {
	resp, err := h.service.SaveRepository(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListGitRepositories(
	ctx context.Context,
	req *connect.Request[idpv1.ListGitRepositoriesRequest],
) (*connect.Response[idpv1.ListGitRepositoriesResponse], error) {
	resp, err := h.service.ListRepositories(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) DeleteGitRepository(
	ctx context.Context,
	req *connect.Request[idpv1.DeleteGitRepositoryRequest],
) (*connect.Response[idpv1.DeleteGitRepositoryResponse], error) {
	resp, err := h.service.DeleteRepository(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) TriggerBuild(
	ctx context.Context,
	req *connect.Request[idpv1.TriggerBuildRequest],
) (*connect.Response[idpv1.TriggerBuildResponse], error) {
	resp, err := h.service.TriggerBuild(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RetryBuild(
	ctx context.Context,
	req *connect.Request[idpv1.RetryBuildRequest],
) (*connect.Response[idpv1.RetryBuildResponse], error) {
	resp, err := h.service.RetryBuild(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListBuilds(
	ctx context.Context,
	req *connect.Request[idpv1.ListBuildsRequest],
) (*connect.Response[idpv1.ListBuildsResponse], error) {
	resp, err := h.service.ListBuilds(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(resp), nil
}

// asConnectError preserves the status code the service chose, so a validation
// failure does not become a 500 that hides its own message.
func asConnectError(err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}
	return connect.NewError(connect.CodeInternal, err)
}

// WebhookHandler serves POST /webhooks/git/{repository_id}.
//
// A plain HTTP handler rather than a Connect RPC: git providers send their own
// payload shapes with their own headers and cannot be asked to speak Connect.
// It is also deliberately outside the auth interceptor — the caller is GitHub,
// not a platform user — which is exactly why HMAC verification is mandatory.
func WebhookHandler(service *Service, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeWebhookError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		repositoryID := strings.Trim(strings.TrimPrefix(r.URL.Path, WebhookPath), "/")
		if repositoryID == "" || strings.Contains(repositoryID, "/") {
			writeWebhookError(w, http.StatusNotFound, "unknown repository")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
		if err != nil {
			writeWebhookError(w, http.StatusBadRequest, "could not read request body")
			return
		}

		headers := WebhookHeaders{
			GitHubSignature:    r.Header.Get("X-Hub-Signature-256"),
			GitLabToken:        r.Header.Get("X-Gitlab-Token"),
			BitbucketSignature: r.Header.Get("X-Hub-Signature"),
			EventType:          firstNonEmpty(r.Header.Get("X-GitHub-Event"), r.Header.Get("X-Gitlab-Event")),
		}

		record, err := service.HandleWebhook(r.Context(), repositoryID, body, headers)
		switch {
		case err == nil:
			writeWebhookJSON(w, http.StatusAccepted, map[string]any{
				"status": "build queued",
				"build":  record.Number,
				"branch": record.Branch,
			})

		case errors.Is(err, ErrRepositoryUnknown):
			writeWebhookError(w, http.StatusNotFound, "unknown repository")

		case errors.Is(err, ErrInvalidSignature):
			if logger != nil {
				logger.Warn("rejected webhook with invalid signature",
					zap.String("repository_id", repositoryID),
					zap.String("remote", r.RemoteAddr))
			}
			writeWebhookError(w, http.StatusUnauthorized, "signature verification failed")

		case errors.Is(err, ErrUnsupportedEvent):
			// 200, not an error: the delivery was authentic and correctly
			// ignored. A non-2xx would make the provider retry and eventually
			// disable the webhook.
			writeWebhookJSON(w, http.StatusOK, map[string]any{"status": "ignored"})

		default:
			if logger != nil {
				logger.Error("webhook build failed",
					zap.String("repository_id", repositoryID), zap.Error(err))
			}
			// The provider sees a generic failure; the detail stays in our logs
			// rather than going to an unauthenticated caller.
			writeWebhookError(w, http.StatusInternalServerError, "could not start build")
		}
	})
}

func writeWebhookJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeWebhookError(w http.ResponseWriter, status int, message string) {
	writeWebhookJSON(w, status, map[string]any{"error": message})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
