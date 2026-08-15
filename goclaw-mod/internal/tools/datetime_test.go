package tools

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDateTimeToolReturnsDerivedLocalCalendarFields(t *testing.T) {
	t.Parallel()

	tool := NewDateTimeTool()
	tool.now = func() time.Time {
		return time.Date(2026, time.July, 23, 1, 42, 19, 0, time.UTC)
	}
	result := tool.Execute(t.Context(), map[string]any{
		"timezone": "Asia/Ho_Chi_Minh",
	})
	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	wants := map[string]any{
		"utc":             "2026-07-23T01:42:19Z",
		"utc_date":        "2026-07-23",
		"utc_time":        "01:42:19",
		"utc_weekday":     "Thursday",
		"utc_iso_weekday": float64(4),
		"unix_ms":         float64(1784770939000),
		"local":           "2026-07-23T08:42:19+07:00",
		"local_date":      "2026-07-23",
		"local_time":      "08:42:19",
		"weekday":         "Thursday",
		"iso_weekday":     float64(4),
		"utc_offset":      "+07:00",
		"timezone":        "Asia/Ho_Chi_Minh",
	}
	for key, want := range wants {
		if got := payload[key]; got != want {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestDateTimeToolReturnsUTCOnlyWithoutTimezone(t *testing.T) {
	t.Parallel()

	tool := NewDateTimeTool()
	tool.now = func() time.Time {
		return time.Date(2026, time.July, 26, 23, 59, 58, 0, time.UTC)
	}
	result := tool.Execute(t.Context(), nil)
	if result.IsError {
		t.Fatalf("Execute() returned error: %s", result.ForLLM)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got := payload["utc_weekday"]; got != "Sunday" {
		t.Errorf("utc_weekday = %#v, want %q", got, "Sunday")
	}
	if got := payload["utc_iso_weekday"]; got != float64(7) {
		t.Errorf("utc_iso_weekday = %#v, want 7", got)
	}
	for _, key := range []string{"local", "local_date", "local_time", "weekday", "iso_weekday", "utc_offset", "timezone"} {
		if _, ok := payload[key]; ok {
			t.Errorf("UTC-only result unexpectedly contains %q", key)
		}
	}
}

func TestDateTimeToolRejectsInvalidTimezone(t *testing.T) {
	t.Parallel()

	result := NewDateTimeTool().Execute(t.Context(), map[string]any{
		"timezone": "UTC+7",
	})
	if !result.IsError {
		t.Fatal("Execute() accepted an invalid IANA timezone")
	}
}

func TestDateTimeToolExecuteSupportsZeroValueAndNilReceiver(t *testing.T) {
	t.Parallel()

	var nilTool *DateTimeTool
	for name, tool := range map[string]*DateTimeTool{
		"zero value":   {},
		"nil receiver": nilTool,
	} {
		t.Run(name, func(t *testing.T) {
			result := tool.Execute(t.Context(), nil)
			if result.IsError {
				t.Fatalf("Execute() returned error: %s", result.ForLLM)
			}

			var payload map[string]any
			if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			for _, key := range []string{"utc", "utc_date", "utc_time", "utc_weekday", "utc_iso_weekday", "unix_ms"} {
				if payload[key] == nil {
					t.Errorf("result is missing %q", key)
				}
			}
		})
	}
}
