package protocol

// Error codes from OpenClaw source (ErrorCodes enum in error-codes.ts)
const (
	ErrInvalidRequest = "INVALID_REQUEST"
	ErrUnavailable    = "UNAVAILABLE"
	ErrNotLinked      = "NOT_LINKED"
	ErrNotPaired      = "NOT_PAIRED"
	ErrAgentTimeout   = "AGENT_TIMEOUT"

	// Additional codes for Go implementation
	ErrUnauthorized       = "UNAUTHORIZED"
	ErrNotFound           = "NOT_FOUND"
	ErrAlreadyExists      = "ALREADY_EXISTS"
	ErrResourceExhausted  = "RESOURCE_EXHAUSTED"
	ErrFailedPrecondition  = "FAILED_PRECONDITION"
	ErrInternal            = "INTERNAL"
	ErrTenantAccessRevoked = "TENANT_ACCESS_REVOKED"
)
