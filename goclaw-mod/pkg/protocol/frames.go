// Package protocol defines the wire format for the GoClaw Gateway WebSocket protocol.
// This package is importable by Service 2 and other clients.
package protocol

import "encoding/json"

// Protocol version. Clients must negotiate this during connect handshake.
const ProtocolVersion = 3

// Frame types
const (
	FrameTypeRequest  = "req"
	FrameTypeResponse = "res"
	FrameTypeEvent    = "event"
)

// RawFrame is used for initial parsing to determine frame type.
type RawFrame struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"` // original bytes for re-parsing
}

// RequestFrame is sent by clients to invoke an RPC method.
type RequestFrame struct {
	Type   string          `json:"type"`   // always "req"
	ID     string          `json:"id"`     // unique request ID (client-generated)
	Method string          `json:"method"` // RPC method name
	Params json.RawMessage `json:"params,omitempty"`
}

// ResponseFrame is sent by the server in response to a request.
type ResponseFrame struct {
	Type    string      `json:"type"`              // always "res"
	ID      string      `json:"id"`                // matches request ID
	OK      bool        `json:"ok"`                // true if success
	Payload any         `json:"payload,omitempty"` // response data (when ok=true)
	Error   *ErrorShape `json:"error,omitempty"`   // error info (when ok=false)
}

// ErrorShape describes a protocol error.
type ErrorShape struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Details      any    `json:"details,omitempty"`
	Retryable    bool   `json:"retryable,omitempty"`
	RetryAfterMs int    `json:"retryAfterMs,omitempty"`
}

// EventFrame is pushed from server to client without a preceding request.
type EventFrame struct {
	Type         string        `json:"type"`                   // always "event"
	Event        string        `json:"event"`                  // event name
	Payload      any           `json:"payload,omitempty"`      // event data
	Seq          int64         `json:"seq,omitempty"`          // ordering sequence number
	StateVersion *StateVersion `json:"stateVersion,omitempty"` // version counters for state sync
}

// StateVersion tracks version counters for optimistic state sync.
type StateVersion struct {
	Presence int64 `json:"presence"`
	Health   int64 `json:"health"`
}

// NewOKResponse creates a success response frame.
func NewOKResponse(id string, payload any) *ResponseFrame {
	return &ResponseFrame{
		Type:    FrameTypeResponse,
		ID:      id,
		OK:      true,
		Payload: payload,
	}
}

// NewErrorResponse creates an error response frame.
func NewErrorResponse(id string, code, message string) *ResponseFrame {
	return &ResponseFrame{
		Type: FrameTypeResponse,
		ID:   id,
		OK:   false,
		Error: &ErrorShape{
			Code:    code,
			Message: message,
		},
	}
}

// NewEvent creates an event frame.
func NewEvent(event string, payload any) *EventFrame {
	return &EventFrame{
		Type:    FrameTypeEvent,
		Event:   event,
		Payload: payload,
	}
}

// ParseFrameType extracts the frame type from raw JSON bytes.
// Returns the type string and remaining bytes for re-parsing.
func ParseFrameType(data []byte) (string, error) {
	var raw struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	return raw.Type, nil
}
