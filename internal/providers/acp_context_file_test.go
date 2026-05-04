package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStableContextContent_StripsDynamicTags(t *testing.T) {
	in := `# Identity
You are atlas.

<current_reply_target>
  channel: telegram-atlas
  chat_id: 12345
  kind: direct
</current_reply_target>

<extra_context>
[크론 작업] morning briefing
</extra_context>

## Tooling
- read_file
- write_file
`
	out := stableContextContent(in)

	if strings.Contains(out, "<current_reply_target>") || strings.Contains(out, "chat_id") {
		t.Errorf("dynamic <current_reply_target> not stripped:\n%s", out)
	}
	if strings.Contains(out, "<extra_context>") || strings.Contains(out, "morning briefing") {
		t.Errorf("dynamic <extra_context> not stripped:\n%s", out)
	}
	if !strings.Contains(out, "## Tooling") || !strings.Contains(out, "atlas") {
		t.Errorf("stable portion lost:\n%s", out)
	}
}

func TestStableContextContent_HashStableAcrossDynamicChanges(t *testing.T) {
	tpl := func(chatID, extra string) string {
		return `# Identity
atlas

<current_reply_target>
  chat_id: ` + chatID + `
</current_reply_target>

<extra_context>
` + extra + `
</extra_context>

## Tooling
read_file
`
	}
	a := stableContextContent(tpl("111", "first call"))
	b := stableContextContent(tpl("999", "tenth call — different content"))
	if a != b {
		t.Errorf("stable content changed across dynamic-only diffs:\nA:\n%s\nB:\n%s", a, b)
	}
}

func TestStableContextContent_DifferentStaticGivesDifferentHash(t *testing.T) {
	a := stableContextContent("# A\ntext1")
	b := stableContextContent("# B\ntext2")
	hashA := sha256.Sum256([]byte(a))
	hashB := sha256.Sum256([]byte(b))
	if hex.EncodeToString(hashA[:]) == hex.EncodeToString(hashB[:]) {
		t.Errorf("hashes equal for different static content")
	}
}

func TestWriteContextFile_SkipsWhenStableHashMatches(t *testing.T) {
	dir := t.TempDir()
	p := &ACPProvider{contextFileName: "GEMINI.md"}
	prompt := "# Static\nTooling\n\n<current_reply_target>chat_id: 1\n</current_reply_target>"

	// First write — should create file + sidecar.
	if !p.writeContextFile(dir, prompt) {
		t.Fatal("first call should write")
	}
	stat1, err := os.Stat(filepath.Join(dir, "GEMINI.md"))
	if err != nil {
		t.Fatal(err)
	}
	hashPath := filepath.Join(dir, "GEMINI.md.sha256")
	if _, err := os.Stat(hashPath); err != nil {
		t.Fatalf("sidecar should exist: %v", err)
	}

	// Second call with only dynamic-tag content changing → no rewrite.
	prompt2 := "# Static\nTooling\n\n<current_reply_target>chat_id: 999\n</current_reply_target>"
	if p.writeContextFile(dir, prompt2) {
		t.Error("dynamic-only change should not rewrite")
	}
	stat2, _ := os.Stat(filepath.Join(dir, "GEMINI.md"))
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Error("file mtime changed despite stable content unchanged")
	}

	// Third call with static change → rewrite.
	prompt3 := "# Different Static\nTooling\n\n<current_reply_target>chat_id: 1\n</current_reply_target>"
	if !p.writeContextFile(dir, prompt3) {
		t.Error("static change should trigger rewrite")
	}
}
