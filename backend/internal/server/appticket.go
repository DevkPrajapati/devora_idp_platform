package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/idp/platform/backend/internal/auth"
	"github.com/idp/platform/backend/internal/kubernetes"
	"go.uber.org/zap"
)

// AppTicketPath mints a short-lived link to a running workload.
//
// This is a plain HTTP endpoint rather than a Connect RPC because it belongs to
// the same browser-integration surface as /apps/ and the git webhook: its
// output is a URL a browser navigates to, not data a typed client consumes.
const AppTicketPath = "/api/app-access/ticket"

type appTicketRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type appTicketResponse struct {
	// URL is ready to hand to window.open — path plus signed ticket.
	URL string `json:"url"`
	// ExpiresInSeconds lets the caller decide whether to re-mint rather than
	// reuse a stale link.
	ExpiresInSeconds int `json:"expiresInSeconds"`
}

// appTicketHandler authenticates the caller, then issues a ticket that /apps/
// will accept for exactly one workload.
//
// This endpoint is where authorization for click-to-open actually happens. The
// redirect handler downstream only verifies the signature — it trusts that this
// handler would not have signed anything the caller was not entitled to.
func appTicketHandler(
	validator *auth.Validator,
	signer *auth.TicketSigner,
	logger *zap.Logger,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		user, err := authenticateRequest(r, validator)
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		var req appTicketRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Namespace = strings.TrimSpace(req.Namespace)
		req.Name = strings.TrimSpace(req.Name)
		if req.Namespace == "" || req.Name == "" {
			http.Error(w, "namespace and name are required", http.StatusBadRequest)
			return
		}

		ticket, err := signer.Mint(req.Namespace, req.Name, user.ID)
		if err != nil {
			logger.Error("mint app access ticket", zap.Error(err))
			http.Error(w, "could not issue access link", http.StatusInternalServerError)
			return
		}

		// Logged at info because it is a privileged action: it grants a browser
		// a direct tunnel into a pod, and the trail should show who asked.
		logger.Info("issued app access ticket",
			zap.String("user", user.ID),
			zap.String("namespace", req.Namespace),
			zap.String("workload", req.Name))

		target := fmt.Sprintf("%s%s/%s?%s=%s",
			kubernetes.AppsPathPrefix,
			url.PathEscape(req.Namespace),
			url.PathEscape(req.Name),
			kubernetes.TicketQueryParam,
			url.QueryEscape(ticket))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(appTicketResponse{
			URL:              target,
			ExpiresInSeconds: int(auth.TicketTTL.Seconds()),
		})
	})
}

// authenticateRequest resolves the caller of a plain HTTP endpoint.
//
// The Connect interceptor cannot be reused here because it operates on
// connect.AnyRequest, so this mirrors its contract: when validation is disabled
// the caller is the dev user, and otherwise a valid bearer token is mandatory.
func authenticateRequest(r *http.Request, validator *auth.Validator) (*auth.User, error) {
	if !validator.Enabled() {
		return auth.DevUser(), nil
	}

	header := r.Header.Get("Authorization")
	if header == "" {
		return nil, auth.ErrMissingToken
	}

	return validator.Validate(r.Context(), header)
}
