package methods

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/clisession"
)

// "Approve always" must silence the SAME action and nothing more. The key is the
// exact command, not its binary: a binary-level key would make an always-allow on
// `echo hello` cover `echo $(curl … | sh)`, and one on `git push` cover
// `git push --force`.
func TestCLIAlwaysAllowKeyIsExactCommand(t *testing.T) {
	key := func(tool, input string) string {
		return cliAlwaysAllowKey(clisession.PermissionRequest{
			ToolName: tool,
			Input:    json.RawMessage(input),
		})
	}

	base := key("Bash", `{"command":"echo hello"}`)

	if same := key("Bash", `{"command":"echo hello"}`); same != base {
		t.Errorf("the identical command produced a different key: %q vs %q", same, base)
	}
	// Whitespace differences are not a different command.
	if trimmed := key("Bash", `{"command":"  echo hello  "}`); trimmed != base {
		t.Errorf("whitespace changed the key: %q vs %q", trimmed, base)
	}

	for name, input := range map[string]string{
		"different args":    `{"command":"echo goodbye"}`,
		"appended pipeline": `{"command":"echo hello | sh"}`,
		"same binary only":  `{"command":"echo"}`,
		"chained command":   `{"command":"echo hello && rm -rf /"}`,
	} {
		if got := key("Bash", input); got == base {
			t.Errorf("%s must NOT reuse the approved key (input %s)", name, input)
		}
	}

	// A different tool with the same command text is a different action.
	if got := key("Shell", `{"command":"echo hello"}`); got == base {
		t.Error("a different tool reused the approved key")
	}
}

// Tools with no command are keyed by tool name, so "always" means "this kind of
// action in this conversation" — the CLIs' own accept-edits behaviour.
func TestCLIAlwaysAllowKeyFallsBackToTool(t *testing.T) {
	edit := cliAlwaysAllowKey(clisession.PermissionRequest{
		ToolName: "Edit",
		Input:    json.RawMessage(`{"file_path":"/workspace/a.go","old_string":"x","new_string":"y"}`),
	})
	other := cliAlwaysAllowKey(clisession.PermissionRequest{
		ToolName: "Edit",
		Input:    json.RawMessage(`{"file_path":"/workspace/b.go","old_string":"p","new_string":"q"}`),
	})
	if edit != other {
		t.Errorf("two edits keyed differently (%q vs %q); always-allow on edits would never hit", edit, other)
	}

	write := cliAlwaysAllowKey(clisession.PermissionRequest{ToolName: "Write"})
	if write == edit {
		t.Error("Write and Edit share a key; approving one would silence the other")
	}

	// Malformed or absent input must not collapse every tool onto one key.
	broken := cliAlwaysAllowKey(clisession.PermissionRequest{ToolName: "Bash", Input: json.RawMessage(`not json`)})
	if broken == cliAlwaysAllowKey(clisession.PermissionRequest{ToolName: "Edit"}) {
		t.Error("unparseable input collapsed two different tools onto one key")
	}
}

// The memory is per-relay (per conversation), so a decision in one chat must not
// govern another.
func TestAlwaysAllowIsScopedToTheConversation(t *testing.T) {
	a := &cliRelay{sessionKey: "chat-a"}
	b := &cliRelay{sessionKey: "chat-b"}

	pr := clisession.PermissionRequest{ToolName: "Bash", Input: json.RawMessage(`{"command":"go test ./..."}`)}
	key := cliAlwaysAllowKey(pr)

	if a.allowedAlways(key) {
		t.Fatal("a fresh conversation already had an always-allow")
	}
	a.rememberAlways(key)

	if !a.allowedAlways(key) {
		t.Error("the conversation that approved it does not remember")
	}
	if b.allowedAlways(key) {
		t.Error("an always-allow leaked into another conversation")
	}

	// A neighbouring command in the SAME conversation is still asked about.
	other := cliAlwaysAllowKey(clisession.PermissionRequest{
		ToolName: "Bash",
		Input:    json.RawMessage(`{"command":"go test ./... -run Secret"}`),
	})
	if a.allowedAlways(other) {
		t.Error("always-allow bled onto a different command in the same conversation")
	}
}

// A tool whose ask can never be satisfied over this transport must not reach the
// user: clicking Approve on it cannot work, so the prompt is a dead end.
func TestUnanswerableToolsAreRefusedWithGuidance(t *testing.T) {
	reason, unanswerable := cliUnanswerableTool("cli:AskUserQuestion")
	if !unanswerable {
		t.Fatal("AskUserQuestion should be refused: this transport carries messages, not a structured choice")
	}
	// The refusal has to tell the model what to do INSTEAD, or the turn just stops.
	if !strings.Contains(strings.ToLower(reason), "plain text") {
		t.Errorf("refusal does not redirect the model to prose: %q", reason)
	}

	// The bare name must work too — the prefix is added by cliApprovalToolName.
	if _, ok := cliUnanswerableTool("AskUserQuestion"); !ok {
		t.Error("the unprefixed tool name was not recognised")
	}

	// Everything consequential still goes to the user. This list is the guard
	// against the helper quietly growing into a bypass.
	for _, tool := range []string{"Bash", "Edit", "Write", "WebFetch", "Task", "NotebookEdit", "Read"} {
		if _, ok := cliUnanswerableTool(tool); ok {
			t.Errorf("%s must still be answered by the user, not handled internally", tool)
		}
	}
}
