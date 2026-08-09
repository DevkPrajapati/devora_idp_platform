package auth

import (
	"context"
	"fmt"
)

type contextKey string

const userContextKey contextKey = "auth_user"

// Role represents a platform RBAC role.
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

// User holds authenticated user information extracted from JWT claims.
type User struct {
	ID       string
	Email    string
	Username string
	Roles    []Role
}

// HasRole checks if the user has the specified role.
func (u *User) HasRole(role Role) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsAdmin returns true if the user has the admin role.
func (u *User) IsAdmin() bool {
	return u.HasRole(RoleAdmin)
}

// CanWrite returns true if the user can perform write operations.
func (u *User) CanWrite() bool {
	return u.HasRole(RoleAdmin) || u.HasRole(RoleDeveloper)
}

// ContextWithUser stores the authenticated user in the context.
func ContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext retrieves the authenticated user from the context.
func UserFromContext(ctx context.Context) (*User, error) {
	user, ok := ctx.Value(userContextKey).(*User)
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found in context")
	}
	return user, nil
}
