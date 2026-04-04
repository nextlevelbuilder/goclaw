package tools

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// Registry manages tool registration and execution.
type Registry struct {
	tools       map[string]Tool
	aliases     map[string]string // alias name → canonical tool name
	disabled    map[string]bool   // tools disabled via admin UI (kept in registry, excluded from List)
	mu          sync.RWMutex
	rateLimiter *ToolRateLimiter // nil = no rate limiting
	scrubbing   bool             // scrub credentials from output (default true)

	// deferredActivator is called when a tool is not in the registry but may be
	// a deferred MCP tool. Returns true if the tool was successfully activated.
	deferredActivator func(name string) bool
}

func NewRegistry() *Registry {
	return &Registry{
		tools:     make(map[string]Tool),
		aliases:   make(map[string]string),
		disabled:  make(map[string]bool),
		scrubbing: true, // enabled by default
	}
}

// SetDeferredActivator registers a callback that activates deferred tools on demand.
// Used by the MCP Manager to enable lazy activation when a deferred tool is called directly.
func (r *Registry) SetDeferredActivator(fn func(name string) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deferredActivator = fn
}

// TryActivateDeferred attempts to activate a named tool via the deferred activator.
// Returns true if the tool is now in the registry (either already was or just activated).
func (r *Registry) TryActivateDeferred(name string) bool {
	r.mu.RLock()
	fn := r.deferredActivator
	r.mu.RUnlock()
	if fn == nil {
		return false
	}
	return fn(name)
}

// SetRateLimiter enables per-key tool rate limiting.
func (r *Registry) SetRateLimiter(rl *ToolRateLimiter) {
	r.rateLimiter = rl
}

// SetScrubbing enables or disables credential scrubbing on tool output.
func (r *Registry) SetScrubbing(enabled bool) {
	r.scrubbing = enabled
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// RegisterAlias maps an alias name to a canonical tool name.
// Rejected if alias collides with an existing real tool.
func (r *Registry) RegisterAlias(alias, canonical string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[alias]; exists {
		slog.Warn("alias conflicts with registered tool", "alias", alias, "canonical", canonical)
		return
	}
	r.aliases[alias] = canonical
}

// Aliases returns a copy of the alias map.
func (r *Registry) Aliases() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make(map[string]string, len(r.aliases))
	maps.Copy(cp, r.aliases)
	return cp
}

// resolve looks up a tool by name, checking real tools first, then aliases.
// Disabled tools are excluded.
func (r *Registry) resolve(name string) (Tool, bool) {
	if t, ok := r.tools[name]; ok {
		if r.disabled[name] {
			return nil, false
		}
		return t, true
	}
	if canonical, ok := r.aliases[name]; ok {
		if r.disabled[canonical] {
			return nil, false
		}
		t, ok := r.tools[canonical]
		return t, ok
	}
	return nil, false
}

// Get returns a tool by name (checks real tools first, then aliases).
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolve(name)
}

// Unregister removes a tool from the registry by name.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Execute runs a tool by name with the given arguments.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) *Result {
	return r.ExecuteWithContext(ctx, name, args, "", "", "", "", nil)
}

// ExecuteWithContext runs a tool with channel/chat/session context and optional async callback.
// peerKind is "direct" or "group" (used by spawn/subagent tools for session key building).
// sessionKey is used to resolve sandbox scope (used by SandboxAware tools).
//
// Context values are injected into ctx so tools can read them without mutable fields,
// making tool instances thread-safe for concurrent execution.
func (r *Registry) ExecuteWithContext(ctx context.Context, name string, args map[string]any, channel, chatID, peerKind, sessionKey string, asyncCB AsyncCallback) *Result {
	r.mu.RLock()
	tool, ok := r.resolve(name)
	r.mu.RUnlock()

	if !ok {
		return ErrorResult("unknown tool: " + name)
	}

	// Inject per-call values into context (immutable — safe for concurrent use)
	if channel != "" {
		ctx = WithToolChannel(ctx, channel)
	}
	if chatID != "" {
		ctx = WithToolChatID(ctx, chatID)
	}
	if peerKind != "" {
		ctx = WithToolPeerKind(ctx, peerKind)
	}
	if sessionKey != "" {
		ctx = WithToolSandboxKey(ctx, sessionKey)
		ctx = WithToolSessionKey(ctx, sessionKey)
	}
	if asyncCB != nil {
		ctx = WithToolAsyncCB(ctx, asyncCB)
	}

	// Rate limit check (per session key)
	if r.rateLimiter != nil && sessionKey != "" {
		if err := r.rateLimiter.Allow(sessionKey); err != nil {
			return ErrorResult(err.Error())
		}
	}

	// Detect empty tool call arguments — typically caused by providers truncating
	// or dropping arguments when output is too large (e.g. DashScope with long content).
	// Give the model an actionable hint instead of a confusing "X is required" error.
	if len(args) == 0 {
		if params := tool.Parameters(); params != nil {
			if req, ok := params["required"].([]string); ok && len(req) > 0 {
				return ErrorResult(fmt.Sprintf(
					"Tool call had empty arguments (required: %s). "+
						"This usually means your previous response was too long for the API to include tool parameters. "+
						"Try again with shorter content — split into smaller parts if needed.",
					strings.Join(req, ", ")))
			}
		}
	}

	start := time.Now()
	result := safeExecute(tool, ctx, args)
	duration := time.Since(start)

	// Scrub credentials from tool output before returning to LLM
	if r.scrubbing {
		if result.ForLLM != "" {
			result.ForLLM = ScrubCredentials(result.ForLLM)
		}
		if result.ForUser != "" {
			result.ForUser = ScrubCredentials(result.ForUser)
		}
	}

	slog.Debug("tool executed",
		"tool", name,
		"duration_ms", duration.Milliseconds(),
		"is_error", result.IsError,
		"async", result.Async,
	)

	return result
}

// safeExecute runs tool.Execute with panic recovery. A panicking tool returns
// an error result instead of crashing the process.
func safeExecute(tool Tool, ctx context.Context, args map[string]any) (result *Result) {
	defer safego.Recover(func(v any) {
		result = ErrorResult(fmt.Sprintf("tool %q panicked: %v", tool.Name(), v))
	}, "tool", tool.Name())
	return tool.Execute(ctx, args)
}

// ProviderDefs returns tool definitions for LLM provider APIs.
// Includes alias definitions (same params/description, alias name).
func (r *Registry) ProviderDefs() []providers.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]providers.ToolDefinition, 0, len(r.tools)+len(r.aliases))
	for name, tool := range r.tools {
		if r.disabled[name] {
			continue
		}
		defs = append(defs, ToProviderDef(tool))
	}
	for alias, canonical := range r.aliases {
		if r.disabled[canonical] {
			continue
		}
		tool, ok := r.tools[canonical]
		if !ok {
			continue
		}
		defs = append(defs, providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionSchema{
				Name:        alias,
				Description: tool.Description(),
				Parameters:  tool.Parameters(),
			},
		})
	}
	return defs
}

// List returns all registered canonical tool names (excludes aliases).
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		if !r.disabled[name] {
			names = append(names, name)
		}
	}
	return names
}

// Disable marks a tool as disabled (excluded from List and policy evaluation)
// without removing it from the registry. Can be re-enabled later.
func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled[name] = true
}

// Enable re-enables a previously disabled tool.
func (r *Registry) Enable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.disabled, name)
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Clone creates a shallow copy of the registry with all registered tools and aliases.
// The clone shares the rate limiter (thread-safe) and scrubbing setting.
// Used by subagent toolsFactory so subagents inherit parent tools (web_fetch, web_search, etc.).
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := &Registry{
		tools:       make(map[string]Tool, len(r.tools)),
		aliases:     make(map[string]string, len(r.aliases)),
		disabled:    make(map[string]bool, len(r.disabled)),
		rateLimiter: r.rateLimiter,
		scrubbing:   r.scrubbing,
	}
	maps.Copy(clone.tools, r.tools)
	maps.Copy(clone.aliases, r.aliases)
	maps.Copy(clone.disabled, r.disabled)
	return clone
}
