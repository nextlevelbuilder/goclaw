package agent

import (
	"context"
	"fmt"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// mockToolLister implements the widened ToolLister interface for testing.
type mockToolLister struct {
	tools   map[string]string // name → description
	aliases map[string]string // alias → canonical
}

func (m *mockToolLister) List() []string {
	names := make([]string, 0, len(m.tools))
	for n := range m.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (m *mockToolLister) Get(name string) (tools.Tool, bool) {
	desc, ok := m.tools[name]
	if !ok {
		return nil, false
	}
	return &mockTool{name: name, desc: desc}, true
}

func (m *mockToolLister) Aliases() map[string]string {
	if m.aliases == nil {
		return nil
	}
	return m.aliases
}

// mockTool is a minimal tools.Tool implementation for testing.
type mockTool struct {
	name string
	desc string
}

func (t *mockTool) Name() string                                              { return t.name }
func (t *mockTool) Description() string                                       { return t.desc }
func (t *mockTool) Parameters() map[string]any                                { return nil }
func (t *mockTool) Execute(_ context.Context, _ map[string]any) *tools.Result { return nil }

// mockSkillsLoader implements the widened SkillsLoader interface.
type mockSkillsLoader struct {
	pinned        string        // pre-built pinned XML
	summary       string        // pre-built full summary
	infos         []skills.Info // what FilterSkills returns; drives inline-vs-search
	capturedAllow []string      // set by BuildSummary for test assertions
}

func (m *mockSkillsLoader) BuildPinnedSummary(_ context.Context, _ []string) string {
	return m.pinned
}

func (m *mockSkillsLoader) BuildSummary(_ context.Context, allowList []string) string {
	m.capturedAllow = allowList
	return m.summary
}

// FilterSkills mirrors the real loader's allowList convention: nil means everything,
// an empty slice means nothing, a populated slice selects by slug.
func (m *mockSkillsLoader) FilterSkills(_ context.Context, allowList []string) []skills.Info {
	if allowList == nil {
		return m.infos
	}
	if len(allowList) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(allowList))
	for _, s := range allowList {
		allowed[s] = true
	}
	var out []skills.Info
	for _, s := range m.infos {
		if allowed[s.Slug] {
			out = append(out, s)
		}
	}
	return out
}

// skillInfoN builds n skills whose combined name+description length puts the estimate
// either side of the inline threshold, depending on descLen.
func skillInfoN(n, descLen int) []skills.Info {
	out := make([]skills.Info, 0, n)
	for i := range n {
		out = append(out, skills.Info{
			Slug:        fmt.Sprintf("skill-%d", i),
			Name:        fmt.Sprintf("skill-%d", i),
			Description: strings.Repeat("x", descLen),
		})
	}
	return out
}

// mockMCPLister implements MCPPreviewLister for testing.
type mockMCPLister struct {
	tools []MCPToolPreviewInfo
	err   error
}

func (m *mockMCPLister) ListToolsForAgent(_ context.Context, _ uuid.UUID, _ string) ([]MCPToolPreviewInfo, error) {
	return m.tools, m.err
}

// mockSkillAccessStore returns canned skill access lists.
type mockSkillAccessStore struct {
	accessible []store.SkillInfo
	err        error
}

func (m *mockSkillAccessStore) ListAccessible(_ context.Context, _ uuid.UUID, _ string) ([]store.SkillInfo, error) {
	return m.accessible, m.err
}
