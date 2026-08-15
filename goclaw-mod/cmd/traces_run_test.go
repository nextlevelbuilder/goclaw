package cmd

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTracesGetJSONOutput(t *testing.T) {
	traceID := "11111111-1111-1111-1111-111111111111"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces/"+traceID {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"trace": {
				"id": "` + traceID + `",
				"run_id": "run-123",
				"start_time": "2026-06-12T01:00:00Z",
				"created_at": "2026-06-12T01:00:00Z",
				"status": "completed",
				"total_input_tokens": 10,
				"total_output_tokens": 20
			},
			"spans": []
		}`))
	}))
	defer srv.Close()
	withTraceTestGateway(t, srv)
	gatewayOutputFormat = "json"

	out, err := captureStdout(t, func() error {
		return runTracesGet(traceID)
	})
	if err != nil {
		t.Fatalf("runTracesGet: %v", err)
	}
	if !strings.Contains(out, `"run_id": "run-123"`) {
		t.Fatalf("output missing run_id: %s", out)
	}
}

func TestRunTracesExportWritesFileAndPrintsJSON(t *testing.T) {
	var gzipPayload bytes.Buffer
	gz := gzip.NewWriter(&gzipPayload)
	_, _ = gz.Write([]byte(`{"trace":{"id":"trace-1"},"spans":[],"sub_traces":[]}`))
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces/trace-1/export" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gzipPayload.Bytes())
	}))
	defer srv.Close()
	withTraceTestGateway(t, srv)

	gatewayOutputFormat = "table"
	outFile := filepath.Join(t.TempDir(), "trace.json.gz")
	if _, err := captureStdout(t, func() error {
		return runTracesExport("trace-1", outFile)
	}); err != nil {
		t.Fatalf("runTracesExport file: %v", err)
	}
	written, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !bytes.Equal(written, gzipPayload.Bytes()) {
		t.Fatal("written gzip payload mismatch")
	}

	gatewayOutputFormat = "json"
	out, err := captureStdout(t, func() error {
		return runTracesExport("trace-1", "")
	})
	if err != nil {
		t.Fatalf("runTracesExport json: %v", err)
	}
	if !strings.Contains(out, `"trace"`) || !strings.Contains(out, `"sub_traces"`) {
		t.Fatalf("json output missing trace tree fields: %s", out)
	}
}

func TestRunTracesFollowJSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces/follow" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("session_key"); got != "session-1" {
			t.Fatalf("session_key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"traces": [],
			"spans_by_trace_id": {},
			"server_time": "2026-06-12T01:00:00Z",
			"next_since": "2026-06-12T01:00:00Z",
			"limit": 50
		}`))
	}))
	defer srv.Close()
	withTraceTestGateway(t, srv)
	gatewayOutputFormat = "json"

	out, err := captureStdout(t, func() error {
		return runTracesFollow(traceFollowOptions{SessionKey: "session-1"})
	})
	if err != nil {
		t.Fatalf("runTracesFollow: %v", err)
	}
	if !strings.Contains(out, `"next_since": "2026-06-12T01:00:00Z"`) {
		t.Fatalf("output missing next_since: %s", out)
	}
}

func withTraceTestGateway(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldServer := gatewayServerOverride
	oldToken := gatewayTokenOverride
	oldOutput := gatewayOutputFormat
	oldClient := httpClient
	t.Cleanup(func() {
		gatewayServerOverride = oldServer
		gatewayTokenOverride = oldToken
		gatewayOutputFormat = oldOutput
		httpClient = oldClient
	})
	gatewayServerOverride = srv.URL
	gatewayTokenOverride = "test-token"
	httpClient = srv.Client()
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(out), runErr
}
