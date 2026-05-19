package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestParseFormFillSSEEventSanitizesURLTokensBeforeScrub(t *testing.T) {
	raw := []byte(`{
		"event": "run_completed",
		"run_id": "run-1",
		"timestamp": "2026-05-14T10:49:35Z",
		"data": {
			"current_url": "https://lexmount.feishu.cn/share/base/form/demo?auth_token=U7CK1RF-b98g1faa-1f42-4433-80f4-4c612621b7d5&foo=bar",
			"visited_urls": [
				"https://lexmount.feishu.cn/share/base/form/demo?foo=bar&auth_token=U7CK1RF-b98g1faa-1f42-4433-80f4-4c612621b7d5"
			],
			"history_excerpt": [
				"Opened https://lexmount.feishu.cn/share/base/form/demo?auth_token=U7CK1RF-b98g1faa-1f42-4433-80f4-4c612621b7d5 and submitted"
			],
			"final_text": "Submitted",
			"submission_result": "Submitted"
		}
	}`)

	parsed, err := parseFormFillSSEEvent(sseEvent{Event: "message", Data: raw})
	if err != nil {
		t.Fatalf("parseFormFillSSEEvent() error = %v", err)
	}

	got := string(parsed.Raw)
	if strings.Contains(got, "auth_token=") || strings.Contains(got, "U7CK1RF") {
		t.Fatalf("sensitive query parameter value was not removed: %s", got)
	}
	if !strings.Contains(got, "redacted_param=auth_token") {
		t.Fatalf("redaction marker missing: %s", got)
	}

	scrubbed := tools.ScrubCredentials(got)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(scrubbed), &decoded); err != nil {
		t.Fatalf("scrubbed result must remain valid JSON: %v\n%s", err, scrubbed)
	}

	data, _ := decoded["data"].(map[string]any)
	if data == nil {
		t.Fatalf("decoded data missing: %#v", decoded)
	}
	if data["current_url"] != "https://lexmount.feishu.cn/share/base/form/demo?redacted_param=auth_token&foo=bar" {
		t.Fatalf("current_url = %#v", data["current_url"])
	}
}

func TestAppendFormFillRawEventKeepsOriginalEventOutOfToolResult(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORM_AGENT_RAW_EVENT_LOG_DIR", dir)

	raw := []byte(`{"event":"run_completed","run_id":"run-raw","data":{"visited_urls":["https://example.com/form?auth_token=secret-token-value"]}}`)
	parsed, err := parseFormFillSSEEvent(sseEvent{Event: "message", Data: raw})
	if err != nil {
		t.Fatalf("parseFormFillSSEEvent() error = %v", err)
	}

	ref := appendFormFillRawEvent(parsed, 1, raw)
	withRef := attachFormFillRawEventRef(parsed, ref)

	if !strings.Contains(string(withRef.Raw), `"raw_event_ref"`) {
		t.Fatalf("raw_event_ref missing: %s", withRef.Raw)
	}
	if strings.Contains(string(withRef.Raw), "secret-token-value") {
		t.Fatalf("safe tool result leaked raw token: %s", withRef.Raw)
	}

	path, _ := ref["log_path"].(string)
	if path == "" || filepath.Dir(path) != dir {
		t.Fatalf("unexpected log path ref: %#v", ref)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw event log: %v", err)
	}
	if !strings.Contains(string(b), "secret-token-value") {
		t.Fatalf("raw event log did not preserve original token: %s", b)
	}
}

func TestRunCompletedWithSuccessFalseBecomesErrorPayload(t *testing.T) {
	raw := []byte(`{
		"event": "run_completed",
		"run_id": "run-failed-business",
		"data": {
			"success": false,
			"submission_result": null,
			"final_text": null,
			"errors": ["content policy blocked"],
			"payload": null
		}
	}`)

	parsed, err := parseFormFillSSEEvent(sseEvent{Event: "message", Data: raw})
	if err != nil {
		t.Fatalf("parseFormFillSSEEvent() error = %v", err)
	}

	data := parsed.Data
	if data["status"] != "failed" {
		t.Fatalf("status = %#v, want failed", data["status"])
	}
	payload, _ := data["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("payload missing: %#v", data)
	}
	if payload["kind"] != "error" {
		t.Fatalf("payload.kind = %#v, want error", payload["kind"])
	}
	if payload["title"] != "表单提交失败" {
		t.Fatalf("payload.title = %#v", payload["title"])
	}
	if !strings.Contains(asString(payload["text"]), "content policy blocked") {
		t.Fatalf("payload.text = %#v", payload["text"])
	}
}
