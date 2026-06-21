package cmd

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"
)

func TestSkillsDepsInstallUsesPerSkillInstallEndpoint(t *testing.T) {
	calls := captureSkillGatewayCalls(t)

	cmd := skillsDepsCmd()
	cmd.SetArgs([]string{"install", "skill-123", "--json"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(calls.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(calls.requests))
	}
	got := calls.requests[0]
	if got.method != http.MethodPost || got.path != "/v1/skills/skill-123/dependencies/install" {
		t.Fatalf("request = %+v", got)
	}
}

func TestSkillsAccessSetUsesPatchModeBody(t *testing.T) {
	calls := captureSkillGatewayCalls(t)

	cmd := skillsAccessCmd()
	cmd.SetArgs([]string{"set", "skill-123", "--mode", "public", "--json"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(calls.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(calls.requests))
	}
	got := calls.requests[0]
	if got.method != http.MethodPatch || got.path != "/v1/skills/skill-123/access" {
		t.Fatalf("request = %+v", got)
	}
	if !reflect.DeepEqual(got.body, map[string]any{"mode": "public"}) {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestSkillsGrantAgentUsesPluralGrantEndpoint(t *testing.T) {
	calls := captureSkillGatewayCalls(t)

	cmd := skillsGrantCmd()
	cmd.SetArgs([]string{"agent", "skill-123", "agent-456", "--can-manage", "--pinned-version", "7", "--json"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(calls.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(calls.requests))
	}
	got := calls.requests[0]
	if got.method != http.MethodPost || got.path != "/v1/skills/skill-123/grants/agents" {
		t.Fatalf("request = %+v", got)
	}
	want := map[string]any{"agent_id": "agent-456", "can_manage": true, "pinned_version": 7}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %#v, want %#v", got.body, want)
	}
}

func TestSkillsAccessEffectiveBuildsOptionalSkillURL(t *testing.T) {
	calls := captureSkillGatewayCalls(t)

	cmd := skillsAccessCmd()
	cmd.SetArgs([]string{"effective", "skill-123", "--agent", "agent-456", "--user", "user-789", "--json"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(calls.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(calls.requests))
	}
	got := calls.requests[0]
	wantPath := "/v1/skills/skill-123/access/effective?agent_id=agent-456&user_id=user-789"
	if got.method != http.MethodGet || got.path != wantPath {
		t.Fatalf("request = %+v, want path %s", got, wantPath)
	}
}

type skillGatewayCall struct {
	method string
	path   string
	body   any
}

type capturedSkillGatewayCalls struct {
	requests []skillGatewayCall
}

func captureSkillGatewayCalls(t *testing.T) *capturedSkillGatewayCalls {
	t.Helper()
	calls := &capturedSkillGatewayCalls{}
	prevDo := skillsGatewayDo
	prevDelete := skillsGatewayDelete
	prevRequire := skillsRequireGateway
	prevStdout := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		_ = w.Close()
		_, _ = io.Copy(io.Discard, r)
		_ = r.Close()
		os.Stdout = prevStdout
		skillsGatewayDo = prevDo
		skillsGatewayDelete = prevDelete
		skillsRequireGateway = prevRequire
	})

	skillsRequireGateway = func() {}
	skillsGatewayDo = func(method, path string, body any) (map[string]any, error) {
		calls.requests = append(calls.requests, skillGatewayCall{method: method, path: path, body: body})
		return map[string]any{"ok": true}, nil
	}
	skillsGatewayDelete = func(path string) error {
		calls.requests = append(calls.requests, skillGatewayCall{method: http.MethodDelete, path: path})
		return nil
	}

	return calls
}

func TestRunLocalSkillDepsStatusPrintsJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/SKILL.md", []byte("---\nname: Demo\ndeps:\n  - system:goclaw-missing-test-bin\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	var out bytes.Buffer
	if err := runLocalSkillDepsStatus(&out, dir, true); err != nil {
		t.Fatalf("runLocalSkillDepsStatus: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"missing_count": 1`)) {
		t.Fatalf("output = %s", out.String())
	}
}
