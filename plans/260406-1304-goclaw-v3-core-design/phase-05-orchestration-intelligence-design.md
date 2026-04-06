# Phase 5: Orchestration & Intelligence Design

## Context Links
- Brainstorm: `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md` (sections 11, 15)
- Architecture report: `plans/reports/Explore-260406-thorough-v3-architecture.md` (section 8)
- Agent link store: `internal/store/agent_link_store.go`
- Current subagent: `internal/tools/subagent.go`, `internal/tools/subagent_exec.go`
- Current team tools: `internal/tools/team_tool.go`, `internal/tools/team_tool_backend.go`
- Event bus: `internal/bus/bus.go`, `internal/bus/types.go`
- Phase 4: `phase-04-system-integration-design.md` (WorkspaceContext, ToolCapability, PolicyEngine)

## Overview
- **Priority:** P1 (builds on Phase 4 integration layer)
- **Status:** Complete
- **Effort:** 6h design

Designs 4 interconnected systems:
1. **Delegate tool** — lightweight inter-agent delegation via agent_links
2. **Orchestration mode config** — layered: spawn always, delegate base, team optional
3. **Self-evolution** — progressive 3-stage rollout (metrics -> suggestions -> auto-adapt)
4. **EventBus integration** — orchestration + evolution events

---

## Key Insights

1. Agent Links infrastructure exists (`agent_link_store.go`) with `CanDelegate()`, `GetLinkBetween()`, `DelegateTargets()`, even `SearchDelegateTargetsByEmbedding()` — but NO tool uses it. Full plumbing ready, needs ~100 LOC tool wrapper.
2. Current orchestration: spawn (self-clone, ~300 LOC) + team_tasks (9,400 LOC, 18 actions). No middle ground. Simple delegation requires team overhead or spawn limitations.
3. Self-evolution is greenfield. No metrics infrastructure exists. Design must be additive — no existing behavior changes in Stage 1.
4. EventBus (`bus.MessageBus`) is channel-oriented (Inbound/Outbound messages). Broadcast events are typed but payloads are `any`. V3 needs structured event types for orchestration lifecycle.

---

## Requirements

### Functional
- F1: `delegate` tool — agent delegates task to linked agent, async or sync
- F2: Delegate permission checked via existing `agentLinkStore.CanDelegate()`
- F3: Delegatee uses own workspace + optional shared area + read-only exports
- F4: Orchestration modes configurable per tenant (default) + per agent (override)
- F5: System prompt only shows tools for active orchestration mode
- F6: Self-evolution Stage 1: record metrics (retrieval quality, tool effectiveness, response feedback)
- F7: Self-evolution Stage 2: analyze metrics, generate suggestions, admin review UI
- F8: Self-evolution Stage 3: auto-adapt with guardrails + rollback
- F9: All orchestration lifecycle events published via EventBus

### Non-Functional
- NF1: Delegate tool adds <5ms overhead over direct agent invocation
- NF2: Metrics recording non-blocking (goroutine, never delays response)
- NF3: Self-evolution stages independently toggleable (feature flags)
- NF4: Backward compatible: existing spawn + team_tasks unaffected

---

## Architecture

### A. Delegate Tool

#### Tool Interface

```go
// DelegateTool enables inter-agent task delegation via agent_links.
// Parameters:
//   agent_key (required): target agent key (from known delegate targets)
//   task (required): task description for the delegatee
//   mode (optional): "async" (default) or "sync"
//   context (optional): additional context string passed to delegatee
//   files (optional): list of file paths to export (read-only for delegatee)

type DelegateTool struct {
    agentLinkStore store.AgentLinkStore
    agentStore     store.AgentStore
    runner         AgentRunner // interface to invoke agent loop
    bus            bus.EventPublisher
}

func (t *DelegateTool) Name() string { return "delegate" }

func (t *DelegateTool) Metadata() ToolMetadata {
    return ToolMetadata{
        Name:         "delegate",
        Capabilities: []ToolCapability{CapAsync},
        Group:        "orchestration",
    }
}

func (t *DelegateTool) Parameters() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "agent_key": map[string]any{
                "type":        "string",
                "description": "Key of the agent to delegate to (from your known delegate targets)",
            },
            "task": map[string]any{
                "type":        "string",
                "description": "Task description for the delegatee",
            },
            "mode": map[string]any{
                "type":        "string",
                "enum":        []string{"async", "sync"},
                "default":     "async",
                "description": "async: fire-and-forget with result notification. sync: wait for completion.",
            },
            "context": map[string]any{
                "type":        "string",
                "description": "Additional context passed to delegatee's system prompt",
            },
            "files": map[string]any{
                "type":        "array",
                "items":       map[string]any{"type": "string"},
                "description": "File paths (relative to your workspace) to share read-only with delegatee",
            },
        },
        "required": []string{"agent_key", "task"},
    }
}
```

#### Execution Flow

```
delegate(agent_key="analyst", task="analyze Q1 data", mode="async")
  |
  v
1. Resolve target: agentStore.GetByKey(agent_key)
     -> fail if agent not found or inactive
  |
  v
2. Permission check: agentLinkStore.CanDelegate(fromAgentID, toAgentID)
     -> fail if no active link
  |
  v
3. Get link config: agentLinkStore.GetLinkBetween(from, to)
     -> read Settings JSON for workspace config
  |
  v
4. Build WorkspaceContext for delegatee:
     - ActivePath: delegatee's own workspace (resolved normally)
     - SharedPath: /data/delegates/{linkID}/shared (created if not exists)
     - ReadOnlyPaths: exported files from delegator (validated against workspace)
     - Scope: "delegate"
  |
  v
5. Build delegation request:
     - ExtraPrompt: "Delegated task from {source_agent_key}: {task}\n{context}"
     - WorkspaceContext: as built above
     - Mode: PromptMinimal (subagent-style, reduced sections)
  |
  v
6. Execute:
     async mode:
       - Spawn goroutine via scheduler lane
       - Return immediately: "Task delegated to {agent_key}. You'll be notified on completion."
       - On completion: bus.Broadcast(delegate.completed event)
       - Result delivered via message bus (same as spawn announce)
     sync mode:
       - Block until delegatee completes (with timeout from link.Settings)
       - Return delegatee's final response directly
  |
  v
7. Emit event: bus.Broadcast("delegate.sent", DelegateSentPayload{...})
```

#### Workspace Model for Delegation

```go
// DelegateWorkspaceConfig is stored in agent_link.settings JSON.
type DelegateWorkspaceConfig struct {
    // SharedPath: shared directory both agents can read/write.
    // Relative to data dir. Default: "delegates/{linkID}/shared"
    SharedPath    string   `json:"shared_path,omitempty"`

    // SourceExports: glob patterns of delegator files shared read-only.
    // Relative to delegator's workspace. E.g., ["docs/", "data/results/"]
    SourceExports []string `json:"source_exports,omitempty"`

    // SyncTimeout: max wait time for sync mode (seconds). Default: 300
    SyncTimeout   int      `json:"sync_timeout,omitempty"`

    // MaxConcurrent: max concurrent delegations on this link. 0 = unlimited.
    // Already exists as AgentLinkData.MaxConcurrent field.
}
```

No new DB tables. Config stored in existing `agent_links.settings` JSONB column.

#### AgentRunner Interface

```go
// AgentRunner abstracts invoking an agent's loop for delegation.
// Implemented by the gateway's run dispatcher.
type AgentRunner interface {
    // RunDelegation executes a delegation task and returns the result.
    // ctx carries workspace context, tenant scope, timeout.
    RunDelegation(ctx context.Context, req DelegationRequest) (*DelegationResult, error)
}

type DelegationRequest struct {
    TargetAgentID  uuid.UUID
    SourceAgentID  uuid.UUID
    LinkID         uuid.UUID
    Task           string
    Context        string
    WorkspaceCtx   *WorkspaceContext
    UserID         string // inherit from delegator's user context
    TenantID       uuid.UUID
    SyncTimeout    time.Duration // 0 = async
}

type DelegationResult struct {
    Response   string    // delegatee's final text response
    ToolCalls  int       // number of tool calls made
    Duration   time.Duration
    Error      error
}
```

---

### B. Orchestration Mode Configuration

#### Mode Definitions

```go
// OrchestrationMode controls which inter-agent tools are available.
type OrchestrationMode string

const (
    // ModeSpawn: self-clone only. spawn tool available.
    // Simplest mode. Agent can only create copies of itself for background work.
    ModeSpawn    OrchestrationMode = "spawn"

    // ModeDelegate: agent links + spawn. delegate tool available.
    // Agent can send tasks to linked agents. Base inter-agent mode.
    ModeDelegate OrchestrationMode = "delegate"

    // ModeTeam: full team tasks + delegate + spawn. team_tasks tool available.
    // Full lifecycle: review, approval, progress tracking.
    // Only available when agent is member of an active team.
    ModeTeam     OrchestrationMode = "team"
)
```

#### Configuration Layering

```go
// OrchestrationConfig controls orchestration behavior.
type OrchestrationConfig struct {
    // Mode: active orchestration mode.
    // Tenant sets default, agent overrides.
    Mode OrchestrationMode `json:"orchestration_mode"`
}
```

**Storage:** No new DB columns. Uses existing config mechanisms:
- Tenant default: `tenant_settings.orchestration_mode` (existing JSONB settings column on tenants table)
- Agent override: `agents.settings -> 'orchestration_mode'` (existing JSONB settings column)

**Resolution order:**
```
1. Agent settings.orchestration_mode (if set)
2. Tenant settings.orchestration_mode (if set)
3. Default: "spawn"
```

#### Tool Visibility by Mode

| Mode | spawn | delegate | team_tasks |
|------|-------|----------|------------|
| spawn | yes | no | no |
| delegate | yes | yes | no |
| team | yes | yes | yes |

**Implementation:** PolicyEngine checks orchestration mode during FilterTools:

```go
// In PolicyEngine.FilterTools(), after existing 7-step pipeline:
func (pe *PolicyEngine) applyOrchestrationMode(
    allowed []string,
    mode OrchestrationMode,
    isTeamMember bool,
) []string {
    switch mode {
    case ModeSpawn:
        allowed = subtractSet(allowed, []string{"delegate", "team_tasks"})
    case ModeDelegate:
        allowed = subtractSet(allowed, []string{"team_tasks"})
    case ModeTeam:
        if !isTeamMember {
            // Downgrade to delegate if not in team
            allowed = subtractSet(allowed, []string{"team_tasks"})
        }
    }
    return allowed
}
```

#### System Prompt Integration

PromptConfig (from Phase 4) carries orchestration info:

```go
// Added to PromptConfig:
type OrchestrationSectionData struct {
    Mode            OrchestrationMode
    DelegateTargets []DelegateTargetEntry // from agentLinkStore.DelegateTargets()
    TeamContext     *TeamSectionData      // only if ModeTeam + team member
}

type DelegateTargetEntry struct {
    AgentKey    string
    DisplayName string
    Description string
}
```

Template renders delegate targets when mode is delegate/team:

```
{{- define "orchestration" -}}
{{- if eq .Mode "delegate" "team" }}
# Delegation
You can delegate tasks to these agents:
{{- range .DelegateTargets }}
- **{{ .AgentKey }}**: {{ .Description }}
{{- end }}
Use `delegate(agent_key, task)` to send work.
{{- end }}
{{- end -}}
```

---

### C. Self-Evolution Design (Progressive)

#### Stage 1: Metrics Infrastructure (v3.0)

##### DB Schema

```sql
-- New table: agent_evolution_metrics
CREATE TABLE agent_evolution_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key TEXT NOT NULL,

    metric_type TEXT NOT NULL,  -- 'retrieval', 'tool', 'feedback'
    metric_key TEXT NOT NULL,   -- specific metric (e.g., tool name, retrieval query)
    value JSONB NOT NULL,       -- metric-specific payload

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX idx_evo_metrics_agent_type ON agent_evolution_metrics(agent_id, metric_type);
CREATE INDEX idx_evo_metrics_created ON agent_evolution_metrics(created_at);
CREATE INDEX idx_evo_metrics_tenant ON agent_evolution_metrics(tenant_id);

-- Partition-ready: created_at is in schema for future range partitioning
-- For now, TTL cleanup via cron: DELETE WHERE created_at < NOW() - INTERVAL '90 days'
```

##### Metric Types

```go
// MetricType constants
const (
    MetricRetrieval MetricType = "retrieval"
    MetricTool      MetricType = "tool"
    MetricFeedback  MetricType = "feedback"
)

// RetrievalMetricValue records per-retrieval quality.
type RetrievalMetricValue struct {
    Query       string  `json:"query"`
    ResultCount int     `json:"result_count"`
    TopScore    float64 `json:"top_score"`
    UsedInReply bool    `json:"used_in_reply"` // heuristic: result text appears in LLM output
    Source      string  `json:"source"`        // "memory_search", "kg_search", "auto_inject"
}

// ToolMetricValue records per-tool-call effectiveness.
type ToolMetricValue struct {
    ToolName    string        `json:"tool_name"`
    Success     bool          `json:"success"`     // !result.IsError
    Duration    time.Duration `json:"duration_ms"`
    ResultSize  int           `json:"result_size"`  // bytes
    Capability  string        `json:"capability"`   // from ToolMetadata
}

// FeedbackMetricValue records implicit satisfaction signal.
type FeedbackMetricValue struct {
    Signal       string `json:"signal"`        // "correction", "followup", "thanks", "new_topic"
    TurnGap      int    `json:"turn_gap"`      // turns between response and signal
    PreviousTool string `json:"previous_tool"` // last tool used before signal
}
```

##### Heuristic: "Was retrieval result used?"

```
After LLM response:
1. Collect all retrieval results from this turn (memory_search, auto-inject L0)
2. For each result, check if any 3+ word substring appears in LLM output
3. Mark used_in_reply = true if found
4. Record metric with used_in_reply flag
```

Simple substring check. Not perfect but sufficient for Stage 1 metrics. No LLM call needed.

##### Implicit Satisfaction Signals

```
After user's next message:
1. If message is a correction ("no", "that's wrong", "I meant..."): signal = "correction"
2. If message continues topic (similar embedding): signal = "followup"
3. If message changes topic entirely: signal = "new_topic"
4. If message is appreciation ("thanks", "perfect"): signal = "thanks"
5. Record feedback metric with previous turn's tool context
```

Classification via keyword matching + embedding similarity. No LLM call.

##### Metrics Store Interface

```go
// EvolutionMetricsStore manages self-evolution metrics.
type EvolutionMetricsStore interface {
    // RecordMetric stores a single metric (non-blocking, goroutine-safe).
    RecordMetric(ctx context.Context, metric EvolutionMetric) error

    // QueryMetrics returns metrics for analysis.
    QueryMetrics(ctx context.Context, agentID uuid.UUID, metricType MetricType, since time.Time, limit int) ([]EvolutionMetric, error)

    // AggregateToolMetrics returns per-tool success rate, avg duration, call count.
    AggregateToolMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]ToolAggregate, error)

    // AggregateRetrievalMetrics returns per-source usage rate.
    AggregateRetrievalMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]RetrievalAggregate, error)

    // Cleanup removes metrics older than TTL.
    Cleanup(ctx context.Context, olderThan time.Time) (int64, error)
}

type EvolutionMetric struct {
    ID         uuid.UUID
    TenantID   uuid.UUID
    AgentID    uuid.UUID
    SessionKey string
    MetricType MetricType
    MetricKey  string
    Value      json.RawMessage // serialized *MetricValue
    CreatedAt  time.Time
}

type ToolAggregate struct {
    ToolName    string
    CallCount   int
    SuccessRate float64
    AvgDuration time.Duration
}

type RetrievalAggregate struct {
    Source    string  // "memory_search", "kg_search", "auto_inject"
    QueryCount int
    UsageRate  float64 // fraction of results used in reply
    AvgScore   float64
}
```

##### Pipeline Integration

Metrics recorded in ObserveStage (Phase 2 pipeline):

```go
// In ObserveStage.Execute():
// After tool result processing:
if toolResult != nil {
    go metricsStore.RecordMetric(ctx, EvolutionMetric{
        MetricType: MetricTool,
        MetricKey:  toolResult.ToolName,
        Value:      marshalToolMetric(toolResult),
    })
}

// After retrieval in ContextStage:
if len(l0Results) > 0 {
    go metricsStore.RecordMetric(ctx, EvolutionMetric{
        MetricType: MetricRetrieval,
        MetricKey:  "auto_inject",
        Value:      marshalRetrievalMetric(query, l0Results),
    })
}
```

Always non-blocking (`go` goroutine). Metrics are best-effort — dropped on error, never block agent loop.

#### Stage 2: Suggestion Engine (v3.1)

##### DB Schema

```sql
-- New table: agent_evolution_suggestions
CREATE TABLE agent_evolution_suggestions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,

    suggestion_type TEXT NOT NULL,   -- 'threshold', 'tool_order', 'skill_add', 'memory_prune'
    suggestion TEXT NOT NULL,         -- human-readable suggestion
    rationale TEXT NOT NULL,          -- data-driven reasoning
    parameters JSONB,                 -- machine-readable params for auto-apply

    status TEXT NOT NULL DEFAULT 'pending',  -- 'pending', 'approved', 'rejected', 'applied', 'rolled_back'
    reviewed_by TEXT,                 -- admin user who reviewed
    reviewed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evo_suggestions_agent ON agent_evolution_suggestions(agent_id, status);
CREATE INDEX idx_evo_suggestions_tenant ON agent_evolution_suggestions(tenant_id);
```

##### Suggestion Types

```go
type SuggestionType string

const (
    SuggestThreshold  SuggestionType = "threshold"   // adjust retrieval threshold
    SuggestToolOrder  SuggestionType = "tool_order"   // reorder tool list for this agent
    SuggestSkillAdd   SuggestionType = "skill_add"    // recommend adding a skill
    SuggestMemoryPrune SuggestionType = "memory_prune" // prune low-value memory
)

// SuggestionParams is the machine-readable payload for auto-apply.
// Each SuggestionType has its own params structure.
type ThresholdParams struct {
    Param    string  `json:"param"`     // "retrieval_threshold", "l0_max_tokens"
    OldValue float64 `json:"old_value"`
    NewValue float64 `json:"new_value"`
}

type ToolOrderParams struct {
    ToolName   string `json:"tool_name"`
    Action     string `json:"action"`   // "promote", "demote", "suggest_removal"
    Reasoning  string `json:"reasoning"`
}
```

##### Analysis Rules (Examples)

```
Rule 1: Low retrieval usage
  IF retrieval.usage_rate < 0.2 over 7 days AND query_count > 50
  THEN suggest: "Raise relevance_threshold from {current} to {current+0.1}"
  REASON: "Only 20% of auto-injected memory results appear in responses"

Rule 2: Tool never succeeds
  IF tool.success_rate < 0.1 over 7 days AND call_count > 20
  THEN suggest: "Consider removing {tool_name} or fixing configuration"
  REASON: "{tool_name} failed 90% of {call_count} calls"

Rule 3: Repeated tool usage
  IF tool.call_count > 100/week for specific tool AND no skill exists
  THEN suggest: "Add skill for {common_task_pattern}"
  REASON: "{tool_name} called {count} times this week, mostly for {pattern}"

Rule 4: Memory bloat
  IF episodic_count > 500 AND avg_usage_rate < 0.05
  THEN suggest: "Prune episodic memories older than 60 days"
  REASON: "95% of older episodic summaries never retrieved"
```

##### Suggestion Store Interface

```go
type EvolutionSuggestionStore interface {
    CreateSuggestion(ctx context.Context, s EvolutionSuggestion) error
    ListSuggestions(ctx context.Context, agentID uuid.UUID, status string, limit int) ([]EvolutionSuggestion, error)
    UpdateSuggestionStatus(ctx context.Context, id uuid.UUID, status string, reviewedBy string) error
    GetSuggestion(ctx context.Context, id uuid.UUID) (*EvolutionSuggestion, error)
}

type EvolutionSuggestion struct {
    ID             uuid.UUID
    TenantID       uuid.UUID
    AgentID        uuid.UUID
    SuggestionType SuggestionType
    Suggestion     string
    Rationale      string
    Parameters     json.RawMessage
    Status         string
    ReviewedBy     string
    ReviewedAt     *time.Time
    CreatedAt      time.Time
}
```

##### Suggestion Engine Interface

```go
// SuggestionEngine analyzes metrics and generates suggestions.
// Runs as periodic cron job (configurable interval, default: daily).
type SuggestionEngine interface {
    // Analyze runs all rules for an agent and creates pending suggestions.
    Analyze(ctx context.Context, agentID uuid.UUID) ([]EvolutionSuggestion, error)

    // AnalyzeAll runs analysis for all agents in tenant.
    AnalyzeAll(ctx context.Context, tenantID uuid.UUID) error
}

// RuleSuggestionEngine implements SuggestionEngine with configurable rules.
type RuleSuggestionEngine struct {
    metricsStore    EvolutionMetricsStore
    suggestionStore EvolutionSuggestionStore
    rules           []AnalysisRule
}

// AnalysisRule is a single suggestion rule.
type AnalysisRule interface {
    // Evaluate checks if the rule fires for the given agent metrics.
    // Returns nil if rule doesn't apply.
    Evaluate(ctx context.Context, agentID uuid.UUID, metrics AnalysisInput) (*EvolutionSuggestion, error)
}

type AnalysisInput struct {
    ToolAggregates      []ToolAggregate
    RetrievalAggregates []RetrievalAggregate
    FeedbackSummary     FeedbackSummary
    AnalysisPeriod      time.Duration // lookback window (default 7 days)
}
```

#### Stage 3: Auto-Adaptation (v3.2+)

##### Guardrails

```go
// AdaptationGuardrails control auto-adaptation limits.
type AdaptationGuardrails struct {
    // MaxDeltaPerCycle: max parameter change per adaptation cycle.
    // E.g., threshold can only change by +/- 0.1 per cycle.
    MaxDeltaPerCycle float64 `json:"max_delta_per_cycle"`

    // MinDataPoints: minimum metrics before adaptation allowed.
    MinDataPoints    int     `json:"min_data_points"`

    // RollbackOnDrop: if quality metric drops by this %, rollback.
    RollbackOnDrop   float64 `json:"rollback_on_drop_pct"` // e.g., 20.0 = 20%

    // AdminOverride: admin can lock parameters (prevent auto-change).
    LockedParams     []string `json:"locked_params,omitempty"`
}

// Default guardrails
var DefaultGuardrails = AdaptationGuardrails{
    MaxDeltaPerCycle: 0.1,
    MinDataPoints:    100,
    RollbackOnDrop:   20.0,
}
```

##### Adaptation Flow

```
1. SuggestionEngine.Analyze() produces suggestion with status="pending"
2. If auto_adapt enabled for this agent:
   a. Check guardrails (min data points, max delta, locked params)
   b. If passes: apply parameters, set status="applied"
   c. Record baseline quality metric
3. After 7 days: compare quality metric to baseline
   a. If quality dropped > RollbackOnDrop%: rollback, set status="rolled_back"
   b. If quality stable/improved: keep, emit event
4. Admin can always: approve pending, reject, rollback applied, lock params
```

##### Auto-Adapt Store (Extension of Suggestion Store)

```go
// Added to EvolutionSuggestionStore for Stage 3:
type EvolutionAdaptStore interface {
    EvolutionSuggestionStore

    // ApplySuggestion applies a suggestion's parameters to agent config.
    ApplySuggestion(ctx context.Context, suggestionID uuid.UUID) error

    // RollbackSuggestion reverts an applied suggestion.
    RollbackSuggestion(ctx context.Context, suggestionID uuid.UUID) error

    // GetAdaptationHistory returns applied/rolled-back suggestions for audit.
    GetAdaptationHistory(ctx context.Context, agentID uuid.UUID, limit int) ([]EvolutionSuggestion, error)
}
```

##### Feature Flags

```go
// Feature flags in agent settings JSON (agents.settings column)
type SelfEvolutionFlags struct {
    MetricsEnabled     bool `json:"self_evolution_metrics"`     // Stage 1 (default: true for v3)
    SuggestionsEnabled bool `json:"self_evolution_suggestions"` // Stage 2 (default: false)
    AutoAdaptEnabled   bool `json:"self_evolution_auto_adapt"`  // Stage 3 (default: false)
}
```

No new DB columns. Flags stored in existing `agents.settings` JSONB.

---

### D. EventBus Integration

#### New Event Topics

```go
// Orchestration events
const (
    TopicDelegateSent      = "delegate:sent"
    TopicDelegateCompleted = "delegate:completed"
    TopicDelegateFailed    = "delegate:failed"
)

// Self-evolution events
const (
    TopicMetricRecorded       = "evolution:metric"
    TopicSuggestionCreated    = "evolution:suggestion"
    TopicAdaptationApplied    = "evolution:adapted"
    TopicAdaptationRolledBack = "evolution:rollback"
)
```

#### Typed Event Payloads

```go
// DelegateSentPayload emitted when delegate tool fires.
type DelegateSentPayload struct {
    SourceAgentID uuid.UUID `json:"source_agent_id"`
    TargetAgentID uuid.UUID `json:"target_agent_id"`
    LinkID        uuid.UUID `json:"link_id"`
    Task          string    `json:"task"`
    Mode          string    `json:"mode"` // "async" or "sync"
    SessionKey    string    `json:"session_key"`
}

// DelegateCompletedPayload emitted when delegatee finishes.
type DelegateCompletedPayload struct {
    SourceAgentID uuid.UUID     `json:"source_agent_id"`
    TargetAgentID uuid.UUID     `json:"target_agent_id"`
    LinkID        uuid.UUID     `json:"link_id"`
    Duration      time.Duration `json:"duration_ms"`
    ToolCalls     int           `json:"tool_calls"`
    Success       bool          `json:"success"`
    Error         string        `json:"error,omitempty"`
}

// DelegateFailedPayload emitted on delegation failure.
type DelegateFailedPayload struct {
    SourceAgentID uuid.UUID `json:"source_agent_id"`
    TargetAgentID uuid.UUID `json:"target_agent_id"`
    LinkID        uuid.UUID `json:"link_id"`
    Reason        string    `json:"reason"` // "permission_denied", "agent_inactive", "timeout"
}

// MetricRecordedPayload emitted on each metric write.
type MetricRecordedPayload struct {
    AgentID    uuid.UUID  `json:"agent_id"`
    MetricType MetricType `json:"metric_type"`
    MetricKey  string     `json:"metric_key"`
}

// SuggestionCreatedPayload emitted when suggestion engine generates one.
type SuggestionCreatedPayload struct {
    AgentID        uuid.UUID      `json:"agent_id"`
    SuggestionID   uuid.UUID      `json:"suggestion_id"`
    SuggestionType SuggestionType `json:"suggestion_type"`
    Summary        string         `json:"summary"`
}
```

#### Event Subscribers

```go
// Registered at gateway startup:

// 1. Delegation result delivery — notifies source agent when async delegation completes
bus.Subscribe("delegate-result-delivery", func(e bus.Event) {
    if e.Name == TopicDelegateCompleted {
        // Deliver result to source agent's session via sessions_send mechanism
    }
})

// 2. Metrics recording — non-blocking metric persistence
bus.Subscribe("evolution-metrics-recorder", func(e bus.Event) {
    if e.Name == TopicMetricRecorded {
        // Already persisted by goroutine in ObserveStage.
        // Subscriber only for UI real-time updates (WebSocket broadcast).
    }
})

// 3. Suggestion notification — notify admin of new suggestions
bus.Subscribe("evolution-suggestion-notify", func(e bus.Event) {
    if e.Name == TopicSuggestionCreated {
        // Broadcast to WebSocket for admin UI notification
    }
})
```

---

## Related Code Files

### Files to Modify
| File | Change |
|------|--------|
| `internal/bus/types.go` | Add orchestration + evolution topic constants + payload types |
| `internal/tools/policy.go` | Add applyOrchestrationMode() to FilterTools |
| `internal/agent/loop.go` | Wire metrics recording in observe phase |
| `internal/agent/systemprompt.go` | Add orchestration section to prompt config |
| `internal/store/agent_link_store.go` | (no change — already has full interface) |
| `cmd/gateway.go` | Register delegate tool, event subscribers, metrics cron |

### Files to Create
| File | Purpose |
|------|---------|
| `internal/tools/delegate_tool.go` | DelegateTool implementation |
| `internal/tools/delegate_workspace.go` | DelegateWorkspaceConfig, workspace setup |
| `internal/agent/orchestration_mode.go` | OrchestrationMode types, resolution logic |
| `internal/agent/agent_runner.go` | AgentRunner interface for delegation dispatch |
| `internal/store/evolution_store.go` | EvolutionMetricsStore + EvolutionSuggestionStore interfaces |
| `internal/store/pg/evolution_metrics.go` | PG implementation of EvolutionMetricsStore |
| `internal/store/pg/evolution_suggestions.go` | PG implementation of EvolutionSuggestionStore |
| `internal/agent/evolution_metrics.go` | Metric types + recording helpers |
| `internal/agent/evolution_suggestion_engine.go` | SuggestionEngine + AnalysisRule implementations |
| `internal/agent/evolution_guardrails.go` | AdaptationGuardrails + auto-adapt logic |

---

## Design Steps

1. Define DelegateTool interface + parameters schema
2. Design delegate execution flow (async + sync modes)
3. Define DelegateWorkspaceConfig in agent_link.settings
4. Define AgentRunner interface for delegation dispatch
5. Define OrchestrationMode enum + resolution logic (tenant -> agent)
6. Design tool visibility filtering by orchestration mode
7. Design orchestration section for system prompt template
8. Define agent_evolution_metrics schema + indexes
9. Define metric types (retrieval, tool, feedback) with value structs
10. Define EvolutionMetricsStore interface + aggregate methods
11. Design metrics recording integration in ObserveStage
12. Define agent_evolution_suggestions schema
13. Define SuggestionEngine + AnalysisRule interfaces
14. Design analysis rules (4 initial rules)
15. Define AdaptationGuardrails + auto-adapt flow
16. Define all EventBus topics + typed payloads
17. Design event subscriber wiring at startup

---

## Todo List

- [ ] A1: DelegateTool interface + parameters
- [ ] A2: Delegate execution flow (async/sync)
- [ ] A3: DelegateWorkspaceConfig schema
- [ ] A4: AgentRunner interface
- [ ] A5: Permission flow (CanDelegate + GetLinkBetween)
- [ ] B1: OrchestrationMode enum + constants
- [ ] B2: Mode resolution (tenant default + agent override)
- [ ] B3: Tool visibility by mode in PolicyEngine
- [ ] B4: Orchestration section in prompt template
- [ ] C1: agent_evolution_metrics DDL + indexes
- [ ] C2: Metric value types (retrieval, tool, feedback)
- [ ] C3: EvolutionMetricsStore interface
- [ ] C4: Retrieval "used in reply" heuristic
- [ ] C5: Implicit satisfaction signal classification
- [ ] C6: agent_evolution_suggestions DDL
- [ ] C7: SuggestionEngine + AnalysisRule interfaces
- [ ] C8: Initial 4 analysis rules
- [ ] C9: AdaptationGuardrails + auto-adapt flow
- [ ] C10: Feature flags in agent settings
- [ ] D1: EventBus topic constants
- [ ] D2: Typed event payloads (delegate, evolution)
- [ ] D3: Event subscriber design (result delivery, notifications)

---

## Success Criteria

1. DelegateTool ~100 LOC (excluding workspace setup), uses existing agentLinkStore.CanDelegate()
2. Orchestration mode resolvable from tenant + agent settings with no new DB columns
3. PolicyEngine correctly filters tools by mode (spawn/delegate/team)
4. agent_evolution_metrics table handles 10K+ rows/day without index degradation
5. Metrics recording never blocks agent loop (goroutine + best-effort)
6. SuggestionEngine rules are additive (new rule = implement AnalysisRule interface)
7. All orchestration events typed (no `map[string]any` payloads)
8. Feature flags independently toggleable per agent

---

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Delegate tool workspace isolation leaks | HIGH | ReadOnlyPaths enforced by same resolvePath() as all file tools. SharedPath created with restricted permissions. |
| Sync delegation timeout blocks caller | MEDIUM | Configurable timeout per link (default 300s). Timeout returns error, does not kill delegatee. |
| Metrics table grows unbounded | MEDIUM | TTL cleanup cron (default 90 days). Partitioning-ready schema (created_at). Monitor table size. |
| Suggestion engine generates noise | LOW | Minimum data points threshold (100). Suggestions require admin review (Stage 2). Auto-adapt has guardrails (Stage 3). |
| Orchestration mode config not discoverable | LOW | Admin UI shows current mode per agent. CLI: goclaw agent info shows mode. |
| Self-evolution "learns wrong" | HIGH | Progressive rollout. Stage 1 = metrics only (no behavior change). Stage 2 = human review. Stage 3 = guardrails + rollback. |
| EventBus topic explosion | LOW | Strict naming convention. Only add topics with subscribers. No orphan topics. |

---

## Security Considerations

- **Delegation permission:** `CanDelegate()` is the sole gate. No bypass via direct tool call — delegate tool always calls CanDelegate(). Link status "disabled" blocks delegation.
- **Cross-tenant delegation:** Agent links are tenant-scoped (both agents must be in same tenant). `agentLinkStore` queries filter by tenant_id from context.
- **Delegate workspace isolation:** Delegatee cannot access delegator's full workspace. Only explicitly exported paths (SourceExports) are shared read-only. SharedPath is a new directory, not inside either workspace.
- **Metrics data sensitivity:** Metrics contain query text and tool names, not user data or PII. Scoped by tenant_id. Cleanup cron prevents accumulation.
- **Auto-adapt scope:** Stage 3 only modifies retrieval parameters (thresholds, weights). Cannot modify tool access, workspace rules, or security settings. Admin can lock any parameter.
