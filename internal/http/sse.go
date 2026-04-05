package http

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ProgressEvent describes a single SSE progress update for long-running operations.
// Used by export, import, and any future streaming endpoints.
type ProgressEvent struct {
	Phase   string `json:"phase"`
	Status  string `json:"status"` // "running", "done", "error"
	Detail  string `json:"detail,omitempty"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
}

// sendSSE writes a named SSE event with JSON payload and flushes.
// The event format follows the standard SSE spec: "event: <name>\ndata: <json>\n\n".
func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	flusher.Flush()
}

// initSSE sets standard headers for an SSE response and writes the 200 status.
// Returns the Flusher or nil if streaming is not supported.
func initSSE(w http.ResponseWriter) http.Flusher {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	return flusher
}
