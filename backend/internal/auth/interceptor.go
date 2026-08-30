package auth

import (
	"context"
	"strings"

	"connectrpc.com/connect"
)

const healthProcedure = "/idp.v1.HealthService/Check"

type interceptor struct {
	validator *Validator
}

// NewInterceptor returns a Connect interceptor that validates JWT tokens for
// both unary and streaming RPCs. Streaming used to skip this because only a
// UnaryInterceptorFunc was registered, which left StreamPodLogs unauthenticated
// and is also why a token-gated proxy could drop live logs.
func NewInterceptor(validator *Validator) connect.Interceptor {
	return &interceptor{validator: validator}
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := i.authenticate(ctx, req.Spec().Procedure, req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.authenticate(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func (i *interceptor) authenticate(ctx context.Context, procedure string, header httpHeader) (context.Context, error) {
	if procedure == healthProcedure {
		return ctx, nil
	}

	if !i.validator.Enabled() {
		return ContextWithUser(ctx, DevUser()), nil
	}

	authHeader := header.Get("Authorization")
	if authHeader == "" {
		return ctx, connect.NewError(connect.CodeUnauthenticated, ErrMissingToken)
	}

	user, err := i.validator.Validate(ctx, authHeader)
	if err != nil {
		return ctx, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return ContextWithUser(ctx, user), nil
}

type httpHeader interface {
	Get(string) string
}

type authzInterceptor struct{}

// NewAuthorizationInterceptor enforces the policy table on unary and streaming RPCs.
func NewAuthorizationInterceptor() connect.Interceptor {
	return authzInterceptor{}
}

func (authzInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := authorize(ctx, req.Spec().Procedure); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (authzInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (authzInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := authorize(ctx, conn.Spec().Procedure); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func authorize(ctx context.Context, procedure string) error {
	level, known := AccessFor(procedure)
	if !known {
		return connect.NewError(connect.CodePermissionDenied, ErrUnclassifiedProcedure)
	}
	if level == AccessPublic {
		return nil
	}
	user, err := UserFromContext(ctx)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, ErrMissingToken)
	}
	if !Allows(user, level) {
		return connect.NewError(connect.CodePermissionDenied, ErrInsufficientPermissions)
	}
	return nil
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
