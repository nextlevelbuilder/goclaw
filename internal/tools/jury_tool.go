package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/contract"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/orchestration"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// defaultJuryMatch is the approval threshold (2/3) used when the caller does
// not supply one. It mirrors the orchestration judge default.
const defaultJuryMatch = 0.66

// defaultJuryConcurrency bounds the fan-out worker pool when unset.
const defaultJuryConcurrency = 4

// JuryFunc scores and judges a completed fan-out round. It receives the
// contestants and their aligned results and returns the aggregated verdict.
// Injecting a custom judge lets a gateway supply an LLM-based rubric without
// coupling this package to providers.
type JuryFunc func(ctx context.Context, contestants []orchestration.Contestant, results []orchestration.ChildResult, opts orchestration.JudgeOpts) (contract.Verdict, error)

// JuryTool runs a competitive fan-out round: it parses a contract task, spawns
// N contenders through an injected DelegateRunFunc, judges the outcomes by
// scoring criteria, and persists the verdict as a durable contract record.
// It is a top-level execution surface for the multi-agent jury/competition
// contract type.
type JuryTool struct {
	delegateRunner DelegateRunFunc      // injected by the gateway; nil-safe
	contracts      store.ContractStore  // durable record persistence; nil-safe
	artifacts      store.ArtifactStore  // optional review artifact persistence; nil-safe
	judge          JuryFunc             // pluggable judge; defaults to a content heuristic
}

// NewJuryTool creates a jury tool. Stores are optional: when either store is
// nil the tool still runs the fan-out and returns the verdict, it simply skips
// persistence for that surface.
func NewJuryTool(delegateRunner DelegateRunFunc, contracts store.ContractStore, artifacts store.ArtifactStore) *JuryTool {
	return &JuryTool{
		delegateRunner: delegateRunner,
		contracts:      contracts,
		artifacts:      artifacts,
		judge:          judgeRound,
	}
}

// SetJudge replaces the default content-heuristic judge. Used by the gateway
// to plug an LLM-based rubric.
func (t *JuryTool) SetJudge(j JuryFunc) {
	if j != nil {
		t.judge = j
	}
}

// Name returns the tool name.
func (t *JuryTool) Name() string { return "jury" }

// Description explains the tool's purpose to the model.
func (t *JuryTool) Description() string {
	return "Run a competitive fan-out round: spawn multiple contender agents on " +
		"the same task via delegation, judge their outputs against scoring " +
		"criteria, and return the aggregate verdict (approve/revise/reject) " +
		"with the winning contender's output."
}

// Parameters declares the tool's JSON schema.
func (t *JuryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "The task each contender must solve",
			},
			"agent": map[string]any{
				"type":        "string",
				"description": "Delegation target agent_key applied to every contender",
			},
			"agents": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "One delegation target agent_key per strategy (overrides agent when lengths match)",
			},
			"strategies": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Labels for each contender, e.g. simplest, performance, safest. Defaults to [simplest, performance, safest]",
			},
			"criteria": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Scoring dimensions, e.g. correctness, performance. Repetition adds weight",
			},
			"concurrency": map[string]any{
				"type":        "integer",
				"description": "Max simultaneous contender runs (default 4)",
			},
			"match": map[string]any{
				"type":        "number",
				"description": "Approval threshold in (0,1]; default 0.66",
			},
		},
		"required": []string{"task"},
	}
}

// Execute parses args, runs the fan-out, judges, and persists the record.
func (t *JuryTool) Execute(ctx context.Context, args map[string]any) *Result {
	task, _ := args["task"].(string)
	if task == "" {
		return ErrorResult("jury: task is required")
	}
	if t.delegateRunner == nil {
		return ErrorResult("jury: delegate runner is not configured")
	}

	labels, err := parseStringList(args["strategies"], []string{"simplest", "performance", "safest"})
	if err != nil {
		return ErrorResult(err.Error())
	}
	criteria, err := parseStringList(args["criteria"], []string{"correctness"})
	if err != nil {
		return ErrorResult(err.Error())
	}
	concurrency := defaultJuryConcurrency
	if c, ok := args["concurrency"].(float64); ok && int(c) > 0 {
		concurrency = int(c)
	}
	match := defaultJuryMatch
	if m, ok := args["match"].(float64); ok && m > 0 && m <= 1 {
		match = m
	}

	// Resolve delegation targets: a shared agent, or a per-strategy list that
	// matches the strategy count. A missing target fails closed before fan-out.
	targets, err := juryDelegateTargets(args, len(labels))
	if err != nil {
		return ErrorResult(err.Error())
	}

	contestants := make([]orchestration.Contestant, len(labels))
	for i, label := range labels {
		contestants[i] = orchestration.Contestant{
			ID:    fmt.Sprintf("contender-%d", i),
			Task:  juryContestantTask(i, task),
			Label: label,
		}
	}

	// Capture the caller's authorization scope and channel routing once so both
	// are preserved on every contender request.
	baseReq := DelegateRequest{
		FromAgentID:   store.AgentIDFromContext(ctx),
		FromAgentKey:  store.AgentKeyFromContext(ctx),
		UserID:        store.UserIDFromContext(ctx),
		SenderID:      store.SenderIDFromContext(ctx),
		Role:          store.RoleFromContext(ctx),
		TenantID:      store.TenantIDFromContext(ctx).String(),
		Channel:       ToolChannelFromCtx(ctx),
		ChannelType:   ToolChannelTypeFromCtx(ctx),
		ChatID:        ToolChatIDFromCtx(ctx),
		PeerKind:      ToolPeerKindFromCtx(ctx),
		SessionKey:    ToolSessionKeyFromCtx(ctx),
		OriginTraceID: tracing.TraceIDFromContext(ctx),
	}
	// RunParallel hands each worker the contestant's Task (the index-marked
	// variant) so the runner can recover which contender it serves and select
	// the matching delegation target.
	runner := func(runCtx context.Context, ctask string) (orchestration.ChildResult, error) {
		index, delegateTask := parseJuryContestantTask(ctask)
		if index < 0 || index >= len(targets) {
			return orchestration.ChildResult{Status: "failed"}, fmt.Errorf("jury: unresolved contender %q", ctask)
		}
		req := baseReq
		req.ToAgentKey = targets[index]
		req.Task = delegateTask
		res, err := t.delegateRunner(runCtx, req)
		if err != nil {
			return orchestration.ChildResult{Status: "failed"}, err
		}
		return orchestration.ChildResult{Content: res.Content, Media: res.Media, Status: "completed"}, nil
	}

	results, runErr := orchestration.RunParallel(ctx, contestants, runner, orchestration.RunParallelOpts{Concurrency: concurrency})
	if runErr != nil {
		slog.Warn("jury.run_parallel_failed", "error", runErr)
		return ErrorResult(fmt.Sprintf("jury: fan-out failed: %v", runErr))
	}

	verdict, judgeErr := t.judge(ctx, contestants, results, orchestration.JudgeOpts{
		Criteria: criteria,
		Scoring:  defaultJuryScoring(),
		Match:    match,
	})
	if judgeErr != nil {
		return ErrorResult(fmt.Sprintf("jury: judge failed: %v", judgeErr))
	}

	slog.Info("jury.decision",
		"contender", verdict.ContenderID,
		"decision", verdict.Decision,
		"score", verdict.Score,
		"task_len", len(task),
	)

	body, err := t.persistRound(ctx, task, contestants, results, verdict)
	if err != nil {
		slog.Warn("jury.persist_failed", "error", err)
	}

	// Choose the winning contender's content for the model to consume.
	winnerContent := ""
	var winnerMedia []bus.MediaFile
	for i, c := range contestants {
		if c.ID == verdict.ContenderID && i < len(results) {
			winnerContent = results[i].Content
			winnerMedia = results[i].Media
			break
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"decision":       verdict.Decision,
		"contender_id":   verdict.ContenderID,
		"score":          verdict.Score,
		"reason":         verdict.Reason,
		"record_id":      body,
		"winning_output": winnerContent,
	})
	r := NewResult(string(payload))
	r.Media = winnerMedia
	return r
}

// judgeRound is the default judge: it scores completed contenders by a simple
// content heuristic. A failed or empty contender earns nothing; longer outputs
// score higher on "correctness" (more evidence), shorter outputs score higher
// on "simplicity". Each criterion uses a label-aware scorer so the same task
// can prefer different strategies.
func judgeRound(ctx context.Context, contestants []orchestration.Contestant, results []orchestration.ChildResult, opts orchestration.JudgeOpts) (contract.Verdict, error) {
	return orchestration.Judge(ctx, contestants, results, opts)
}

// defaultJuryScoring builds the default scorer set. "correctness" rewards
// substantive output; "simplest" rewards brevity; other labels fall back to
// the correctness scorer so the round still produces a score.
func defaultJuryScoring() map[string]orchestration.Scorer {
	correctness := func(res orchestration.ChildResult, label string) (float64, string) {
		n := len(strings.TrimSpace(res.Content))
		switch {
		case res.Status == "failed":
			return 0, "failed"
		case n == 0:
			return 0, "empty"
		case n < 64:
			return 0.4, "thin output"
		case n < 512:
			return 0.7, "adequate output"
		default:
			return 1.0, "substantive output"
		}
	}
	simplest := func(res orchestration.ChildResult, label string) (float64, string) {
		n := len(strings.TrimSpace(res.Content))
		switch {
		case res.Status == "failed":
			return 0, "failed"
		case n == 0:
			return 0, "empty"
		case n <= 256:
			return 1.0, "concise"
		case n <= 1024:
			return 0.6, "moderate length"
		default:
			return 0.3, "verbose"
		}
	}
	return map[string]orchestration.Scorer{
		"correctness": correctness,
		"simplest":    simplest,
		"performance": correctness,
		"safest":      correctness,
	}
}

// persistRound writes the competition record and, when the verdict is an
// approval and an artifact store is configured, a TypeReview artifact. The
// returned string is the record ID (empty when persistence is unavailable).
func (t *JuryTool) persistRound(ctx context.Context, task string, contestants []orchestration.Contestant, results []orchestration.ChildResult, verdict contract.Verdict) (string, error) {
	if t.contracts == nil {
		return "", nil
	}
	round := contract.Contract{
		Kind:        contract.ContractCompetition,
		Task:        task,
		AuthorAgent: store.AgentKeyFromContext(ctx),
		Verdicts:    []contract.Verdict{verdict},
	}
	// Contract.Verdicts carries json:"-" so the round embeds it explicitly to
	// keep the durable body self-contained for audit.
	body, err := json.Marshal(map[string]any{
		"contract": round,
		"verdicts": round.Verdicts,
	})
	if err != nil {
		return "", fmt.Errorf("marshal contract: %w", err)
	}
	rec := &store.ContractRecord{
		TenantID: store.TenantIDFromContext(ctx),
		Kind:     store.ContractRecordCompetition,
		Body:     string(body),
		Status:   store.ContractRecordClosed,
	}
	if err := t.contracts.CreateContractRecord(ctx, rec); err != nil {
		return "", err
	}
	if verdict.Decision == "approve" && t.artifacts != nil {
		t.persistReviewArtifact(ctx, task, contestants, results, verdict, rec)
	}
	return rec.ID.String(), nil
}

// persistReviewArtifact stores the jury outcome as a TypeReview artifact. The
// artifact embeds the verdict, the winning contender's output, and the losing
// outputs for auditability. Failures are logged, never fatal to the round.
func (t *JuryTool) persistReviewArtifact(ctx context.Context, task string, contestants []orchestration.Contestant, results []orchestration.ChildResult, verdict contract.Verdict, rec *store.ContractRecord) {
	content, err := json.Marshal(map[string]any{
		"task":          task,
		"verdict":       verdict,
		"record_id":     rec.ID,
		"contestants":   contestants,
		"child_results": results,
	})
	if err != nil {
		slog.Warn("jury.review_artifact_marshal_failed", "error", err)
		return
	}
	art := &store.Artifact{
		TenantID:    store.TenantIDFromContext(ctx),
		RunID:       rec.RunID,
		Type:        store.ArtifactTypeReview,
		Status:      store.ArtifactStatusFinal,
		Title:       "Jury verdict: " + task,
		Content:     string(content),
		AuthorAgent: store.AgentKeyFromContext(ctx),
	}
	if err := t.artifacts.CreateArtifact(ctx, art); err != nil {
		slog.Warn("jury.review_artifact_persist_failed", "error", err)
	}
}

// juryContestantTask wraps a task with a per-contender index so the runner can
// recover which contender a worker is serving. The marker uses a NUL separator
// which cannot appear in normal task text.
func juryContestantTask(index int, task string) string {
	return fmt.Sprintf("%d\x00%s", index, task)
}

// parseJuryContestantTask splits an index-marked contestant task back into its
// index and the real task. An absent marker yields (-1, task).
func parseJuryContestantTask(marked string) (int, string) {
	idx, rest, ok := strings.Cut(marked, "\x00")
	if !ok {
		return -1, marked
	}
	index := 0
	for _, r := range idx {
		if r < '0' || r > '9' {
			return -1, marked
		}
		index = index*10 + int(r-'0')
	}
	return index, rest
}

// juryDelegateTargets resolves the delegation target agent_key list for the
// round. A shared "agent" applies to every contender; a per-strategy "agents"
// list overrides it when its length matches the strategy count. An empty
// result fails closed.
func juryDelegateTargets(args map[string]any, strategyCount int) ([]string, error) {
	shared, _ := args["agent"].(string)
	perStrategy, err := parseStringList(args["agents"], nil)
	if err != nil {
		return nil, err
	}
	switch {
	case len(perStrategy) > 0:
		if len(perStrategy) != strategyCount {
			return nil, fmt.Errorf("jury: agents list (%d) must match strategy count (%d)", len(perStrategy), strategyCount)
		}
		return perStrategy, nil
	case shared != "":
		targets := make([]string, strategyCount)
		for i := range targets {
			targets[i] = shared
		}
		return targets, nil
	default:
		return nil, fmt.Errorf("jury: a delegation target agent is required (agent or agents)")
	}
}

// parseStringList reads a []any arg (from JSON tool args) or a []string arg
// and returns the strings, falling back to defaults when the arg is absent.
func parseStringList(raw any, defaults []string) ([]string, error) {
	if raw == nil {
		return append([]string(nil), defaults...), nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("item %d must be a string", i)
			}
			if strings.TrimSpace(s) == "" {
				continue
			}
			out = append(out, strings.TrimSpace(s))
		}
		if len(out) == 0 {
			return append([]string(nil), defaults...), nil
		}
		return out, nil
	case []string:
		if len(v) == 0 {
			return append([]string(nil), defaults...), nil
		}
		return v, nil
	default:
		return nil, fmt.Errorf("expected a list of strings")
	}
}
