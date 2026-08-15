package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestDropBuiltinUserFileIfPredefined_WithUserPredefined verifies that USER.md
// is dropped from the merged context files for predefined agents when
// USER_PREDEFINED.md is present — the operator owns the user-context prompt.
func TestDropBuiltinUserFileIfPredefined_WithUserPredefined(t *testing.T) {
	files := []bootstrap.ContextFile{
		{Path: bootstrap.UserFile, Content: "built-in user template"},
		{Path: bootstrap.UserPredefinedFile, Content: "operator authored"},
		{Path: bootstrap.SoulFile, Content: "soul"},
	}

	got := dropBuiltinUserFileIfPredefined(store.AgentTypePredefined, files)

	for _, f := range got {
		assert.NotEqual(t, bootstrap.UserFile, f.Path, "USER.md must be dropped when USER_PREDEFINED.md is present")
	}
	assert.Len(t, got, 2)
}

// TestDropBuiltinUserFileIfPredefined_WithoutUserPredefined verifies USER.md is
// kept for predefined agents that do NOT have USER_PREDEFINED.md.
func TestDropBuiltinUserFileIfPredefined_WithoutUserPredefined(t *testing.T) {
	files := []bootstrap.ContextFile{
		{Path: bootstrap.UserFile, Content: "built-in user template"},
		{Path: bootstrap.SoulFile, Content: "soul"},
	}

	got := dropBuiltinUserFileIfPredefined(store.AgentTypePredefined, files)

	assert.Equal(t, files, got)
}

// TestDropBuiltinUserFileIfPredefined_OpenAgentUntouched verifies open agents
// are never affected, even if USER_PREDEFINED.md were somehow present.
func TestDropBuiltinUserFileIfPredefined_OpenAgentUntouched(t *testing.T) {
	files := []bootstrap.ContextFile{
		{Path: bootstrap.UserFile, Content: "built-in user template"},
		{Path: bootstrap.UserPredefinedFile, Content: "operator authored"},
	}

	got := dropBuiltinUserFileIfPredefined(store.AgentTypeOpen, files)

	assert.Equal(t, files, got)
}
