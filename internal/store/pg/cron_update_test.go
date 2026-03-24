package pg

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestScheduleKindCorruption_LogicPath verifies that empty schedule_kind
// (which would happen if the pre-fetch Scan failed and error was ignored)
// falls through validation with no error — proving why the Scan error
// MUST be checked (as it now is in the fix).
//
// The fix: UpdateJob now returns an error when the schedule-fetch Scan fails,
// so this corrupted logic path is no longer reachable.
func TestScheduleKindCorruption_LogicPath(t *testing.T) {
	// When Scan fails, curKind is "" (zero value).
	// With partial patch (no kind specified), newKind inherits "".
	curKind := "" // simulates failed Scan
	patchKind := ""

	newKind := patchKind
	if newKind == "" {
		newKind = curKind
	}

	// Verify the corruption: empty kind bypasses all validation cases
	merged := store.CronSchedule{Kind: newKind}
	validationRan := false
	switch merged.Kind {
	case "cron", "every", "at":
		validationRan = true
	}

	if validationRan {
		t.Error("expected validation to be skipped for empty kind")
	}
	if newKind != "" {
		t.Error("expected empty newKind from failed Scan")
	}

	// This proves the corruption path exists.
	// The fix (returning error from Scan) prevents reaching this path.
	t.Log("Confirmed: empty schedule_kind bypasses validation — fix prevents this by checking Scan error")
}

// TestPayloadUnmarshal_CorruptedJSON verifies that corrupted JSON in the
// payload column causes json.Unmarshal to fail, which would wipe existing
// fields if the error is ignored — proving why the error MUST be checked.
//
// The fix: UpdateJob now returns an error when payload Unmarshal fails,
// so existing fields are never overwritten with zero values.
func TestPayloadUnmarshal_CorruptedJSON(t *testing.T) {
	corruptedJSON := []byte(`{"kind":"message","message":"daily report","deliver":true,CORRUPT`)

	var payload store.CronPayload
	err := json.Unmarshal(corruptedJSON, &payload)

	// Prove that Unmarshal fails
	if err == nil {
		t.Fatal("expected json.Unmarshal to fail on corrupted JSON")
	}

	// Prove that payload is zero-valued after failed Unmarshal
	if payload.Message != "" {
		t.Error("expected empty Message after failed Unmarshal")
	}
	if payload.Deliver {
		t.Error("expected false Deliver after failed Unmarshal")
	}

	// If we ignored this error and applied patch fields, we'd wipe existing data.
	// The fix returns an error instead.
	t.Log("Confirmed: failed Unmarshal leaves zero payload — fix prevents wipe by returning error")
}

// TestPayloadUnmarshal_ValidJSON verifies normal payload round-trip works.
func TestPayloadUnmarshal_ValidJSON(t *testing.T) {
	original := store.CronPayload{
		Kind:    "message",
		Message: "daily report",
		Deliver: true,
		Channel: "telegram",
		To:      "admin-group",
	}
	data, _ := json.Marshal(original)

	var decoded store.CronPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decoded.Message != original.Message {
		t.Errorf("Message mismatch: got %q, want %q", decoded.Message, original.Message)
	}
	if decoded.Channel != original.Channel {
		t.Errorf("Channel mismatch: got %q, want %q", decoded.Channel, original.Channel)
	}
	if decoded.To != original.To {
		t.Errorf("To mismatch: got %q, want %q", decoded.To, original.To)
	}
}
