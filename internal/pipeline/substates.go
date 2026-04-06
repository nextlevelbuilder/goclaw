package pipeline

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ContextState: owned by ContextStage, read by ThinkStage.
type ContextState struct {
	SkillsSummary  string
	HadBootstrap   bool
	OverheadTokens int // system prompt + context files (accurate via TokenCounter)
}

// ThinkState: owned by ThinkStage.
type ThinkState struct {
	LastResponse    *providers.ChatResponse
	TotalUsage      providers.Usage
	TruncRetries    int  // consecutive truncation retries (max 3)
	StreamingActive bool // true during active stream
}

// PruneState: owned by PruneStage.
type PruneState struct {
	MidLoopCompacted bool // true after first in-loop compaction
	HistoryTokens    int  // last computed history token count
	HistoryBudget    int  // contextWindow * maxHistoryShare
}

// ToolState: owned by ToolStage.
type ToolState struct {
	TotalToolCalls int
	AsyncToolCalls []string // tool names that executed async (spawn)
	LoopKilled     bool     // set when loop detector triggers critical
}

// ObserveState: owned by ObserveStage.
type ObserveState struct {
	FinalContent   string // accumulated response text
	FinalThinking  string // reasoning output
	BlockReplies   int
	LastBlockReply string
}

// CompactState: owned by CheckpointStage + MemoryFlushStage.
type CompactState struct {
	CheckpointFlushedMsgs  int
	MemoryFlushedThisCycle bool
	CompactionCount        int
}

// EvolutionState: owned by skill evolution nudge logic.
type EvolutionState struct {
	Nudge70Sent    bool
	Nudge90Sent    bool
	PostscriptSent bool
	BootstrapWrite bool // BOOTSTRAP.md write detected
}

// RunResult is the final output of a pipeline run.
type RunResult struct {
	Content       string
	Thinking      string
	TotalUsage    providers.Usage
	Iterations    int
	ToolCalls     int
	LoopKilled    bool
	Duration      time.Duration
	AsyncToolCalls []string
}
