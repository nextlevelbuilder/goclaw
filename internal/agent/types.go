package agent

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Agent is the core abstraction for an AI agent execution loop.
// Implemented by *Loop; extracted as an interface for testability and composability.
type Agent interface {
	ID() string
	Run(ctx context.Context, req RunRequest) (*RunResult, error)
	IsRunning() bool
	Model() string
	ProviderName() string
	Provider() providers.Provider
}
