package base

import (
	"fmt"

	"github.com/google/uuid"
)

// TenantIDForInsert returns tid if non-nil, otherwise fallback.
// Callers extract tenant ID from context before calling this.
func TenantIDForInsert(tid, fallback uuid.UUID) uuid.UUID {
	if tid == uuid.Nil {
		return fallback
	}
	return tid
}

// RequireTenantID returns an error if tid is nil (fail-closed).
// Callers extract tenant ID from context before calling this.
func RequireTenantID(tid uuid.UUID) error {
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	return nil
}
