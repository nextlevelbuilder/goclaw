# CP-08: Plugin Ecosystem

**Patterns**: #14 (4-point Extension) + #15 (Security Sandbox) + #16 (Reconciliation Install)
**Priority**: MEDIUM-LOW (largest effort, highest long-term value)
**Dependencies**: CP-07 (plugin hooks extend skill concepts)
**Estimated effort**: 8-12 weeks
**Branch**: `feature/cp-08-plugin-ecosystem`

---

## Objective

Build a full plugin ecosystem for GoClaw with:
- 4 extension points (commands, agents, hooks, servers)
- 26 lifecycle hook events
- Reconciliation-based install (Kubernetes-style desired vs actual state)
- Security sandbox for third-party plugins

This is the largest checkpoint — break into sub-phases.

---

## Sub-phase A: Plugin Manifest + Registry (2 weeks)

### A.1 Plugin manifest format

**File**: `plugin.yaml` (in each plugin directory)

```yaml
name: feature-dev
version: 1.2.0
description: Feature development workflow with specialized agents
author: goclaw-community
license: MIT
min_goclaw_version: "0.12.0"
dependencies:
  - name: code-review
    version: ">=1.0.0"

commands:
  - name: feature-dev
    file: commands/feature-dev.md
    description: Start feature development workflow

agents:
  - name: code-architect
    file: agents/architect.md
    tools: ["read_file", "list_files", "exec", "web_search"]
    memory: project
    # CANNOT set: permissionMode, hooks, mcpServers (security restriction)

  - name: code-reviewer
    file: agents/reviewer.md
    tools: ["read_file", "exec", "memory_search"]

hooks:
  PostToolUse:
    - match_tool: write_file
      command: "golangci-lint run ${file_path}"
      timeout: 30s
  SessionEnd:
    - command: "echo 'Session ended' >> /tmp/goclaw-audit.log"

servers:
  mcp:
    - name: custom-tools
      command: "node ./mcp-server.js"
      args: ["--port", "3100"]
  lsp:
    - name: gopls
      command: "gopls"
      args: ["serve"]
```

### A.2 Create plugin package

```
internal/plugins/
  manifest.go       ← Parse plugin.yaml
  registry.go       ← Plugin registry (load, enable, disable)
  validator.go      ← Validate manifest + security checks
  errors.go         ← Plugin-specific error types
```

**File**: `internal/plugins/manifest.go`

```go
package plugins

type PluginManifest struct {
	Name             string            `yaml:"name"`
	Version          string            `yaml:"version"`
	Description      string            `yaml:"description"`
	Author           string            `yaml:"author"`
	License          string            `yaml:"license"`
	MinGoClawVersion string            `yaml:"min_goclaw_version"`
	Dependencies     []PluginDep       `yaml:"dependencies"`
	Commands         []PluginCommand   `yaml:"commands"`
	Agents           []PluginAgent     `yaml:"agents"`
	Hooks            map[string][]Hook `yaml:"hooks"`
	Servers          PluginServers     `yaml:"servers"`
}

type PluginDep struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"` // semver constraint
}

type PluginCommand struct {
	Name        string `yaml:"name"`
	File        string `yaml:"file"`
	Description string `yaml:"description"`
}

type PluginAgent struct {
	Name   string   `yaml:"name"`
	File   string   `yaml:"file"`
	Tools  []string `yaml:"tools"`
	Memory string   `yaml:"memory"` // "user", "project", "local"
}

type Hook struct {
	MatchTool string        `yaml:"match_tool"`
	Command   string        `yaml:"command"`
	Timeout   time.Duration `yaml:"timeout"`
}

type PluginServers struct {
	MCP []ServerDef `yaml:"mcp"`
	LSP []ServerDef `yaml:"lsp"`
}

type ServerDef struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}
```

### A.3 Plugin Registry

**File**: `internal/plugins/registry.go`

```go
type PluginState string
const (
	PluginInstalled PluginState = "installed"
	PluginEnabled   PluginState = "enabled"
	PluginDisabled  PluginState = "disabled"
	PluginError     PluginState = "error"
)

type Plugin struct {
	Manifest  PluginManifest
	State     PluginState
	Path      string // filesystem path
	Source    string // "local", "marketplace", "git"
	LoadedAt  time.Time
	Error     string
}

type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin // name → plugin
}

func (r *Registry) Register(p *Plugin) error { ... }
func (r *Registry) Enable(name string) error { ... }
func (r *Registry) Disable(name string) error { ... }
func (r *Registry) Get(name string) *Plugin { ... }
func (r *Registry) List() []*Plugin { ... }
func (r *Registry) Commands() []PluginCommand { ... }  // aggregated
func (r *Registry) Agents() []PluginAgent { ... }      // aggregated
func (r *Registry) HooksFor(event string) []Hook { ... } // aggregated
```

---

## Sub-phase B: Hook System (2 weeks)

### B.1 Define 26 hook events

**File**: `internal/plugins/hooks/events.go`

```go
package hooks

type Event string

const (
	// Tool lifecycle
	PreToolUse        Event = "PreToolUse"
	PostToolUse       Event = "PostToolUse"
	PostToolFailure   Event = "PostToolUseFailure"

	// Permission
	PermissionDenied  Event = "PermissionDenied"
	PermissionRequest Event = "PermissionRequest"

	// Session
	SessionStart      Event = "SessionStart"
	SessionEnd        Event = "SessionEnd"
	Setup             Event = "Setup"

	// Agent lifecycle
	SubagentStart     Event = "SubagentStart"
	SubagentStop      Event = "SubagentStop"
	TeammateIdle      Event = "TeammateIdle"

	// Task
	TaskCreated       Event = "TaskCreated"
	TaskCompleted     Event = "TaskCompleted"
	Stop              Event = "Stop"
	StopFailure       Event = "StopFailure"

	// Context
	PreCompact        Event = "PreCompact"
	PostCompact       Event = "PostCompact"
	Notification      Event = "Notification"

	// Filesystem
	FileChanged       Event = "FileChanged"
	CwdChanged        Event = "CwdChanged"
	WorktreeCreate    Event = "WorktreeCreate"
	WorktreeRemove    Event = "WorktreeRemove"

	// Config
	ConfigChange      Event = "ConfigChange"
	InstructionsLoaded Event = "InstructionsLoaded"
	UserPromptSubmit  Event = "UserPromptSubmit"

	// Channel
	ChannelMessage    Event = "ChannelMessage"
)
```

### B.2 Hook executor

**File**: `internal/plugins/hooks/executor.go`

```go
type HookResult struct {
	Action  string // "allow", "deny", "prevent", "" (no action)
	Message string // injected into conversation if non-empty
	Error   error
}

type Executor struct {
	registry *plugins.Registry
}

func (e *Executor) Fire(ctx context.Context, event Event, data map[string]any) []HookResult {
	hooks := e.registry.HooksFor(string(event))
	results := make([]HookResult, 0, len(hooks))

	for _, hook := range hooks {
		// Check match_tool filter
		if hook.MatchTool != "" {
			if toolName, _ := data["tool_name"].(string); toolName != hook.MatchTool {
				continue
			}
		}

		// Expand variables in command
		cmd := expandVariables(hook.Command, data)

		// Execute with timeout
		timeout := hook.Timeout
		if timeout == 0 { timeout = 10 * time.Second }

		result := executeHookCommand(ctx, cmd, timeout)
		results = append(results, result)

		// "prevent" stops further hooks AND the triggering action
		if result.Action == "prevent" {
			break
		}
	}

	return results
}
```

### B.3 Integrate into pipeline

**PreToolUse** — in ToolStage before executing each tool:
```go
results := hookExecutor.Fire(ctx, hooks.PreToolUse, map[string]any{
	"tool_name": tc.Name,
	"tool_args": tc.Args,
})
for _, r := range results {
	if r.Action == "deny" || r.Action == "prevent" {
		// Block tool execution
		return PermissionDeniedMessage(tc, r.Message)
	}
}
```

**PostToolUse** — after tool result:
```go
hookExecutor.Fire(ctx, hooks.PostToolUse, map[string]any{
	"tool_name": tc.Name,
	"tool_args": tc.Args,
	"result":    result.Content,
	"file_path": extractFilePath(tc.Args),
})
```

---

## Sub-phase C: Reconciliation Install (2 weeks)

### C.1 Reconciler

**File**: `internal/plugins/reconciler.go`

```go
type Reconciler struct {
	declared  func() []PluginDeclaration // from config.json
	installed func() map[string]*Plugin  // from disk
	registry  *Registry
}

type PluginDeclaration struct {
	Name    string `json:"name"`
	Source  string `json:"source"` // "local:/path", "git:url", "marketplace:name"
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

type ReconcileDiff struct {
	Missing       []PluginDeclaration // declared but not installed
	SourceChanged []PluginDeclaration // installed but source/version changed
	Removed       []string            // installed but not declared
	UpToDate      []string            // no changes needed
}

func (r *Reconciler) Diff() ReconcileDiff { ... }
func (r *Reconciler) Apply(ctx context.Context, diff ReconcileDiff) error { ... }
```

---

## Sub-phase D: Security Sandbox (1 week)

### D.1 Plugin agent restrictions

**File**: `internal/plugins/validator.go`

```go
func ValidatePluginAgent(agent PluginAgent, source string) error {
	if source != "local" {
		// Third-party plugins CANNOT escalate
		if agent.PermissionMode != "" {
			return fmt.Errorf("plugin agents cannot set permissionMode")
		}
		if len(agent.Hooks) > 0 {
			return fmt.Errorf("plugin agents cannot set per-agent hooks")
		}
		if len(agent.McpServers) > 0 {
			return fmt.Errorf("plugin agents cannot set per-agent MCP servers")
		}
	}
	return nil
}
```

---

## Sub-phase E: Config Integration (1 week)

### config.json additions

```json
{
  "plugins": {
    "declarations": [
      {"name": "feature-dev", "source": "local:./plugins/feature-dev", "enabled": true},
      {"name": "code-review", "source": "marketplace:code-review", "enabled": true}
    ],
    "marketplace_url": "https://plugins.goclaw.dev/v1",
    "blocked_sources": [],
    "strict_known_sources": false
  }
}
```

---

## Verification Checklist

### Manifest & Registry
- [ ] Plugin with valid plugin.yaml loads successfully
- [ ] Plugin with missing required fields → error
- [ ] Enable/disable toggles plugin availability
- [ ] Dependency check: missing dependency → error

### Hooks
- [ ] PreToolUse hook fires before tool execution
- [ ] PostToolUse hook fires after tool result
- [ ] Hook with `match_tool` only fires for matching tool
- [ ] Hook returning "prevent" blocks tool execution
- [ ] Hook timeout enforced (no hanging)
- [ ] Variables expanded in hook commands (${file_path}, etc.)

### Reconciliation
- [ ] Declared plugin not installed → auto-install
- [ ] Installed plugin not declared → unregister
- [ ] Source changed → update
- [ ] Background reconciliation, non-blocking startup

### Security
- [ ] Plugin agent cannot set permissionMode → error
- [ ] Plugin agent cannot set hooks → error
- [ ] Plugin agent cannot set mcpServers → error
- [ ] Local plugins have no restrictions
