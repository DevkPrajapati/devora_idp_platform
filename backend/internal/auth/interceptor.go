package auth

import (
	"context"
	"strings"

	"connectrpc.com/connect"
)

const healthProcedure = "/idp.v1.HealthService/Check"

// NewInterceptor returns a Connect RPC interceptor that validates JWT tokens.
func NewInterceptor(validator *Validator) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().Procedure == healthProcedure {
				return next(ctx, req)
			}

			if !validator.Enabled() {
				ctx = ContextWithUser(ctx, DevUser())
				return next(ctx, req)
			}

			authHeader := req.Header().Get("Authorization")
			if authHeader == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, ErrMissingToken)
			}

			user, err := validator.Validate(ctx, authHeader)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			ctx = ContextWithUser(ctx, user)
			return next(ctx, req)
		}
	}
}

// RequireRole returns an interceptor that enforces role-based access.
func RequireRole(roles ...Role) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			user, err := UserFromContext(ctx)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			for _, role := range roles {
				if user.HasRole(role) {
					return next(ctx, req)
				}
			}

			return nil, connect.NewError(connect.CodePermissionDenied, ErrInsufficientPermissions)
		}
	}
}

// BearerToken extracts the token from an Authorization header.
func BearerToken(header string) string {
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return strings.TrimSpace(header)
}
