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
		false,               // stateless
		false,               // deliver
		"",                  // deliver_channel
		"",                  // deliver_to
		false,               // wake_heartbeat
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
	}
	payloadJSON, _ := json.Marshal(payload)

	// Build row with deliver/deliver_channel/deliver_to set as dedicated columns.
	rowVals := validCronRow(payloadJSON)
	// Indices: stateless=13, deliver=14, deliver_channel=15, deliver_to=16, wake_heartbeat=17
	rowVals[14] = true      // deliver
	rowVals[15] = "telegram" // deliver_channel
	rowVals[16] = "user123" // deliver_to
	row := &mockRowScanner{values: rowVals}

	job, err := scanCronRow(row)
	if err != nil {
		t.Fatalf("scanCronRow returned unexpected error: %v", err)
	}

	if job.Payload.Message != "hello world" {
		t.Errorf("expected Message 'hello world', got %q", job.Payload.Message)
	}
	if job.DeliverChannel != "telegram" {
		t.Errorf("expected DeliverChannel 'telegram', got %q", job.DeliverChannel)
	}
	if job.DeliverTo != "user123" {
		t.Errorf("expected DeliverTo 'user123', got %q", job.DeliverTo)
	}
	if !job.Deliver {
		t.Errorf("expected Deliver true")
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
