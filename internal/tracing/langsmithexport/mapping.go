package langsmithexport

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	langsmith "github.com/langchain-ai/langsmith-go"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// mapRunType converts a GoClaw span type to a LangSmith run type.
func mapRunType(spanType string) string {
	switch spanType {
	case store.SpanTypeLLMCall:
		return "llm"
	case store.SpanTypeToolCall:
		return "tool"
	case store.SpanTypeAgent:
		return "chain"
	case store.SpanTypeEmbedding:
		return "llm"
	default:
		return "chain"
	}
}

// formatDottedOrder generates a LangSmith dotted-order string from a timestamp
// and UUID. This format is required for proper run tree hierarchy in the UI.
// Format: YYYYMMDDTHHMMSSffffffZ<uuid>
func formatDottedOrder(t time.Time, id uuid.UUID) string {
	return fmt.Sprintf("%s%06dZ%s",
		t.UTC().Format("20060102T150405"),
		t.UTC().Nanosecond()/1000,
		id.String(),
	)
}

// spanToRunCreate converts a GoClaw SpanData into a LangSmith RunCreate.
// parentDottedOrder is the parent's dotted_order for building child hierarchy;
// empty for root runs. parentLangsmithID remaps ParentSpanID when the parent's
// LangSmith run ID differs from its GoClaw span ID (root span remapping).
func spanToRunCreate(s store.SpanData, parentDottedOrder string, parentLangsmithID *uuid.UUID) *langsmith.RunCreate {
	// LangSmith requires root run_id == trace_id. For root spans (no parent),
	// use TraceID as the run ID so both constraints are satisfied.
	runID := s.ID
	if s.ParentSpanID == nil {
		runID = s.TraceID
	}

	// Build dotted_order: root = "<ts><runID>", child = "<parentDO>.<ts><runID>".
	segment := formatDottedOrder(s.StartTime, runID)
	dottedOrder := segment
	if parentDottedOrder != "" {
		dottedOrder = parentDottedOrder + "." + segment
	}

	rc := &langsmith.RunCreate{
		ID:          runID,
		TraceID:     s.TraceID,
		Name:        s.Name,
		RunType:     mapRunType(s.SpanType),
		StartTime:   s.StartTime,
		DottedOrder: dottedOrder,
	}

	if s.ParentSpanID != nil {
		if parentLangsmithID != nil {
			rc.ParentRunID = parentLangsmithID
		} else {
			rc.ParentRunID = s.ParentSpanID
		}
	}

	// Build inputs based on span type.
	rc.Inputs = buildInputs(s)

	// Build extra with invocation_params + metadata (matches langsmith-go convention).
	rc.Extra = buildExtra(s)

	// If span already has end_time (single-shot complete span), set outputs too.
	if s.EndTime != nil {
		rc.EndTime = *s.EndTime
		rc.Outputs = buildOutputs(s)
		if s.Status == store.SpanStatusError && s.Error != "" {
			rc.Error = s.Error
		}
	}

	return rc
}

// buildInputs constructs the Inputs field following LangSmith conventions.
// LLM runs use {"messages": [...]}, tool runs use {"tool_name": ..., "input": ...},
// chain/agent runs use {"input": ...}.
func buildInputs(s store.SpanData) map[string]any {
	inputs := make(map[string]any)
	switch s.SpanType {
	case store.SpanTypeLLMCall:
		if s.InputPreview != "" {
			inputs["messages"] = s.InputPreview
		}
	case store.SpanTypeToolCall:
		if s.ToolName != "" {
			inputs["tool_name"] = s.ToolName
		}
		if s.InputPreview != "" {
			inputs["input"] = s.InputPreview
		}
	default:
		if s.InputPreview != "" {
			inputs["input"] = s.InputPreview
		}
	}
	return inputs
}

// buildExtra constructs the Extra field following langsmith-go test conventions.
// Uses "invocation_params" for model config and "metadata" for additional context.
func buildExtra(s store.SpanData) map[string]any {
	extra := make(map[string]any)

	// Model invocation params (matches langsmith-go integration test pattern).
	if s.Model != "" || s.Provider != "" {
		params := make(map[string]any)
		if s.Model != "" {
			params["model"] = s.Model
		}
		if s.Provider != "" {
			params["provider"] = s.Provider
		}
		extra["invocation_params"] = params
	}

	// Span metadata forwarded as-is.
	if len(s.Metadata) > 0 {
		var meta map[string]any
		if err := json.Unmarshal(s.Metadata, &meta); err == nil {
			extra["metadata"] = meta
		}
	}

	return extra
}

// spanUpdateToRunUpdate converts a deferred span update into a LangSmith RunUpdate.
func spanUpdateToRunUpdate(u tracing.SpanUpdate) *langsmith.RunUpdate {
	ru := &langsmith.RunUpdate{
		ID:      u.SpanID,
		TraceID: u.TraceID,
	}

	outputs := make(map[string]any)
	usage := make(map[string]any)

	for k, v := range u.Updates {
		switch k {
		case "end_time":
			if t, ok := v.(time.Time); ok {
				ru.EndTime = t
			}
		case "status":
			// Mapped via error field below.
		case "error":
			if s, ok := v.(string); ok && s != "" {
				ru.Error = s
			}
		case "output_preview":
			if s, ok := v.(string); ok {
				outputs["content"] = s
			}
		case "input_tokens":
			if n, ok := toInt(v); ok {
				usage["prompt_tokens"] = n
			}
		case "output_tokens":
			if n, ok := toInt(v); ok {
				usage["completion_tokens"] = n
			}
		case "finish_reason":
			if s, ok := v.(string); ok {
				outputs["finish_reason"] = s
			}
		case "total_cost":
			if f, ok := toFloat(v); ok {
				outputs["total_cost"] = f
			}
		}
	}

	// Assemble usage following LangSmith convention (prompt_tokens/completion_tokens/total_tokens).
	if len(usage) > 0 {
		pt, _ := toInt(usage["prompt_tokens"])
		ct, _ := toInt(usage["completion_tokens"])
		usage["total_tokens"] = pt + ct
		outputs["usage"] = usage
	}

	if len(outputs) > 0 {
		ru.Outputs = outputs
	}

	return ru
}

// buildOutputs extracts outputs from a completed SpanData.
func buildOutputs(s store.SpanData) map[string]any {
	out := make(map[string]any)
	if s.OutputPreview != "" {
		out["content"] = s.OutputPreview
	}
	if s.InputTokens > 0 || s.OutputTokens > 0 {
		out["usage"] = map[string]int{
			"prompt_tokens":     s.InputTokens,
			"completion_tokens": s.OutputTokens,
			"total_tokens":      s.InputTokens + s.OutputTokens,
		}
	}
	if s.FinishReason != "" {
		out["finish_reason"] = s.FinishReason
	}
	if s.TotalCost != nil {
		out["total_cost"] = *s.TotalCost
	}
	return out
}

// toInt converts various numeric types to int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// toFloat converts various numeric types to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// applySpanUpdate applies a deferred span update to a buffered RunCreate,
// producing a complete single-shot POST. Used for child spans to avoid
// standalone PATCH operations (which lack parent_run_id and fail validation).
func applySpanUpdate(rc *langsmith.RunCreate, u tracing.SpanUpdate) {
	outputs := make(map[string]any)
	if rc.Outputs != nil {
		for k, v := range rc.Outputs {
			outputs[k] = v
		}
	}
	usage := make(map[string]any)

	for k, v := range u.Updates {
		switch k {
		case "end_time":
			if t, ok := v.(time.Time); ok {
				rc.EndTime = t
			}
		case "error":
			if s, ok := v.(string); ok && s != "" {
				rc.Error = s
			}
		case "output_preview":
			if s, ok := v.(string); ok {
				outputs["content"] = s
			}
		case "input_tokens":
			if n, ok := toInt(v); ok {
				usage["prompt_tokens"] = n
			}
		case "output_tokens":
			if n, ok := toInt(v); ok {
				usage["completion_tokens"] = n
			}
		case "finish_reason":
			if s, ok := v.(string); ok {
				outputs["finish_reason"] = s
			}
		case "total_cost":
			if f, ok := toFloat(v); ok {
				outputs["total_cost"] = f
			}
		}
	}

	if len(usage) > 0 {
		pt, _ := toInt(usage["prompt_tokens"])
		ct, _ := toInt(usage["completion_tokens"])
		usage["total_tokens"] = pt + ct
		outputs["usage"] = usage
	}
	if len(outputs) > 0 {
		rc.Outputs = outputs
	}
}

// isRunningSpan returns true if the span is in "running" state (start phase of two-phase).
func isRunningSpan(s store.SpanData) bool {
	return s.Status == store.SpanStatusRunning && s.EndTime == nil
}
