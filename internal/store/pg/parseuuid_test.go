package pg

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseUUID_Valid(t *testing.T) {
	want := uuid.New()
	got, err := parseUUID(want.String())
	if err != nil {
		t.Fatalf("parseUUID(valid): unexpected error %v", err)
	}
	if got != want {
		t.Errorf("parseUUID: got %v, want %v", got, want)
	}
}

func TestParseUUID_Invalid_ReturnsError(t *testing.T) {
	got, err := parseUUID("goctech-leader")
	if err == nil {
		t.Fatal("parseUUID(agent_key): expected error, got nil")
	}
	if got != uuid.Nil {
		t.Errorf("parseUUID(invalid): got %v, want uuid.Nil", got)
	}
	// Error must include both the offending value (for debuggability) and wrap
	// the underlying uuid package error (for errors.Is assertions).
	if !strings.Contains(err.Error(), "goctech-leader") {
		t.Errorf("parseUUID error must include raw value, got: %v", err)
	}
}

func TestParseUUID_Empty_ReturnsError(t *testing.T) {
	_, err := parseUUID("")
	if err == nil {
		t.Error("parseUUID(\"\"): expected error for empty input")
	}
}

func TestParseUUIDOrNil_Valid(t *testing.T) {
	want := uuid.New()
	got := parseUUIDOrNil(want.String())
	if got != want {
		t.Errorf("parseUUIDOrNil: got %v, want %v", got, want)
	}
}

func TestParseUUIDOrNil_Invalid_ReturnsNil(t *testing.T) {
	got := parseUUIDOrNil("not-a-uuid")
	if got != uuid.Nil {
		t.Errorf("parseUUIDOrNil(invalid): got %v, want uuid.Nil", got)
	}
}

// TestParseUUID_ErrorIsUnwrappable verifies the wrapped error chain so
// downstream callers can errors.Is/errors.As against the root uuid error.
func TestParseUUID_ErrorIsUnwrappable(t *testing.T) {
	_, err := parseUUID("bad")
	if err == nil {
		t.Fatal("expected error")
	}
	// Unwrap should not be nil — the wrapped error chain is what lets callers
	// decide whether to return a 400 vs 500.
	if errors.Unwrap(err) == nil {
		t.Error("parseUUID error should wrap the underlying uuid.Parse error")
	}
}

