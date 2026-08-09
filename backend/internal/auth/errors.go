package auth

import "errors"

var (
	ErrMissingToken            = errors.New("authentication required")
	ErrInsufficientPermissions = errors.New("insufficient permissions")
	// ErrUnclassifiedProcedure reports an RPC with no entry in the access
	// policy. Surfaced as PermissionDenied so an unclassified procedure fails
	// closed rather than inheriting a permissive default.
	ErrUnclassifiedProcedure = errors.New("procedure has no access policy")
)
