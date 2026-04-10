package vault

import (
	"sync"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// EnrichProgress tracks enrichment pipeline progress and broadcasts via WS events.
type EnrichProgress struct {
	mu       sync.Mutex
	msgBus   bus.EventPublisher
	tenantID uuid.UUID
	total    int
	done     int
	running  bool
}

// NewEnrichProgress creates a progress tracker that broadcasts to WS clients.
func NewEnrichProgress(msgBus bus.EventPublisher) *EnrichProgress {
	return &EnrichProgress{msgBus: msgBus}
}

// EnrichEvent is the WS event payload for vault enrichment progress.
type EnrichEvent struct {
	Phase   string `json:"phase"`   // enriching, complete
	Done    int    `json:"done"`    // docs completed so far
	Total   int    `json:"total"`   // total docs in pipeline
	Running bool   `json:"running"` // false when pipeline idle
}

// Status returns current progress state (for polling fallback / HTTP endpoint).
func (p *EnrichProgress) Status() EnrichEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	phase := "enriching"
	if !p.running {
		phase = "idle"
	}
	return EnrichEvent{Phase: phase, Done: p.done, Total: p.total, Running: p.running}
}

func (p *EnrichProgress) broadcast(e EnrichEvent) {
	if p.msgBus == nil {
		return
	}
	bus.BroadcastForTenant(p.msgBus, protocol.EventVaultEnrichProgress, p.tenantID, e)
}

// Start signals enrichment with updated total and tenant scope.
func (p *EnrichProgress) Start(total int, tenantID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = total
	p.tenantID = tenantID
	p.running = true
	p.broadcast(EnrichEvent{Phase: "enriching", Done: p.done, Total: p.total, Running: true})
}

// AddDone increments completed count by n and broadcasts progress.
func (p *EnrichProgress) AddDone(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	p.broadcast(EnrichEvent{Phase: "enriching", Done: p.done, Total: p.total, Running: true})
}

// Finish signals the enrichment pipeline has completed and resets counters.
func (p *EnrichProgress) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.broadcast(EnrichEvent{Phase: "complete", Done: p.done, Total: p.total, Running: false})
	p.done = 0
	p.total = 0
	p.running = false
}
