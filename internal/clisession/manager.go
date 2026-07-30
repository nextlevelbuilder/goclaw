package clisession

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ErrManagerStopped is returned when a session is requested after Shutdown. It is
// a distinct sentinel so a caller can tell "the gateway is going down" apart from
// "this CLI failed to start" and avoid retrying a doomed request.
var ErrManagerStopped = errors.New("clisession: manager is shut down")

const (
	// DefaultIdleTTL is how long a session may go without activity before it is
	// reaped. A CLI session pins a sandbox container and a running process, so an
	// abandoned conversation is real host cost — but a user who steps away mid-task
	// should not lose their agent's context either. 30 minutes is the compromise;
	// note that a long build or a pending approval both count as activity, so this
	// only expires conversations nobody is having.
	DefaultIdleTTL = 30 * time.Minute

	// DefaultReapInterval is how often the reaper looks. Sessions therefore live
	// up to IdleTTL+ReapInterval, which is fine — the TTL is a cost control, not a
	// deadline.
	DefaultReapInterval = 5 * time.Minute
)

// ManagerOpts configures a Manager. The zero value is valid and yields the
// defaults above.
type ManagerOpts struct {
	IdleTTL      time.Duration
	ReapInterval time.Duration
}

// Manager owns the live sessions, one per key (the chat session key), and reaps
// the idle ones.
//
// Structured after internal/sandbox.DockerManager, which solves the same problem
// for containers: a mutex-guarded map, creation under the lock so a concurrent
// request joins an in-progress creation instead of starting a second process, a
// ticker goroutine for pruning, and an explicit Stop/Shutdown for the gateway's
// shutdown path.
type Manager struct {
	opts ManagerOpts

	mu       sync.Mutex
	sessions map[string]*Session
	stopped  bool

	stopCh chan struct{}
}

// NewManager creates a manager and starts its idle reaper. The caller must call
// Shutdown to release the reaper goroutine and every live session.
func NewManager(opts ManagerOpts) *Manager {
	if opts.IdleTTL <= 0 {
		opts.IdleTTL = DefaultIdleTTL
	}
	if opts.ReapInterval <= 0 {
		opts.ReapInterval = DefaultReapInterval
	}
	m := &Manager{
		opts:     opts,
		sessions: make(map[string]*Session),
		stopCh:   make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

// GetOrCreate returns the live session for key, starting one if there is none.
//
// The map lock is held ACROSS creation on purpose: two messages arriving at once
// for the same chat must join one CLI process, not race into two — which would
// double the host cost and split the conversation's context between two agents
// that cannot see each other's work. newSession returns as soon as the process
// has started, so the critical section is short.
func (m *Manager) GetOrCreate(ctx context.Context, key string, opts SessionOpts) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return nil, ErrManagerStopped
	}

	if s, ok := m.sessions[key]; ok {
		if !s.isClosed() {
			return s, nil
		}
		// The CLI exited (finished, crashed, OOM-killed). Drop the corpse and start
		// fresh rather than handing back a session whose every Send fails.
		slog.Debug("clisession: replacing exited session", "key", key, "exit", s.ExitCode())
		delete(m.sessions, key)
		go func() { _ = s.Close() }() // reap outside the lock; Close is idempotent
	}

	// Detach from the CALLER's ctx. The session must outlive the request that
	// happened to create it — otherwise the first user turn's HTTP/WS handler
	// returning would kill the conversation. Its lifetime is instead bounded by
	// Close, Shutdown and the idle reaper.
	s, err := newSession(context.WithoutCancel(ctx), key, opts)
	if err != nil {
		return nil, err
	}
	m.sessions[key] = s
	return s, nil
}

// Get returns the live session for key without creating one.
func (m *Manager) Get(key string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok || s.isClosed() {
		return nil, false
	}
	return s, true
}

// Close ends the session for key. Unknown keys are not an error — closing an
// already-gone session is the caller's intent either way.
func (m *Manager) Close(key string) error {
	m.mu.Lock()
	s, ok := m.sessions[key]
	delete(m.sessions, key)
	m.mu.Unlock()

	if !ok {
		return nil
	}
	return s.Close()
}

// Shutdown stops the reaper and closes every session. Called on gateway stop.
// Idempotent; after it returns, GetOrCreate refuses new sessions.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.stopCh)
	live := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		live = append(live, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	// Closed outside the lock: each Close waits for its in-flight approvals to
	// unwind, and holding the map lock through that would block anything else
	// still trying to look a session up during shutdown.
	var wg sync.WaitGroup
	for _, s := range live {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			if err := s.Close(); err != nil {
				slog.Warn("clisession: session close failed during shutdown", "key", s.key, "error", err)
			}
		}(s)
	}
	wg.Wait()
	slog.Info("clisession manager shut down", "closed", len(live))
}

// Len reports how many sessions are tracked. For tests and diagnostics.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// reapLoop closes idle and exited sessions on a ticker.
func (m *Manager) reapLoop() {
	ticker := time.NewTicker(m.opts.ReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.reap(time.Now())
		}
	}
}

// reap closes every session that has been idle past the TTL, or whose process
// has already exited. Split out from the ticker so tests can drive it directly
// instead of waiting on wall-clock time.
func (m *Manager) reap(now time.Time) {
	cutoff := now.Add(-m.opts.IdleTTL)

	m.mu.Lock()
	var doomed []*Session
	for key, s := range m.sessions {
		switch {
		case s.isClosed():
			// Process already gone — untrack it so the next GetOrCreate starts clean.
			delete(m.sessions, key)
			doomed = append(doomed, s)
		case s.LastUsed().Before(cutoff):
			delete(m.sessions, key)
			doomed = append(doomed, s)
			slog.Info("clisession reaped idle session", "key", key, "idle_for", now.Sub(s.LastUsed()).Round(time.Second))
		}
	}
	m.mu.Unlock()

	for _, s := range doomed {
		if err := s.Close(); err != nil {
			slog.Warn("clisession: reap close failed", "key", s.key, "error", err)
		}
	}
}
