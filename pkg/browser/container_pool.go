package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ContainerPoolEngine manages a pool of ContainerEngine instances, one per profile key.
// It implements Engine and routes NewPage calls to the correct container based on
// the profile directory in the context (set via WithProfileDir).
type ContainerPoolEngine struct {
	mu       sync.Mutex
	image    string
	maxPool  int
	engines  map[string]*poolEntry // profileKey → entry
	copts    []ContainerOpt        // passed to child ContainerEngines
	proxyURL string
	proxyMgr *ProxyManager // optional: assigns per-container proxy from pool
	tenantID string        // tenant for proxy assignment
	logger   *slog.Logger
	launched bool
}

// poolEntry tracks a single container in the pool.
type poolEntry struct {
	engine    *ContainerEngine
	lastUsed  time.Time
	ready     chan struct{} // closed when launch completes
	err       error        // set if launch failed
	hasProxy  bool         // true if container was launched with a proxy
	proxyUser string       // proxy auth username (for CDP Fetch auth)
	proxyPass string       // proxy auth password (decrypted, for CDP Fetch auth)
}

// NewContainerPoolEngine creates a pool engine that manages up to maxPool containers.
func NewContainerPoolEngine(image string, maxPool int, logger *slog.Logger, copts ...ContainerOpt) *ContainerPoolEngine {
	if logger == nil {
		logger = slog.Default()
	}
	if image == "" {
		image = DefaultContainerImage
	}
	if maxPool <= 0 {
		maxPool = 10
	}
	return &ContainerPoolEngine{
		image:   image,
		maxPool: maxPool,
		engines: make(map[string]*poolEntry),
		copts:   copts,
		logger:  logger,
	}
}

// Launch validates that Docker is available. No container is created yet —
// containers are created on demand in NewPage.
func (p *ContainerPoolEngine) Launch(opts LaunchOpts) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.launched {
		return nil
	}

	// Validate Docker is available
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker not available: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	p.logger.Info("container pool engine ready", "docker", strings.TrimSpace(string(out)), "image", p.image, "maxPool", p.maxPool)

	// Ensure image exists before any container is created.
	// For goclaw/* images this auto-builds on first run (~2-5 min).
	if err := ensureImage(p.image, p.logger); err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}

	p.proxyURL = opts.ProxyURL
	p.launched = true
	return nil
}

// NewPage routes the request to the correct container based on profileDir in context.
func (p *ContainerPoolEngine) NewPage(ctx context.Context, url string) (Page, error) {
	profileDir := profileDirFromCtx(ctx)
	key := profileDir
	if key == "" {
		key = "_shared"
	}

	useProxy, _ := useProxyFromCtx(ctx)
	p.logger.Info("container pool NewPage",
		"key", key, "useProxy", useProxy, "proxyMgrWired", p.proxyMgr != nil)
	entry, err := p.getOrCreate(key, profileDir, useProxy)
	if err != nil {
		return nil, fmt.Errorf("container pool: get engine for %q: %w", key, err)
	}
	// Check if the caller's context expired while waiting for container launch.
	// Container launch (waitForCDP) takes up to 60s, but the caller's action
	// timeout is typically 30s. The container stays in the pool for next request.
	if ctx.Err() != nil {
		return nil, fmt.Errorf("container pool: context expired while waiting for container launch (container is ready for next request): %w", ctx.Err())
	}
	// Inject proxy auth credentials into context for CDP Fetch-based auth
	if entry.proxyUser != "" {
		ctx = WithProxyAuthCreds(ctx, &ProxyAuthCreds{
			Username: entry.proxyUser,
			Password: entry.proxyPass,
		})
	}
	return entry.engine.NewPage(ctx, url)
}

// Pages aggregates pages from all running containers.
func (p *ContainerPoolEngine) Pages() ([]Page, error) {
	p.mu.Lock()
	entries := make([]*poolEntry, 0, len(p.engines))
	for _, e := range p.engines {
		entries = append(entries, e)
	}
	p.mu.Unlock()

	var all []Page
	for _, e := range entries {
		// Wait for ready
		<-e.ready
		if e.err != nil {
			continue
		}
		pages, err := e.engine.Pages()
		if err != nil {
			continue
		}
		all = append(all, pages...)
	}
	return all, nil
}

// Close shuts down all containers in the pool.
func (p *ContainerPoolEngine) Close() error {
	p.mu.Lock()
	entries := p.engines
	p.engines = make(map[string]*poolEntry)
	p.launched = false
	p.mu.Unlock()

	var firstErr error
	for key, e := range entries {
		<-e.ready // wait for launch to finish before closing
		if err := e.engine.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		p.logger.Info("pool container closed", "key", key)
	}
	return firstErr
}

// IsConnected returns true if the pool has been launched (Docker validated).
func (p *ContainerPoolEngine) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.launched
}

// Incognito returns a lightweight wrapper that delegates to the pool but has a
// no-op Close(). The Manager stores the result in tenantEngines and calls Close()
// during shutdown/reconnect — we must NOT close the entire pool in that path.
func (p *ContainerPoolEngine) Incognito() (Engine, error) {
	return &poolTenantView{pool: p}, nil
}

// poolTenantView is a thin wrapper around ContainerPoolEngine returned by Incognito().
// It delegates all operations to the pool but its Close() is a no-op, preventing
// Manager.closeTenantEnginesLocked() from destroying the shared container pool.
type poolTenantView struct {
	pool *ContainerPoolEngine
}

func (v *poolTenantView) Launch(opts LaunchOpts) error           { return v.pool.Launch(opts) }
func (v *poolTenantView) NewPage(ctx context.Context, url string) (Page, error) {
	return v.pool.NewPage(ctx, url)
}
func (v *poolTenantView) Pages() ([]Page, error)                 { return v.pool.Pages() }
func (v *poolTenantView) Close() error                           { return nil } // no-op: don't destroy the pool
func (v *poolTenantView) Incognito() (Engine, error)             { return v, nil }
func (v *poolTenantView) IsConnected() bool                      { return v.pool.IsConnected() }
func (v *poolTenantView) Name() string                           { return v.pool.Name() }

// Name returns the engine identifier.
func (p *ContainerPoolEngine) Name() string { return "container-pool" }

// getOrCreate returns an existing pool entry or creates a new container.
func (p *ContainerPoolEngine) getOrCreate(key, profileDir string, useProxy bool) (*poolEntry, error) {
	return p.getOrCreateRetry(key, profileDir, useProxy, 2)
}

func (p *ContainerPoolEngine) getOrCreateRetry(key, profileDir string, useProxy bool, retries int) (*poolEntry, error) {
	p.mu.Lock()

	// Check existing entry
	if entry, ok := p.engines[key]; ok {
		// Wait outside lock to see if it's ready
		p.mu.Unlock()
		<-entry.ready
		if entry.err != nil {
			if retries <= 0 {
				return nil, fmt.Errorf("container launch failed after retries: %w", entry.err)
			}
			// Previous launch failed — remove and retry
			p.mu.Lock()
			if p.engines[key] == entry {
				delete(p.engines, key)
			}
			p.mu.Unlock()
			return p.getOrCreateRetry(key, profileDir, useProxy, retries-1)
		}
		// Check if container is still alive
		if entry.engine.IsConnected() {
			// If agent wants proxy but container was launched without one,
			// evict and recreate with proxy. Chrome --proxy-server is launch-time only.
			if useProxy && !entry.hasProxy {
				p.logger.Info("container pool: evicting non-proxy container for proxy re-launch", "key", key)
				entry.engine.Close()
				p.mu.Lock()
				if p.engines[key] == entry {
					delete(p.engines, key)
				}
				p.mu.Unlock()
				return p.getOrCreateRetry(key, profileDir, useProxy, retries-1)
			}
			p.mu.Lock()
			entry.lastUsed = time.Now()
			p.mu.Unlock()
			return entry, nil
		}
		if retries <= 0 {
			return nil, fmt.Errorf("container dead and no retries left for key %q", key)
		}
		// Dead container — cleanup and recreate
		entry.engine.Close()
		p.mu.Lock()
		if p.engines[key] == entry {
			delete(p.engines, key)
		}
		p.mu.Unlock()
		return p.getOrCreateRetry(key, profileDir, useProxy, retries-1)
	}

	// Evict if at capacity
	if len(p.engines) >= p.maxPool {
		p.evictOldestLocked()
	}

	// Create new entry with ready channel
	entry := &poolEntry{
		engine:   NewContainerEngine(p.image, p.logger, p.copts...),
		lastUsed: time.Now(),
		ready:    make(chan struct{}),
	}
	p.engines[key] = entry
	proxyURL := p.proxyURL
	pm := p.proxyMgr
	tid := p.tenantID
	p.mu.Unlock()

	// Assign proxy from pool (sticky per-profile) if ProxyManager is available
	// and the agent has opted in via use_proxy config. Default is no proxy.
	var proxyName string
	var proxyUser, proxyPass string
	if pm != nil && useProxy {
		proxy, err := pm.AssignForProfile(context.Background(), tid, profileDir, "")
		if err == nil {
			fmtURL, user, pass, fmtErr := pm.FormatURLAndCreds(proxy)
			if fmtErr == nil {
				proxyURL = fmtURL
				proxyUser = user
				proxyPass = pass
				proxyName = proxy.Name
				p.logger.Info("proxy pool: assigned proxy for container",
					"key", key, "proxy", proxyName, "tenant", tid, "hasAuth", user != "")
			} else {
				p.logger.Warn("proxy pool: format URL failed, falling back to static proxy", "error", fmtErr)
			}
		} else {
			p.logger.Warn("proxy pool: no healthy proxy available, falling back to static proxy",
				"error", err, "tenant", tid, "profileDir", profileDir)
		}
	} else if useProxy && pm == nil {
		p.logger.Warn("proxy pool: useProxy=true but ProxyManager not wired")
	}

	// Launch container (outside lock)
	err := entry.engine.Launch(LaunchOpts{
		ProfileDir: profileDir,
		ProxyURL:   proxyURL,
		ProxyUser:  proxyUser,
		ProxyPass:  proxyPass,
	})
	entry.hasProxy = proxyURL != ""
	entry.proxyUser = proxyUser
	entry.proxyPass = proxyPass
	entry.err = err
	close(entry.ready)

	if err != nil {
		// Remove failed entry
		p.mu.Lock()
		if p.engines[key] == entry {
			delete(p.engines, key)
		}
		p.mu.Unlock()
		return nil, err
	}

	logAttrs := []any{"key", key, "profileDir", profileDir, "pool_size", p.poolSize()}
	if proxyName != "" {
		logAttrs = append(logAttrs, "proxy", proxyName)
	}
	p.logger.Info("pool container created", logAttrs...)
	return entry, nil
}

// evictOldestLocked removes the least-recently-used container. Must be called with mu held.
func (p *ContainerPoolEngine) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	for key, entry := range p.engines {
		select {
		case <-entry.ready:
			// Only consider entries that are fully launched
			if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.lastUsed
			}
		default:
			// Still launching — skip
		}
	}
	if oldestKey == "" {
		return
	}
	entry := p.engines[oldestKey]
	delete(p.engines, oldestKey)

	// Close in background to avoid blocking under lock
	go func() {
		<-entry.ready
		entry.engine.Close()
		p.logger.Info("pool container evicted (LRU)", "key", oldestKey)
	}()
}

// poolSize returns the current number of entries (for logging).
func (p *ContainerPoolEngine) poolSize() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.engines)
}
