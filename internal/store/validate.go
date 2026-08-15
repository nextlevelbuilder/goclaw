package store

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxUserIDLength is the maximum allowed length for user identifier strings
// (user_id, owner_id, granted_by, requested_by, reviewed_by, etc.).
// Matches the VARCHAR(255) constraint in the database schema.
const MaxUserIDLength = 255

// ValidateUserID validates user identifiers for length and dangerous characters.
// Defense-in-depth: SQL injection is handled by parameterized queries, but we
// also reject clearly malicious patterns at the validation layer.
func ValidateUserID(id string) error {
	if len(id) > MaxUserIDLength {
		return fmt.Errorf("user identifier too long: %d chars (max %d)", len(id), MaxUserIDLength)
	}

	// Reject null bytes
	if strings.ContainsRune(id, '\x00') {
		return fmt.Errorf("user identifier contains null byte")
	}

	// Reject control characters (below space, including tab/newline/carriage return)
	for _, r := range id {
		if r < 32 {
			return fmt.Errorf("user identifier contains control character: %U", r)
		}
		// Reject dangerous unicode categories
		if unicode.Is(unicode.Cf, r) { // Format characters (ZWJ, RTL override, etc.)
			return fmt.Errorf("user identifier contains format character: %U", r)
		}
	}

	// Reject BOM
	if strings.HasPrefix(id, "\uFEFF") {
		return fmt.Errorf("user identifier starts with BOM")
	}

	return nil
}
