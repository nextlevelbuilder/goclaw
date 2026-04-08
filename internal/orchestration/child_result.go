package orchestration

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	plpkg "github.com/nextlevelbuilder/goclaw/internal/pipeline"
)

// ChildResult is a unified struct capturing the outcome of a child agent run,
// regardless of whether it came from v2 RunResult or v3 PipelineResult.
type ChildResult struct {
	Content      string
	Media        []bus.MediaFile
	InputTokens  int64
	OutputTokens int64
	Runtime      time.Duration
	Iterations   int
	Status       string // "completed", "failed", "cancelled"
}

// CaptureFromRunResult converts an agent.RunResult (v2) to ChildResult.
func CaptureFromRunResult(r *agent.RunResult, runtime time.Duration) ChildResult {
	if r == nil {
		return ChildResult{Status: "failed", Runtime: runtime}
	}
	var inTok, outTok int64
	if r.Usage != nil {
		inTok = int64(r.Usage.PromptTokens)
		outTok = int64(r.Usage.CompletionTokens)
	}
	return ChildResult{
		Content:      r.Content,
		Media:        MediaResultToBusFiles(r.Media),
		InputTokens:  inTok,
		OutputTokens: outTok,
		Runtime:      runtime,
		Iterations:   r.Iterations,
		Status:       "completed",
	}
}

// CaptureFromPipelineResult converts a pipeline.RunResult (v3) to ChildResult.
func CaptureFromPipelineResult(r *plpkg.RunResult, runtime time.Duration) ChildResult {
	if r == nil {
		return ChildResult{Status: "failed", Runtime: runtime}
	}
	media := make([]bus.MediaFile, 0, len(r.MediaResults))
	for _, m := range r.MediaResults {
		media = append(media, bus.MediaFile{Path: m.Path, MimeType: m.ContentType})
	}
	return ChildResult{
		Content:      r.Content,
		Media:        media,
		InputTokens:  int64(r.TotalUsage.PromptTokens),
		OutputTokens: int64(r.TotalUsage.CompletionTokens),
		Runtime:      runtime,
		Iterations:   r.Iterations,
		Status:       "completed",
	}
}
