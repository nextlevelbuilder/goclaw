package base

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// --- Nullable helpers ---
// Convert Go zero values to nil pointers for nullable DB columns.

// NilStr returns nil for empty strings, pointer otherwise.
func NilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// NilInt returns nil for zero, pointer otherwise.
func NilInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// NilUUID returns nil for uuid.Nil, pointer otherwise.
func NilUUID(u *uuid.UUID) *uuid.UUID {
	if u == nil || *u == uuid.Nil {
		return nil
	}
	return u
}

// NilTime returns nil for nil/zero time, pointer otherwise.
func NilTime(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}

// --- Deref helpers ---
// Safely dereference nullable pointers with zero-value defaults.

// DerefStr returns "" for nil, value otherwise.
func DerefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// DerefInt returns 0 for nil, value otherwise.
func DerefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// DerefUUID returns uuid.Nil for nil, value otherwise.
func DerefUUID(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}

// DerefBytes returns nil for nil, value otherwise.
func DerefBytes(b *[]byte) []byte {
	if b == nil {
		return nil
	}
	return *b
}

// --- JSON helpers ---
// Handle nullable JSON columns with safe defaults.

// JsonOrEmpty returns "{}" for nil, data otherwise.
func JsonOrEmpty(data []byte) []byte {
	if data == nil {
		return []byte("{}")
	}
	return data
}

// JsonOrEmptyArray returns "[]" for nil, data otherwise.
func JsonOrEmptyArray(data []byte) []byte {
	if data == nil {
		return []byte("[]")
	}
	return data
}

// JsonOrNull returns nil for nil RawMessage, []byte otherwise.
func JsonOrNull(data json.RawMessage) any {
	if data == nil {
		return nil
	}
	return []byte(data)
}
