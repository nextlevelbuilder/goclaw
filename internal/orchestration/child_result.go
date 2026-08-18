package orchestration

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// ChildResult is a unified struct capturing the outcome of a child agent run,
// regardless of the execution path that produced it.
type ChildResult struct {
	Content      string
	Media        []bus.MediaFile
	InputTokens  int64
	OutputTokens int64
	Runtime      time.Duration
	Iterations   int
	Status       string // "completed", "failed", "cancelled"
}