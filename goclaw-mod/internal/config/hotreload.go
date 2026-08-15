package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ChangeHandler is called when the config file changes.
// It receives the newly loaded config.
type ChangeHandler func(cfg *Config)

// Watcher watches a config file for changes and reloads it.
// Changes are debounced (300ms) to avoid rapid reloads.
type Watcher struct {
	path       string
	watcher    *fsnotify.Watcher
	handlers   []ChangeHandler
	debounce   time.Duration
	stopChan   chan struct{}
	mu         sync.Mutex
}

// NewWatcher creates a config file watcher.
func NewWatcher(configPath string) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		path:     configPath,
		watcher:  w,
		debounce: 300 * time.Millisecond,
	}, nil
}

// OnChange registers a handler to be called when config changes.
func (cw *Watcher) OnChange(handler ChangeHandler) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.handlers = append(cw.handlers, handler)
}

// Start begins watching the config file for changes.
func (cw *Watcher) Start() error {
	if err := cw.watcher.Add(cw.path); err != nil {
		return err
	}

	cw.stopChan = make(chan struct{})
	go cw.watchLoop()

	slog.Info("config watcher started", "path", cw.path)
	return nil
}

// Stop halts the file watcher.
func (cw *Watcher) Stop() {
	if cw.stopChan != nil {
		close(cw.stopChan)
	}
	cw.watcher.Close()
	slog.Info("config watcher stopped")
}

func (cw *Watcher) watchLoop() {
	var debounceTimer *time.Timer

	for {
		select {
		case <-cw.stopChan:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}

			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}

			// Debounce: reset timer on each change
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(cw.debounce, func() {
				cw.reload()
			})

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", "error", err)
		}
	}
}

func (cw *Watcher) reload() {
	slog.Info("config file changed, reloading", "path", cw.path)

	cfg, err := Load(cw.path)
	if err != nil {
		slog.Error("config reload failed", "error", err)
		return
	}

	cw.mu.Lock()
	handlers := make([]ChangeHandler, len(cw.handlers))
	copy(handlers, cw.handlers)
	cw.mu.Unlock()

	for _, h := range handlers {
		h(cfg)
	}

	slog.Info("config reloaded successfully")
}
