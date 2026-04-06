package vault

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// VaultSyncWorker watches a workspace directory for file changes and keeps
// the vault document registry in sync with the filesystem.
type VaultSyncWorker struct {
	vaultStore store.VaultStore
	debounce   time.Duration
}

// NewVaultSyncWorker creates a new sync worker with 500ms debounce.
func NewVaultSyncWorker(vs store.VaultStore) *VaultSyncWorker {
	return &VaultSyncWorker{vaultStore: vs, debounce: 500 * time.Millisecond}
}

// Watch starts watching a workspace directory for changes. Blocks until ctx cancelled.
func (w *VaultSyncWorker) Watch(ctx context.Context, workspaceDir, tenantID, agentID string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(workspaceDir); err != nil {
		return err
	}

	var mu sync.Mutex
	pending := make(map[string]struct{})
	var timer *time.Timer

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
				continue
			}
			mu.Lock()
			pending[event.Name] = struct{}{}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounce, func() {
				mu.Lock()
				paths := make([]string, 0, len(pending))
				for p := range pending {
					paths = append(paths, p)
				}
				pending = make(map[string]struct{})
				mu.Unlock()

				for _, p := range paths {
					w.handleChange(ctx, p, workspaceDir, tenantID, agentID)
				}
			})
			mu.Unlock()
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Warn("vault.watcher_error", "err", err)
		}
	}
}

func (w *VaultSyncWorker) handleChange(ctx context.Context, path, workspaceDir, tenantID, agentID string) {
	relPath, err := filepath.Rel(workspaceDir, path)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return
	}
	relPath = filepath.ToSlash(relPath)

	fsHash, err := ContentHashFile(path)
	if err != nil {
		// File deleted or unreadable — mark with deleted metadata
		doc, getErr := w.vaultStore.GetDocument(ctx, tenantID, agentID, relPath)
		if getErr == nil && doc != nil {
			meta := doc.Metadata
			if meta == nil {
				meta = make(map[string]any)
			}
			meta["deleted"] = true
			doc.Metadata = meta
			_ = w.vaultStore.UpsertDocument(ctx, doc)
		}
		return
	}

	doc, err := w.vaultStore.GetDocument(ctx, tenantID, agentID, relPath)
	if err != nil {
		// Not registered — skip (will register on next agent write)
		return
	}
	if doc.ContentHash == fsHash {
		return // unchanged
	}
	if err := w.vaultStore.UpdateHash(ctx, tenantID, doc.ID, fsHash); err != nil {
		slog.Warn("vault.sync_hash_update", "path", relPath, "err", err)
	}
}
