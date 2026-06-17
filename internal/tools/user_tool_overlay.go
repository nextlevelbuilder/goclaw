package tools

// userToolOverlay is a request-scoped ToolExecutor that overlays a set of per-user
// tools on top of a base executor (the shared registry). It lets the PolicyEngine
// evaluate AND emit per-user MCP tools (require_user_credentials servers) under the
// SAME allow/deny rules as registry tools, WITHOUT registering the per-user tool
// objects into the shared registry — doing so would leak one user's credentialed
// BridgeTool to every other user (see agent.getUserMCPTools).
//
// Construct one per request from the calling actor's tools (see
// agent.buildFilteredTools) and discard it after the tool-filtering pass. The base
// executor is embedded, so every ToolExecutor method except List/Get delegates to
// it unchanged.
type userToolOverlay struct {
	ToolExecutor                 // embedded base (shared registry)
	extra        map[string]Tool // per-user tools by name; take precedence in Get
	names        []string        // per-user names, insertion order, appended by List
}

// NewUserToolOverlay returns a ToolExecutor exposing base's tools plus userTools.
// Duplicate names within userTools collapse (first wins); nil entries are skipped.
// For List(), per-user names already present in base are not duplicated. For Get(),
// a per-user tool shadows a same-named base tool — per-user objects carry the
// actor's credentials. Returns base unchanged when userTools is empty, so callers
// pay nothing on the common no-per-user path.
func NewUserToolOverlay(base ToolExecutor, userTools []Tool) ToolExecutor {
	if len(userTools) == 0 {
		return base
	}
	extra := make(map[string]Tool, len(userTools))
	names := make([]string, 0, len(userTools))
	for _, t := range userTools {
		if t == nil {
			continue
		}
		name := t.Name()
		if _, dup := extra[name]; dup {
			continue
		}
		extra[name] = t
		names = append(names, name)
	}
	if len(extra) == 0 {
		return base
	}
	return &userToolOverlay{ToolExecutor: base, extra: extra, names: names}
}

// Get resolves per-user tools first, then falls back to the base executor.
func (o *userToolOverlay) Get(name string) (Tool, bool) {
	if t, ok := o.extra[name]; ok {
		return t, true
	}
	return o.ToolExecutor.Get(name)
}

// List returns base tool names plus any per-user names not already present in base.
func (o *userToolOverlay) List() []string {
	base := o.ToolExecutor.List()
	seen := make(map[string]bool, len(base))
	out := make([]string, 0, len(base)+len(o.names))
	for _, n := range base {
		seen[n] = true
		out = append(out, n)
	}
	for _, n := range o.names {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

// Compile-time check: the overlay satisfies ToolExecutor.
var _ ToolExecutor = (*userToolOverlay)(nil)
