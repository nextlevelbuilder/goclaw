package acp

import (
	"encoding/json"
	"testing"
)

// roundTrip marshals v to JSON then unmarshals into a new value of same type.
// Returns the JSON bytes and the round-tripped value.
func roundTrip[T any](t *testing.T, v T) ([]byte, T) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	return data, out
}

func TestInitializeRequest_RoundTrip(t *testing.T) {
	req := InitializeRequest{
		ClientInfo: ClientInfo{Name: "goclaw", Version: "1.0"},
		Capabilities: ClientCaps{
			Fs:       &FsCaps{ReadTextFile: true, WriteTextFile: false},
			Terminal: &TerminalCaps{Enabled: true},
		},
	}
	_, got := roundTrip(t, req)
	if got.ClientInfo.Name != "goclaw" {
		t.Errorf("ClientInfo.Name: got %q", got.ClientInfo.Name)
	}
	if got.ClientInfo.Version != "1.0" {
		t.Errorf("ClientInfo.Version: got %q", got.ClientInfo.Version)
	}
	if got.Capabilities.Fs == nil || !got.Capabilities.Fs.ReadTextFile {
		t.Error("Capabilities.Fs.ReadTextFile should be true")
	}
	if got.Capabilities.Fs.WriteTextFile {
		t.Error("Capabilities.Fs.WriteTextFile should be false")
	}
	if got.Capabilities.Terminal == nil || !got.Capabilities.Terminal.Enabled {
		t.Error("Capabilities.Terminal.Enabled should be true")
	}
}

func TestInitializeResponse_RoundTrip(t *testing.T) {
	resp := InitializeResponse{
		AgentInfo: AgentInfo{Name: "claude-code", Version: "2.0"},
		Capabilities: AgentCaps{
			LoadSession: true,
			PromptCapabilities: &PromptCaps{
				Audio:           false,
				Image:           true,
				EmbeddedContext: true,
			},
			SessionCapabilities: &SessionCaps{},
		},
	}
	_, got := roundTrip(t, resp)
	if got.AgentInfo.Name != "claude-code" {
		t.Errorf("AgentInfo.Name: got %q", got.AgentInfo.Name)
	}
	if !got.Capabilities.LoadSession {
		t.Error("LoadSession should be true")
	}
	if got.Capabilities.PromptCapabilities == nil {
		t.Fatal("PromptCapabilities should not be nil")
	}
	if !got.Capabilities.PromptCapabilities.Image {
		t.Error("PromptCapabilities.Image should be true")
	}
}

func TestNewSessionResponse_RoundTrip(t *testing.T) {
	resp := NewSessionResponse{SessionID: "sess-abc-123"}
	_, got := roundTrip(t, resp)
	if got.SessionID != "sess-abc-123" {
		t.Errorf("SessionID: got %q", got.SessionID)
	}
}

func TestPromptRequest_RoundTrip(t *testing.T) {
	req := PromptRequest{
		SessionID: "sess-1",
		Content: []ContentBlock{
			{Type: "text", Text: "hello"},
			{Type: "image", Data: "base64data", MimeType: "image/png"},
		},
	}
	_, got := roundTrip(t, req)
	if got.SessionID != "sess-1" {
		t.Errorf("SessionID: got %q", got.SessionID)
	}
	if len(got.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(got.Content))
	}
	if got.Content[0].Type != "text" || got.Content[0].Text != "hello" {
		t.Errorf("content[0]: got type=%q text=%q", got.Content[0].Type, got.Content[0].Text)
	}
	if got.Content[1].Type != "image" || got.Content[1].Data != "base64data" {
		t.Errorf("content[1]: got type=%q data=%q", got.Content[1].Type, got.Content[1].Data)
	}
}

func TestPromptResponse_RoundTrip(t *testing.T) {
	resp := PromptResponse{StopReason: "endTurn"}
	_, got := roundTrip(t, resp)
	if got.StopReason != "endTurn" {
		t.Errorf("StopReason: got %q", got.StopReason)
	}
}

func TestCancelNotification_RoundTrip(t *testing.T) {
	n := CancelNotification{SessionID: "sess-xyz"}
	_, got := roundTrip(t, n)
	if got.SessionID != "sess-xyz" {
		t.Errorf("SessionID: got %q", got.SessionID)
	}
}

func TestSessionUpdate_RoundTrip(t *testing.T) {
	exitCode := 0
	su := SessionUpdate{
		Kind:       "message",
		StopReason: "endTurn",
		Message: &MessageUpdate{
			Role: "assistant",
			Content: []ContentBlock{
				{Type: "text", Text: "done"},
			},
		},
		ToolCall: &ToolCallUpdate{
			ID:     "tc-1",
			Name:   "run_code",
			Status: "completed",
			Content: []ContentBlock{
				{Type: "text", Text: "exit 0"},
			},
		},
	}
	_ = exitCode
	_, got := roundTrip(t, su)
	if got.Kind != "message" {
		t.Errorf("Kind: got %q", got.Kind)
	}
	if got.StopReason != "endTurn" {
		t.Errorf("StopReason: got %q", got.StopReason)
	}
	if got.Message == nil || got.Message.Role != "assistant" {
		t.Error("Message.Role should be assistant")
	}
	if got.ToolCall == nil || got.ToolCall.ID != "tc-1" {
		t.Error("ToolCall.ID should be tc-1")
	}
}

func TestContentBlock_OmitEmpty(t *testing.T) {
	// Text block: data and mimeType should be omitted
	cb := ContentBlock{Type: "text", Text: "hello"}
	data, err := json.Marshal(cb)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	if _, hasData := m["data"]; hasData {
		t.Error("data field should be omitted for text block")
	}
	if _, hasMime := m["mimeType"]; hasMime {
		t.Error("mimeType field should be omitted for text block")
	}
}

func TestTerminalTypes_RoundTrip(t *testing.T) {
	create := CreateTerminalRequest{Command: "bash", Args: []string{"-c", "echo hi"}, Cwd: "/tmp"}
	_, gotCreate := roundTrip(t, create)
	if gotCreate.Command != "bash" || len(gotCreate.Args) != 2 || gotCreate.Cwd != "/tmp" {
		t.Errorf("CreateTerminalRequest round-trip failed: %+v", gotCreate)
	}

	exitCode := 42
	output := TerminalOutputResponse{Output: "hello", ExitStatus: &exitCode}
	_, gotOutput := roundTrip(t, output)
	if gotOutput.Output != "hello" {
		t.Errorf("Output: got %q", gotOutput.Output)
	}
	if gotOutput.ExitStatus == nil || *gotOutput.ExitStatus != 42 {
		t.Errorf("ExitStatus: expected 42, got %v", gotOutput.ExitStatus)
	}

	waitResp := WaitForTerminalExitResponse{ExitStatus: 1}
	_, gotWait := roundTrip(t, waitResp)
	if gotWait.ExitStatus != 1 {
		t.Errorf("WaitForTerminalExitResponse.ExitStatus: got %d", gotWait.ExitStatus)
	}
}

func TestPermissionTypes_RoundTrip(t *testing.T) {
	req := RequestPermissionRequest{ToolName: "bash", Description: "run a script"}
	_, got := roundTrip(t, req)
	if got.ToolName != "bash" || got.Description != "run a script" {
		t.Errorf("RequestPermissionRequest round-trip: %+v", got)
	}

	resp := RequestPermissionResponse{Outcome: "approved"}
	_, gotResp := roundTrip(t, resp)
	if gotResp.Outcome != "approved" {
		t.Errorf("RequestPermissionResponse.Outcome: got %q", gotResp.Outcome)
	}
}

func TestFsTypes_RoundTrip(t *testing.T) {
	read := ReadTextFileRequest{Path: "/workspace/file.go"}
	_, gotRead := roundTrip(t, read)
	if gotRead.Path != "/workspace/file.go" {
		t.Errorf("ReadTextFileRequest.Path: got %q", gotRead.Path)
	}

	readResp := ReadTextFileResponse{Content: "package main"}
	_, gotReadResp := roundTrip(t, readResp)
	if gotReadResp.Content != "package main" {
		t.Errorf("ReadTextFileResponse.Content: got %q", gotReadResp.Content)
	}

	write := WriteTextFileRequest{Path: "/workspace/out.txt", Content: "data"}
	_, gotWrite := roundTrip(t, write)
	if gotWrite.Path != "/workspace/out.txt" || gotWrite.Content != "data" {
		t.Errorf("WriteTextFileRequest round-trip: %+v", gotWrite)
	}
}

func TestClientCaps_NilOmitted(t *testing.T) {
	caps := ClientCaps{} // both Fs and Terminal nil
	data, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	if _, hasFs := m["fs"]; hasFs {
		t.Error("fs should be omitted when nil")
	}
	if _, hasTerm := m["terminal"]; hasTerm {
		t.Error("terminal should be omitted when nil")
	}
}

func TestTerminalOutputResponse_NilExitStatus(t *testing.T) {
	resp := TerminalOutputResponse{Output: "running...", ExitStatus: nil}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	if _, hasExit := m["exitStatus"]; hasExit {
		t.Error("exitStatus should be omitted when nil")
	}
}
