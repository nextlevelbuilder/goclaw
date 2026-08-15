// Package backends provides concrete Backend/Session/Stream implementations
// for the workstation package. Registered via init() so callers only need a
// blank import.
package backends

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"golang.org/x/crypto/ssh"
)

const (
	// maxClientsPerWorkstation is the hard cap on pooled *ssh.Client per workstation.
	maxClientsPerWorkstation = 4
	// poolQueueTimeout is the maximum wait time when pool is at capacity.
	poolQueueTimeout = 10 * time.Second
	// idleTTL defines how long an unreferenced client lives before eviction.
	idleTTL = 10 * time.Minute
	// pruneInterval is how often the background goroutine sweeps idle clients.
	pruneInterval = 60 * time.Second
	// circuitFailThreshold triggers lockout after this many consecutive auth failures.
	circuitFailThreshold = 3
	// circuitLockoutDuration is the lockout period after circuit opens.
	circuitLockoutDuration = 10 * time.Minute
)

// ErrPoolExhausted is returned when no client slot is available within poolQueueTimeout.
var ErrPoolExhausted = errors.New("ssh client pool exhausted: too many concurrent connections")

// ErrCircuitOpen is returned when the circuit breaker has tripped due to repeated auth failures.
var ErrCircuitOpen = errors.New("ssh auth circuit open: too many consecutive failures")

// pooledClient tracks a live *ssh.Client with reference counting and last-use timestamp.
type pooledClient struct {
	client  *ssh.Client
	refCnt  int
	lastUse time.Time
}

// circuitState tracks auth failure counts per workstation for circuit breaking.
type circuitState struct {
	failures int
	lockedAt time.Time
	isOpen   bool
}

// clientPool manages a set of *ssh.Client per workstation UUID.
type clientPool struct {
	mu       sync.Mutex
	clients  map[uuid.UUID][]*pooledClient
	circuits map[uuid.UUID]*circuitState
	// sem limits simultaneous dial operations to cap clients; value = available slots.
	sem    map[uuid.UUID]chan struct{}
	stopCh chan struct{}
	once   sync.Once
}

// newClientPool creates and starts a clientPool with background pruning.
func newClientPool() *clientPool {
	p := &clientPool{
		clients:  make(map[uuid.UUID][]*pooledClient),
		circuits: make(map[uuid.UUID]*circuitState),
		sem:      make(map[uuid.UUID]chan struct{}),
		stopCh:   make(chan struct{}),
	}
	go p.pruneLoop()
	return p
}

// semFor returns (and lazily creates) the semaphore channel for a workstation.
// Caller must hold p.mu.
func (p *clientPool) semFor(wsID uuid.UUID) chan struct{} {
	ch, ok := p.sem[wsID]
	if !ok {
		ch = make(chan struct{}, maxClientsPerWorkstation)
		for range maxClientsPerWorkstation {
			ch <- struct{}{}
		}
		p.sem[wsID] = ch
	}
	return ch
}

// Get borrows an *ssh.Client from the pool, dialing a new one if needed.
// Returns a release function that must be called when done.
func (p *clientPool) Get(
	ctx context.Context,
	ws *store.Workstation,
	meta *store.SSHMetadata,
	keyMaterial []byte,
) (*ssh.Client, func(), error) {
	p.mu.Lock()
	// Circuit breaker check.
	cs := p.circuitFor(ws.ID)
	if cs.isOpen {
		if time.Since(cs.lockedAt) < circuitLockoutDuration {
			p.mu.Unlock()
			return nil, nil, ErrCircuitOpen
		}
		// Lockout expired — reset and allow one retry.
		cs.isOpen = false
		cs.failures = 0
	}
	// Try to reuse an existing client with free capacity.
	for _, pc := range p.clients[ws.ID] {
		if pc.refCnt < maxClientsPerWorkstation {
			pc.refCnt++
			pc.lastUse = time.Now()
			client := pc.client
			p.mu.Unlock()
			release := func() { p.decRef(ws.ID, client) }
			return client, release, nil
		}
	}
	// Need a new client — acquire semaphore slot.
	sem := p.semFor(ws.ID)
	p.mu.Unlock()

	// Wait for a slot with timeout.
	select {
	case <-sem:
	case <-time.After(poolQueueTimeout):
		return nil, nil, ErrPoolExhausted
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	client, err := dialSSH(ctx, meta, keyMaterial)
	if err != nil {
		sem <- struct{}{} // return slot on dial failure
		p.recordAuthFailure(ws.ID, ws.WorkstationKey, err)
		return nil, nil, fmt.Errorf("ssh[%s]: dial: %w", ws.WorkstationKey, err)
	}

	p.mu.Lock()
	p.circuits[ws.ID] = &circuitState{} // reset on success
	pc := &pooledClient{client: client, refCnt: 1, lastUse: time.Now()}
	p.clients[ws.ID] = append(p.clients[ws.ID], pc)
	p.mu.Unlock()

	// I4 fix: wrap release in sync.Once so double-call (e.g. defer + explicit) is idempotent.
	// Without Once, a double-call would return an extra token to the semaphore, inflating
	// effective pool capacity beyond maxClientsPerWorkstation.
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			p.decRef(ws.ID, client)
			sem <- struct{}{} // return slot
		})
	}
	return client, release, nil
}

// decRef decrements the reference count for a client. Closes if refCnt reaches 0
// and the client has been idle beyond TTL.
func (p *clientPool) decRef(wsID uuid.UUID, client *ssh.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pc := range p.clients[wsID] {
		if pc.client == client {
			pc.refCnt--
			pc.lastUse = time.Now()
			return
		}
	}
}

// circuitFor returns (and lazily creates) the circuit state for a workstation.
// Caller must hold p.mu.
func (p *clientPool) circuitFor(wsID uuid.UUID) *circuitState {
	cs, ok := p.circuits[wsID]
	if !ok {
		cs = &circuitState{}
		p.circuits[wsID] = cs
	}
	return cs
}

// recordAuthFailure increments the failure counter and potentially opens the circuit.
func (p *clientPool) recordAuthFailure(wsID uuid.UUID, wsKey string, dialErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cs := p.circuitFor(wsID)
	cs.failures++
	if cs.failures >= circuitFailThreshold && !cs.isOpen {
		cs.isOpen = true
		cs.lockedAt = time.Now()
		slog.Warn("security.ssh_auth_circuit_open",
			"workstation_id", wsID,
			"workstation_key", wsKey,
			"failures", cs.failures,
			"lockout_minutes", circuitLockoutDuration.Minutes(),
			"err", dialErr,
		)
	}
}

// CloseWorkstation closes all pooled clients for the given workstation (e.g. on delete).
func (p *clientPool) CloseWorkstation(wsID uuid.UUID) {
	p.mu.Lock()
	clients := p.clients[wsID]
	delete(p.clients, wsID)
	delete(p.circuits, wsID)
	delete(p.sem, wsID)
	p.mu.Unlock()
	for _, pc := range clients {
		_ = pc.client.Close()
	}
}

// Close shuts down the pool and closes all managed clients.
func (p *clientPool) Close() {
	p.once.Do(func() { close(p.stopCh) })
	p.mu.Lock()
	all := p.clients
	p.clients = make(map[uuid.UUID][]*pooledClient)
	p.circuits = make(map[uuid.UUID]*circuitState)
	p.sem = make(map[uuid.UUID]chan struct{})
	p.mu.Unlock()
	for _, pcs := range all {
		for _, pc := range pcs {
			_ = pc.client.Close()
		}
	}
}

// pruneLoop evicts idle clients on a regular interval.
func (p *clientPool) pruneLoop() {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.prune()
		case <-p.stopCh:
			return
		}
	}
}

// prune closes clients that have zero references and have been idle beyond idleTTL.
func (p *clientPool) prune() {
	p.mu.Lock()
	for wsID, pcs := range p.clients {
		kept := pcs[:0]
		for _, pc := range pcs {
			if pc.refCnt == 0 && time.Since(pc.lastUse) > idleTTL {
				_ = pc.client.Close()
			} else {
				kept = append(kept, pc)
			}
		}
		if len(kept) == 0 {
			delete(p.clients, wsID)
			delete(p.circuits, wsID)
			delete(p.sem, wsID)
		} else {
			p.clients[wsID] = kept
		}
	}
	p.mu.Unlock()
}

// dialSSH, buildHostKeyCallback, buildAuthMethods live in ssh_dial.go.
