package pg

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockRowScanner satisfies cronRowScanner for unit-testing scanCronRow
// without hitting a real database.
type mockRowScanner struct {
	values []any
	err    error
}

func (m *mockRowScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	// Copy pre-set values into dest pointers.
	for i, d := range dest {
		if i >= len(m.values) {
			break
		}
		switch dp := d.(type) {
		case *uuid.UUID:
			if v, ok := m.values[i].(uuid.UUID); ok {
				*dp = v
			}
		case **uuid.UUID:
			if v, ok := m.values[i].(*uuid.UUID); ok {
				*dp = v
			}
		case *string:
			if v, ok := m.values[i].(string); ok {
				*dp = v
			}
		case **string:
			if v, ok := m.values[i].(*string); ok {
				*dp = v
			}
		case *bool:
			if v, ok := m.values[i].(bool); ok {
				*dp = v
			}
		case **time.Time:
			if v, ok := m.values[i].(*time.Time); ok {
				*dp = v
			}
		case **int64:
			if v, ok := m.values[i].(*int64); ok {
				*dp = v
			}
		case *[]byte:
			if v, ok := m.values[i].([]byte); ok {
				*dp = v
			}
		case *time.Time:
			if v, ok := m.values[i].(time.Time); ok {
				*dp = v
			}
		}
	}
	return nil
}

// validCronRow returns mock scanner values for a valid cron job row.
func validCronRow(payloadJSON []byte) []any {
	id := uuid.New()
	tenantID := uuid.New()
	now := time.Now()
	expr := "*/5 * * * *"
	tz := "UTC"
	return []any{
		id,                  // id
		tenantID,            // tenant_id
		(*uuid.UUID)(nil),   // agent_id
		(*string)(nil),      // user_id
		"test-job",          // name
		true,                // enabled
		"cron",              // schedule_kind
		&expr,               // cron_expression
		(*time.Time)(nil),   // run_at
		&tz,                 // timezone
		(*int64)(nil),       // interval_ms
		payloadJSON,         // payload
		false,               // delete_after_run
		(*time.Time)(nil),   // next_run_at
		(*time.Time)(nil),   // last_run_at
		(*string)(nil),      // last_status
		(*string)(nil),      // last_error
		now,                 // created_at
		now,                 // updated_at
	}
}

// TestScanCronRow_MalformedPayloadJSON verifies that malformed payload JSON
// returns an error instead of silently returning a zero-valued payload.
func TestScanCronRow_MalformedPayloadJSON(t *testing.T) {
	malformedJSON := []byte(`{invalid json!!!}`)
	row := &mockRowScanner{values: validCronRow(malformedJSON)}

	_, err := scanCronRow(row)
	if err == nil {
		t.Fatal("expected error for malformed payload JSON, got nil")
	}
	if !strings.Contains(err.Error(), "payload") {
		t.Errorf("error should mention payload, got: %v", err)
	}
}

// TestScanCronRow_ValidPayload verifies correct behavior with valid JSON.
func TestScanCronRow_ValidPayload(t *testing.T) {
	payload := store.CronPayload{
		Kind:    "message",
		Message: "hello world",
		Deliver: true,
		Channel: "telegram",
		To:      "user123",
	}
	payloadJSON, _ := json.Marshal(payload)
	row := &mockRowScanner{values: validCronRow(payloadJSON)}

	job, err := scanCronRow(row)
	if err != nil {
		t.Fatalf("scanCronRow returned unexpected error: %v", err)
	}

	if job.Payload.Message != "hello world" {
		t.Errorf("expected Message 'hello world', got %q", job.Payload.Message)
	}
	if job.Payload.Channel != "telegram" {
		t.Errorf("expected Channel 'telegram', got %q", job.Payload.Channel)
	}
	if job.Payload.To != "user123" {
		t.Errorf("expected To 'user123', got %q", job.Payload.To)
	}
}

// TestScanCronRow_NullPayload verifies that NULL payload (empty []byte) is handled.
func TestScanCronRow_NullPayload(t *testing.T) {
	row := &mockRowScanner{values: validCronRow(nil)}

	job, err := scanCronRow(row)
	if err != nil {
		t.Fatalf("scanCronRow returned unexpected error: %v", err)
	}

	// NULL payload should result in zero-valued payload (not an error).
	if job.Payload.Message != "" {
		t.Errorf("expected empty Message for NULL payload, got %q", job.Payload.Message)
	}
}

// TestScanCronRow_ScanError verifies that a DB scan error is propagated.
func TestScanCronRow_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("connection reset")}

	_, err := scanCronRow(row)
	if err == nil {
		t.Fatal("expected error from scanCronRow when scan fails")
	}
}
