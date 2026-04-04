package tools

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ToolExecutor abstracts tool execution for dependency inversion.
// Production uses *Registry; tests can inject a mock.
type ToolExecutor interface {
	ExecuteWithContext(ctx context.Context, name string, args map[string]any, channel, chatID, peerKind, sessionKey string, asyncCB AsyncCallback) *Result
	TryActivateDeferred(name string) bool
	ProviderDefs() []providers.ToolDefinition
	Get(name string) (Tool, bool)
	List() []string
	Aliases() map[string]string
}

// Compile-time check: *Registry satisfies ToolExecutor.
var _ ToolExecutor = (*Registry)(nil)
