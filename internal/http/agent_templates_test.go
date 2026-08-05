package http

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
)

// The starter prompts name skills and tools by their exact identifiers. A prompt
// that names something we do not ship is the worst kind of wrong: nothing fails
// to compile, no test complains, and the agent simply goes looking for a
// capability it cannot load and then explains its failure to the user.
//
// These tests read the REAL skills directory and the REAL tool sources, so a
// renamed skill or a deleted tool breaks them.
//
// Both catalogues are checked from here: the per-user starters in this package and
// the tenant-wide built-ins in bootstrap. They are separate lists for good reasons
// (who can edit them, who receives them) but they have the identical failure mode,
// and a guard that covered only one would leave the other free to rot.

type promptedAgent struct {
	Key          string
	DisplayName  string
	Emoji        string
	SystemPrompt string
	MaxIter      int
	Origin       string
}

func allPromptedAgents() []promptedAgent {
	var out []promptedAgent
	for _, t := range starterAgentTemplates {
		out = append(out, promptedAgent{t.Key, t.DisplayName, t.Emoji, t.SystemPrompt, t.MaxIter, "starter"})
	}
	for _, b := range bootstrap.BuiltinAgents {
		out = append(out, promptedAgent{b.Key, b.DisplayName, b.Emoji, b.SystemPrompt, b.MaxIter, "builtin"})
	}
	return out
}

// e.g. `use_skill`, `create_video` — two or more lowercase words joined by _.
var toolTokenRe = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

// A quoted identifier: a skill named in prose (the "pptx" skill) or a tool name
// as registered in source ("use_skill").
//
// The underscore in the class is load-bearing. Without it this collected zero
// snake_case names, so the tool check below passed vacuously against an empty
// set and then reported every real tool as missing — which is exactly what it
// did on first run.
var quotedRe = regexp.MustCompile(`"([a-z0-9_-]+)"`)

func shippedSkills(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "skills"))
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), "_") {
			out[e.Name()] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("no skills found — the scan would pass vacuously")
	}
	return out
}

// Every quoted string appearing in internal/tools, which is where tool names are
// registered. Substring matching against the concatenated source would also work,
// but a set makes the failure message exact.
func registeredToolNames(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	root := filepath.Join("..", "tools")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, m := range quotedRe.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tools: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no tool names found — the scan would pass vacuously")
	}
	return out
}

func TestStarterTemplatesNameRealSkills(t *testing.T) {
	skills := shippedSkills(t)
	for _, tmpl := range allPromptedAgents() {
		for _, m := range quotedRe.FindAllStringSubmatch(tmpl.SystemPrompt, -1) {
			word := m[1]
			// Only judge quoted words that LOOK like a skill reference — the
			// prompts also quote example titles and phrases. A quoted word that
			// matches a shipped skill is fine by definition; the failure case is
			// a quoted word that is skill-shaped and does not exist, so this
			// checks the ones the prompt itself frames as skills.
			if !strings.Contains(tmpl.SystemPrompt, `"`+word+`" skill`) {
				continue
			}
			if !skills[word] {
				t.Errorf("%s %q names skill %q, which is not in skills/ (have: %v)",
					tmpl.Origin, tmpl.Key, word, keys(skills))
			}
		}
	}
}

func TestStarterTemplatesNameRealTools(t *testing.T) {
	tools := registeredToolNames(t)
	// Words that are snake_case English rather than tool names. Kept explicit and
	// short: if this list starts growing, the prompts are drifting into jargon.
	allowed := map[string]bool{}
	for _, tmpl := range allPromptedAgents() {
		for _, tok := range toolTokenRe.FindAllString(tmpl.SystemPrompt, -1) {
			if allowed[tok] || tools[tok] {
				continue
			}
			t.Errorf("%s %q references %q, which is not a registered tool name in internal/tools",
				tmpl.Origin, tmpl.Key, tok)
		}
	}
}

func TestStarterTemplatesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tmpl := range allPromptedAgents() {
		if tmpl.Key == "" || tmpl.DisplayName == "" || tmpl.Emoji == "" {
			t.Errorf("template %+v is missing key, name or emoji", tmpl)
		}
		// The key becomes an agent_key prefix (`slides-019e8e88`), which routes
		// session keys and delegate(name=...) — a space or capital would produce
		// a slug nobody can type.
		if tmpl.Key != strings.ToLower(tmpl.Key) || strings.ContainsAny(tmpl.Key, " _/") {
			t.Errorf("template key %q is not a clean slug", tmpl.Key)
		}
		if seen[tmpl.Key] {
			t.Errorf("duplicate template key %q — the second clone would collide", tmpl.Key)
		}
		seen[tmpl.Key] = true
		if len(tmpl.SystemPrompt) < 200 {
			t.Errorf("template %q has a %d-char prompt; a starter that vague is worse than none",
				tmpl.Key, len(tmpl.SystemPrompt))
		}
		if tmpl.MaxIter <= 0 {
			t.Errorf("template %q has MaxIter %d — the agent could not act", tmpl.Key, tmpl.MaxIter)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
