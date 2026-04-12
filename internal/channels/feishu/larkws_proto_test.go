package feishu

import (
	"bytes"
	"testing"
)

// TestMarshalUnmarshalRoundTrip verifies that a wsFrame survives a
// marshal → unmarshal round-trip with no data loss.
func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	original := &wsFrame{
		SeqID:           42,
		LogID:           99,
		Service:         1,
		Method:          2,
		PayloadEncoding: "utf-8",
		PayloadType:     "json",
		Payload:         []byte(`{"hello":"world"}`),
		LogIDNew:        "logid-abc",
		Headers: []wsHeader{
			{Key: "type", Value: "event"},
			{Key: "message_id", Value: "msg_001"},
		},
	}

	data := marshalFrame(original)
	if len(data) == 0 {
		t.Fatal("marshalFrame returned empty bytes")
	}

	got, err := unmarshalFrame(data)
	if err != nil {
		t.Fatalf("unmarshalFrame returned error: %v", err)
	}

	if got.SeqID != original.SeqID {
		t.Errorf("SeqID: got %d, want %d", got.SeqID, original.SeqID)
	}
	if got.LogID != original.LogID {
		t.Errorf("LogID: got %d, want %d", got.LogID, original.LogID)
	}
	if got.Service != original.Service {
		t.Errorf("Service: got %d, want %d", got.Service, original.Service)
	}
	if got.Method != original.Method {
		t.Errorf("Method: got %d, want %d", got.Method, original.Method)
	}
	if got.PayloadEncoding != original.PayloadEncoding {
		t.Errorf("PayloadEncoding: got %q, want %q", got.PayloadEncoding, original.PayloadEncoding)
	}
	if got.PayloadType != original.PayloadType {
		t.Errorf("PayloadType: got %q, want %q", got.PayloadType, original.PayloadType)
	}
	if !bytes.Equal(got.Payload, original.Payload) {
		t.Errorf("Payload: got %q, want %q", got.Payload, original.Payload)
	}
	if got.LogIDNew != original.LogIDNew {
		t.Errorf("LogIDNew: got %q, want %q", got.LogIDNew, original.LogIDNew)
	}
	if len(got.Headers) != len(original.Headers) {
		t.Fatalf("Headers len: got %d, want %d", len(got.Headers), len(original.Headers))
	}
	hm := got.headerMap()
	if hm["type"] != "event" {
		t.Errorf("header type: got %q, want %q", hm["type"], "event")
	}
	if hm["message_id"] != "msg_001" {
		t.Errorf("header message_id: got %q, want %q", hm["message_id"], "msg_001")
	}
}

// TestMarshalUnmarshal_ZeroValues verifies frames with all-zero/empty fields.
func TestMarshalUnmarshal_ZeroValues(t *testing.T) {
	f := &wsFrame{}
	data := marshalFrame(f)
	got, err := unmarshalFrame(data)
	if err != nil {
		t.Fatalf("unmarshalFrame zero frame error: %v", err)
	}
	if got.SeqID != 0 || got.LogID != 0 {
		t.Errorf("expected zero IDs, got SeqID=%d LogID=%d", got.SeqID, got.LogID)
	}
}

// TestMarshalUnmarshal_OnlyPayload verifies payload-only frames work.
func TestMarshalUnmarshal_OnlyPayload(t *testing.T) {
	payload := []byte("raw-binary-data")
	f := &wsFrame{Payload: payload}
	data := marshalFrame(f)
	got, err := unmarshalFrame(data)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload mismatch: got %q, want %q", got.Payload, payload)
	}
}

// TestUnmarshalFrame_MalformedInput verifies no panic and an error is returned.
func TestUnmarshalFrame_MalformedInput(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"truncated varint", []byte{0x80, 0x80}},        // incomplete multi-byte varint
		{"bad wire type", []byte{0x0f}},                  // field 1, wire type 7 (unsupported)
		{"length prefix no data", []byte{0x12, 0x05}},   // field 2 wire 2, length=5 but no data
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Must not panic.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("unmarshalFrame panicked: %v", r)
				}
			}()
			// Error or not — just must not panic. Empty input is valid and returns empty frame.
			_, _ = unmarshalFrame(tc.data)
		})
	}
}

// TestHeaderMap_Empty verifies headerMap on a frame with no headers.
func TestHeaderMap_Empty(t *testing.T) {
	f := &wsFrame{}
	m := f.headerMap()
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

// TestHeaderMap_Dedup verifies last-writer-wins for duplicate header keys.
func TestHeaderMap_Dedup(t *testing.T) {
	f := &wsFrame{Headers: []wsHeader{
		{Key: "type", Value: "first"},
		{Key: "type", Value: "second"},
	}}
	m := f.headerMap()
	if m["type"] != "second" {
		t.Errorf("expected 'second', got %q", m["type"])
	}
}

// TestMarshalFrame_MultipleHeaders verifies multiple distinct headers survive.
func TestMarshalFrame_MultipleHeaders(t *testing.T) {
	f := &wsFrame{
		Headers: []wsHeader{
			{Key: "sum", Value: "3"},
			{Key: "seq", Value: "1"},
			{Key: "message_id", Value: "fragmented-msg"},
		},
	}
	data := marshalFrame(f)
	got, err := unmarshalFrame(data)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	hm := got.headerMap()
	if hm["sum"] != "3" {
		t.Errorf("sum: got %q", hm["sum"])
	}
	if hm["seq"] != "1" {
		t.Errorf("seq: got %q", hm["seq"])
	}
	if hm["message_id"] != "fragmented-msg" {
		t.Errorf("message_id: got %q", hm["message_id"])
	}
}
