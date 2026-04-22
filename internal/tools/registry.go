package tools

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// Registry manages tool registration and execution.
type Registry struct {
	tools       map[string]Tool
	metadata    map[string]ToolMetadata // per-tool capability metadata
	aliases     map[string]string       // alias name → canonical tool name
	disabled    map[string]bool         // tools disabled via admin UI (kept in registry, excluded from List)
	mu          sync.RWMutex
	rateLimiter *ToolRateLimiter // nil = no rate limiting
	scrubbing   bool             // scrub credentials from output (default true)

	// Per-registry tool groups (eliminates global map race condition).
	// MCP tools register their groups here so each Loop has isolated namespace.
	toolGroups   map[string][]string
	toolGroupsMu sync.RWMutex

	// deferredActivator is called when a tool is not in the registry but may be
	// a deferred MCP tool. Returns true if the tool was successfully activated.
	deferredActivator func(name string) bool
}

func NewRegistry() *Registry {
	r := &Registry{
		tools:      make(map[string]Tool),
		metadata:   make(map[string]ToolMetadata),
		aliases:    make(map[string]string),
		disabled:   make(map[string]bool),
		toolGroups: make(map[string][]string),
		scrubbing:  true, // enabled by default
	}
	// Seed built-in tool groups (deep copy from package-level constant data)
	for name, members := range builtinToolGroups {
		r.toolGroups[name] = append([]string(nil), members...)
	}
	return r
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

// RegisterWithMetadata adds a tool with explicit capability metadata.
func (r *Registry) RegisterWithMetadata(tool Tool, meta ToolMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := tool.Name()
	r.tools[name] = tool
	meta.Name = name
	r.metadata[name] = meta
}

// GetMetadata returns capability metadata for a tool.
// Returns inferred defaults if no explicit metadata was registered.
func (r *Registry) GetMetadata(name string) ToolMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.metadata[name]; ok {
		return m
	}
	return inferMetadata(name)
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
// Results are sorted by tool name for deterministic ordering (prompt caching).
func (r *Registry) ProviderDefs() []providers.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Sort canonical tool names for deterministic ordering.
	sortedNames := make([]string, 0, len(r.tools))
	for name := range r.tools {
		if !r.disabled[name] {
			sortedNames = append(sortedNames, name)
		}
	}
	slices.Sort(sortedNames)

	defs := make([]providers.ToolDefinition, 0, len(sortedNames)+len(r.aliases))
	for _, name := range sortedNames {
		defs = append(defs, ToProviderDef(r.tools[name]))
	}

	// Sort alias names for deterministic ordering.
	sortedAliases := make([]string, 0, len(r.aliases))
	for alias := range r.aliases {
		sortedAliases = append(sortedAliases, alias)
	}
	slices.Sort(sortedAliases)

	for _, alias := range sortedAliases {
		canonical := r.aliases[alias]
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
// Results are sorted lexicographically for deterministic ordering — critical
// for LLM prompt caching (Anthropic/OpenAI cache by exact prefix match).
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		if !r.disabled[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
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
	r.toolGroupsMu.RLock()
	defer r.toolGroupsMu.RUnlock()

	clone := &Registry{
		tools:       make(map[string]Tool, len(r.tools)),
		metadata:    make(map[string]ToolMetadata, len(r.metadata)),
		aliases:     make(map[string]string, len(r.aliases)),
		disabled:    make(map[string]bool, len(r.disabled)),
		toolGroups:  make(map[string][]string, len(r.toolGroups)),
		rateLimiter: r.rateLimiter,
		scrubbing:   r.scrubbing,
	}
	maps.Copy(clone.tools, r.tools)
	maps.Copy(clone.metadata, r.metadata)
	maps.Copy(clone.aliases, r.aliases)
	maps.Copy(clone.disabled, r.disabled)
	// Deep-copy toolGroups (each slice must be copied)
	for name, members := range r.toolGroups {
		clone.toolGroups[name] = append([]string(nil), members...)
	}
	return clone
}

// RegisterToolGroup adds or replaces a dynamic tool group.
// Used by the MCP manager to register "mcp" and "mcp:{serverName}" groups.
func (r *Registry) RegisterToolGroup(name string, members []string) {
	r.toolGroupsMu.Lock()
	r.toolGroups[name] = members
	r.toolGroupsMu.Unlock()
}

// MergeToolGroup adds members to an existing tool group without removing existing entries.
// Used by per-user MCP tool loading to extend the "mcp" group incrementally.
func (r *Registry) MergeToolGroup(name string, members []string) {
	r.toolGroupsMu.Lock()
	defer r.toolGroupsMu.Unlock()
	existing := r.toolGroups[name]
	seen := make(map[string]bool, len(existing))
	for _, m := range existing {
		seen[m] = true
	}
	for _, m := range members {
		if !seen[m] {
			existing = append(existing, m)
			seen[m] = true
		}
	}
	r.toolGroups[name] = existing
}

// UnregisterToolGroup removes a dynamic tool group.
func (r *Registry) UnregisterToolGroup(name string) {
	r.toolGroupsMu.Lock()
	delete(r.toolGroups, name)
	r.toolGroupsMu.Unlock()
}

// GetToolGroup returns the members of a tool group (thread-safe).
func (r *Registry) GetToolGroup(name string) ([]string, bool) {
	r.toolGroupsMu.RLock()
	defer r.toolGroupsMu.RUnlock()
	members, ok := r.toolGroups[name]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent mutation
	return append([]string(nil), members...), true
}

// ExpandToolGroups expands a spec list (which may contain "group:xxx") into concrete tool names,
// filtered against available tools. Thread-safe.
func (r *Registry) ExpandToolGroups(available []string, spec []string) []string {
	r.toolGroupsMu.RLock()
	defer r.toolGroupsMu.RUnlock()

	expanded := make(map[string]bool)
	for _, s := range spec {
		if after, ok := strings.CutPrefix(s, "group:"); ok {
			if members, ok := r.toolGroups[after]; ok {
				for _, m := range members {
					expanded[m] = true
				}
			}
		} else {
			expanded[s] = true
		}
	}

	var result []string
	for _, t := range available {
		if expanded[t] {
			result = append(result, t)
		}
	}
	return result
}

// MatchDenySpec returns true if name matches any entry in the deny spec (with group expansion).
func (r *Registry) MatchDenySpec(name string, spec []string) bool {
	r.toolGroupsMu.RLock()
	defer r.toolGroupsMu.RUnlock()

	for _, s := range spec {
		if after, ok := strings.CutPrefix(s, "group:"); ok {
			if members, ok := r.toolGroups[after]; ok {
				if slices.Contains(members, name) {
					return true
				}
			}
		} else if s == name {
			return true
		}
	}
	return false
}
