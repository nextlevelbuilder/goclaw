package agent

import (
	"context"
	"sort"

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

func (t *mockTool) Name() string                                          { return t.name }
func (t *mockTool) Description() string                                   { return t.desc }
func (t *mockTool) Parameters() map[string]any                            { return nil }
func (t *mockTool) Execute(_ context.Context, _ map[string]any) *tools.Result { return nil }

// mockSkillsLoader implements the widened SkillsLoader interface.
type mockSkillsLoader struct {
	pinned        string   // pre-built pinned XML
	summary       string   // pre-built full summary
	capturedAllow []string // set by BuildSummary for test assertions
}

func (m *mockSkillsLoader) BuildPinnedSummary(_ context.Context, _ []string) string {
	return m.pinned
}

func (m *mockSkillsLoader) BuildSummary(_ context.Context, allowList []string) string {
	m.capturedAllow = allowList
	return m.summary
}

// mockSkillAccessStore returns canned skill access lists.
type mockSkillAccessStore struct {
	accessible []store.SkillInfo
	err        error
}

func (m *mockSkillAccessStore) ListAccessible(_ context.Context, _ uuid.UUID, _ string) ([]store.SkillInfo, error) {
	return m.accessible, m.err
}
