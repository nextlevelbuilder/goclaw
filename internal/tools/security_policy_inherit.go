package tools

// Security policy inheritance for spawned subagents.
//
// A subagent gets a cloned registry, but the file and exec tools inside it are
// constructed fresh (cmd/gateway_agents.go). A fresh tool carries none of the hardening
// the gateway applied to the parent's instances at startup — no path denials, no
// exemptions, no shell deny-group toggles, no command keyword allowlist. The result is
// that spawning a subagent widens what the agent can reach: the parent is blocked from
// the data dir and the internal databases, the subagent is not.
//
// These methods copy that policy across so the subagent starts no wider than its parent.
// Inheriting from the live parent instance keeps one source of truth — a deny path added
// at startup, or reloaded later via config pub/sub, reaches subagents without a second
// wiring site that can drift.
//
// Allow-prefixes are inherited too: they are what make the parent's deny roots usable at
// all (the skills store sits under the denied data dir), so copying denials without them
// would leave the subagent unable to read the skills it is told to use.

import "maps"

// InheritSecurityPolicy copies path denials, deny exemptions, global shell deny-group
// toggles and the command keyword allowlist from parent onto t.
func (t *ExecTool) InheritSecurityPolicy(parent *ExecTool) {
	if t == nil || parent == nil || t == parent {
		return
	}
	// DenyPaths rebuilds the compiled patterns from the raw roots, so pass the roots
	// rather than copying pathDenyPatterns — that keeps the slash-variant expansion and
	// the pathDenyRoots bookkeeping in one place.
	t.DenyPaths(parent.pathDenyRoots...)
	t.AllowPathExemptions(parent.denyExemptions...)

	parent.policyMu.RLock()
	groups := parent.globalDenyGroups
	rules := slicesClone(parent.commandKeywordAllowlist)
	parent.policyMu.RUnlock()

	if len(groups) > 0 {
		copied := make(map[string]bool, len(groups))
		maps.Copy(copied, groups)
		t.SetGlobalShellDenyGroups(copied)
	}
	if len(rules) > 0 {
		t.SetCommandKeywordAllowlist(rules)
	}
}

// InheritPathPolicy copies allowed and denied path prefixes from parent onto t.
func (t *ReadFileTool) InheritPathPolicy(parent *ReadFileTool) {
	if t == nil || parent == nil || t == parent {
		return
	}
	t.AllowPaths(parent.allowedPrefixes...)
	t.DenyPaths(parent.deniedPrefixes...)
}

// InheritPathPolicy copies allowed and denied path prefixes from parent onto t.
func (t *WriteFileTool) InheritPathPolicy(parent *WriteFileTool) {
	if t == nil || parent == nil || t == parent {
		return
	}
	t.AllowPaths(parent.allowedPrefixes...)
	t.DenyPaths(parent.deniedPrefixes...)
}

// InheritPathPolicy copies allowed and denied path prefixes from parent onto t.
func (t *ListFilesTool) InheritPathPolicy(parent *ListFilesTool) {
	if t == nil || parent == nil || t == parent {
		return
	}
	t.AllowPaths(parent.allowedPrefixes...)
	t.DenyPaths(parent.deniedPrefixes...)
}
