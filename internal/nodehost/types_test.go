package nodehost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- JSON round-trip tests ---

func TestSystemRunParamsRoundTrip(t *testing.T) {
	raw := `"test"`
	cwd := "/tmp"
	timeout := 5000
	screen := true
	agent := "a1"
	session := "s1"
	approved := true
	decision := "allow-once"
	runID := "r1"
	suppress := false

	orig := SystemRunParams{
		Command:              []string{"echo", "hello"},
		RawCommand:           &raw,
		Cwd:                  &cwd,
		Env:                  map[string]string{"FOO": "bar"},
		TimeoutMs:            &timeout,
		NeedsScreenRecording: &screen,
		AgentID:              &agent,
		SessionKey:           &session,
		Approved:             &approved,
		ApprovalDecision:     &decision,
		RunID:                &runID,
		SuppressNotifyOnExit: &suppress,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SystemRunParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Command) != 2 || got.Command[0] != "echo" {
		t.Errorf("command mismatch: %v", got.Command)
	}
	if got.RawCommand == nil || *got.RawCommand != raw {
		t.Errorf("rawCommand mismatch")
	}
	if got.Env["FOO"] != "bar" {
		t.Errorf("env mismatch")
	}
}

func TestRunResultRoundTrip(t *testing.T) {
	exitCode := 0
	orig := RunResult{
		ExitCode:  &exitCode,
		TimedOut:  false,
		Success:   true,
		Stdout:    "output",
		Stderr:    "",
		Truncated: false,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got RunResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("exitCode mismatch")
	}
	if got.Stdout != "output" {
		t.Errorf("stdout mismatch: %q", got.Stdout)
	}
	if got.Success != true {
		t.Errorf("success should be true")
	}
}

func TestRunResultJSONFieldNames(t *testing.T) {
	exitCode := 1
	errMsg := "fail"
	r := RunResult{
		ExitCode:  &exitCode,
		TimedOut:  true,
		Success:   false,
		Stdout:    "out",
		Stderr:    "err",
		Error:     &errMsg,
		Truncated: true,
	}
	data, _ := json.Marshal(r)
	s := string(data)

	// Verify camelCase JSON field names match TS wire format.
	for _, field := range []string{"exitCode", "timedOut", "success", "stdout", "stderr", "error", "truncated"} {
		if !strings.Contains(s, `"`+field+`"`) {
			t.Errorf("missing JSON field %q in: %s", field, s)
		}
	}
}

func TestExecEventPayloadRoundTrip(t *testing.T) {
	exitCode := 0
	timedOut := false
	success := true
	suppress := false

	orig := ExecEventPayload{
		SessionKey:           "sk",
		RunID:                "rid",
		Host:                 "h1",
		Command:              "echo hi",
		ExitCode:             &exitCode,
		TimedOut:             &timedOut,
		Success:              &success,
		Output:               "hi",
		Reason:               "",
		SuppressNotifyOnExit: &suppress,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ExecEventPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SessionKey != "sk" || got.RunID != "rid" {
		t.Errorf("key fields mismatch")
	}
}

func TestExecFinishedEventParamsRoundTrip(t *testing.T) {
	exitCode := 0
	success := true
	orig := ExecFinishedEventParams{
		SessionKey:  "sk",
		RunID:       "rid",
		CommandText: "echo hi",
		Result: ExecFinishedResult{
			Stdout:   "hi",
			ExitCode: &exitCode,
			Success:  &success,
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ExecFinishedEventParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Result.Stdout != "hi" {
		t.Errorf("nested result mismatch")
	}
}

func TestSystemRunPolicyDecisionRoundTrip(t *testing.T) {
	decision := ApprovalAllowOnce
	orig := SystemRunPolicyDecision{
		Allowed:            false,
		AnalysisOk:         true,
		AllowlistSatisfied: false,
		RequiresAsk:        true,
		ApprovalDecision:   &decision,
		ApprovedByAsk:      false,
		EventReason:        EventReasonApprovalRequired,
		ErrorMessage:       "SYSTEM_RUN_DENIED: approval required",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SystemRunPolicyDecision
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Allowed != false {
		t.Errorf("allowed should be false")
	}
	if got.ApprovalDecision == nil || *got.ApprovalDecision != ApprovalAllowOnce {
		t.Errorf("approvalDecision mismatch")
	}
	if got.EventReason != EventReasonApprovalRequired {
		t.Errorf("eventReason mismatch: %q", got.EventReason)
	}
}

// --- ExecApprovalDecision tests ---

func TestResolveExecApprovalDecision(t *testing.T) {
	tests := []struct {
		input string
		want  *ExecApprovalDecision
	}{
		{"allow-once", ptr(ApprovalAllowOnce)},
		{"allow-always", ptr(ApprovalAllowAlways)},
		{"invalid", nil},
		{"", nil},
		{"deny", nil},
	}

	for _, tt := range tests {
		got := ResolveExecApprovalDecision(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("ResolveExecApprovalDecision(%q) = %v, want nil", tt.input, *got)
			}
		} else {
			if got == nil || *got != *tt.want {
				t.Errorf("ResolveExecApprovalDecision(%q) mismatch", tt.input)
			}
		}
	}
}

func ptr(d ExecApprovalDecision) *ExecApprovalDecision { return &d }

// --- Config tests ---

func TestConfigLoadMissingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)

	cfg, err := LoadNodeHostConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for missing file")
	}
}

func TestConfigLoadCorruptJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)
	os.WriteFile(filepath.Join(tmp, "node.json"), []byte("{invalid"), 0o600)

	_, err := LoadNodeHostConfig()
	if err == nil {
		t.Fatalf("expected error for corrupt JSON")
	}
}

func TestConfigLoadValid(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)

	data := `{"version":1,"nodeId":"abc-123","token":"tok","displayName":"my-node"}`
	os.WriteFile(filepath.Join(tmp, "node.json"), []byte(data), 0o600)

	cfg, err := LoadNodeHostConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.NodeID != "abc-123" {
		t.Errorf("nodeId mismatch: %q", cfg.NodeID)
	}
	if cfg.Token != "tok" {
		t.Errorf("token mismatch: %q", cfg.Token)
	}
}

func TestConfigNormalizeGeneratesUUID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)

	// Config with empty nodeId should get a generated UUID.
	data := `{"version":1,"nodeId":""}`
	os.WriteFile(filepath.Join(tmp, "node.json"), []byte(data), 0o600)

	cfg, err := LoadNodeHostConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.NodeID == "" {
		t.Fatalf("expected generated nodeId")
	}
	// UUID v4 format: 8-4-4-4-12 hex chars.
	if len(cfg.NodeID) != 36 {
		t.Errorf("nodeId length %d, want 36: %q", len(cfg.NodeID), cfg.NodeID)
	}
}

func TestConfigSavePermissions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)

	cfg := &NodeHostConfig{Version: 1, NodeID: "test-id"}
	if err := SaveNodeHostConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	filePath := filepath.Join(tmp, "node.json")
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}

	// Verify content round-trips.
	raw, _ := os.ReadFile(filePath)
	var got NodeHostConfig
	json.Unmarshal(raw, &got)
	if got.NodeID != "test-id" {
		t.Errorf("saved nodeId mismatch: %q", got.NodeID)
	}
}

func TestConfigEnsureCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)

	cfg, err := EnsureNodeHostConfig()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if cfg.NodeID == "" {
		t.Fatalf("expected generated nodeId")
	}
	if cfg.Version != 1 {
		t.Errorf("version = %d, want 1", cfg.Version)
	}

	// File should exist.
	if _, err := os.Stat(filepath.Join(tmp, "node.json")); err != nil {
		t.Fatalf("config file not created: %v", err)
	}
}

func TestConfigEnsureNormalizesExisting(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)

	// Write config with missing nodeId.
	data := `{"version":1,"nodeId":"","token":"keep-me"}`
	os.WriteFile(filepath.Join(tmp, "node.json"), []byte(data), 0o600)

	cfg, err := EnsureNodeHostConfig()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if cfg.NodeID == "" {
		t.Fatalf("expected generated nodeId")
	}
	if cfg.Token != "keep-me" {
		t.Errorf("token lost: %q", cfg.Token)
	}
}

func TestConfigSaveAtomicNoPartialWrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GOCLAW_STATE_DIR", tmp)

	cfg := &NodeHostConfig{Version: 1, NodeID: "atomic-test"}
	if err := SaveNodeHostConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Temp file should not remain.
	tmpFile := filepath.Join(tmp, "node.json.tmp")
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Errorf("temp file should not exist after save")
	}
}

func TestNewUUIDFormat(t *testing.T) {
	id, err := newUUID()
	if err != nil {
		t.Fatalf("newUUID: %v", err)
	}
	if len(id) != 36 {
		t.Fatalf("uuid length %d, want 36: %q", len(id), id)
	}
	// Check dashes at correct positions.
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("uuid dash positions wrong: %q", id)
	}
	// Version nibble should be '4'.
	if id[14] != '4' {
		t.Errorf("uuid version nibble = %c, want '4': %q", id[14], id)
	}
	// Variant bits at position 19 should be 8, 9, a, or b.
	v := id[19]
	if v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("uuid variant nibble = %c, want 8-b: %q", v, id)
	}
}

func TestNewUUIDUniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for range 100 {
		id, err := newUUID()
		if err != nil {
			t.Fatalf("newUUID: %v", err)
		}
		if seen[id] {
			t.Fatalf("duplicate uuid: %q", id)
		}
		seen[id] = true
	}
}

func TestSkillBinTrustEntryRoundTrip(t *testing.T) {
	orig := SkillBinTrustEntry{Name: "mytool", ResolvedPath: "/usr/bin/mytool"}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SkillBinTrustEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "mytool" || got.ResolvedPath != "/usr/bin/mytool" {
		t.Errorf("mismatch: %+v", got)
	}
}

func TestSystemRunApprovalPlanRoundTrip(t *testing.T) {
	cwd := "/home"
	agent := "a1"
	session := "s1"
	orig := SystemRunApprovalPlan{
		Argv:        []string{"echo", "hi"},
		Cwd:         &cwd,
		CommandText: "echo hi",
		AgentID:     &agent,
		SessionKey:  &session,
		MutableFileOperand: &SystemRunApprovalFileOperand{
			ArgvIndex: 1,
			Path:      "/tmp/f",
			SHA256:    "abc",
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SystemRunApprovalPlan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CommandText != "echo hi" {
		t.Errorf("commandText mismatch")
	}
	if got.MutableFileOperand == nil || got.MutableFileOperand.Path != "/tmp/f" {
		t.Errorf("mutableFileOperand mismatch")
	}
}
