package tools

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// isMemoryDir checks if a path refers to the memory directory itself.
// Handles "memory", "./memory", "/workspace/memory" etc.
func isMemoryDir(path, workspace string) bool {
	clean := filepath.Clean(path)
	if clean == "memory" {
		return true
	}
	if workspace != "" && filepath.IsAbs(clean) {
		expected := filepath.Join(filepath.Clean(workspace), "memory")
		return clean == expected
	}
	return false
}

// isMemoryPath checks if a path refers to a memory file (MEMORY.md, memory.md, memory/*).
// Handles both relative and absolute paths (when workspace is provided).
func isMemoryPath(path, workspace string) bool {
	clean := filepath.Clean(path)
	base := filepath.Base(clean)

	// Root-level MEMORY.md or memory.md
	dir := filepath.Dir(clean)
	if (dir == "." || dir == "/" || dir == "") && (base == bootstrap.MemoryFile || base == bootstrap.MemoryAltFile) {
		return true
	}

	// Anything under memory/ directory (relative)
	if strings.HasPrefix(clean, "memory/") || strings.HasPrefix(clean, "memory\\") {
		return true
	}

	// Absolute path at workspace root or under workspace/memory/
	if workspace != "" && filepath.IsAbs(clean) {
		cleanWS := filepath.Clean(workspace)
		if filepath.Dir(clean) == cleanWS && (base == bootstrap.MemoryFile || base == bootstrap.MemoryAltFile) {
			return true
		}
		memDir := filepath.Join(cleanWS, "memory")
		if strings.HasPrefix(clean, memDir+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// KGExtractFunc is a callback invoked after a memory write to extract KG entities.
// agentID, userID, content are passed from the write context.
type KGExtractFunc func(ctx context.Context, agentID, userID, content string)

// MemoryInterceptor routes memory file reads/writes to the MemoryStore.
// Keeps MEMORY.md and memory/* in Postgres.
type MemoryInterceptor struct {
	memStore    store.MemoryStore
	workspace   string
	kgExtractFn KGExtractFunc
}

// NewMemoryInterceptor creates an interceptor backed by the given memory store.
func NewMemoryInterceptor(ms store.MemoryStore, workspace string) *MemoryInterceptor {
	return &MemoryInterceptor{memStore: ms, workspace: workspace}
}

// SetKGExtractFunc sets the callback for KG extraction after memory writes.
func (m *MemoryInterceptor) SetKGExtractFunc(fn KGExtractFunc) {
	m.kgExtractFn = fn
}

// ReadFile attempts to read a memory file from the DB.
// Returns (content, true, nil) if handled, or ("", false, nil) if not a memory path.
func (m *MemoryInterceptor) ReadFile(ctx context.Context, path string) (string, bool, error) {
	if !isMemoryPath(path, m.workspace) {
		return "", false, nil
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return "", false, nil // no agent context
	}

	// Normalize absolute path to workspace-relative for DB storage
	relPath := normalizeToRelative(path, m.workspace)

	userID := store.MemoryUserID(ctx)
	agentStr := agentID.String()

	// Try per-user first, then global
	content, err := m.memStore.GetDocument(ctx, agentStr, userID, relPath)
	if err != nil && userID != "" {
		content, err = m.memStore.GetDocument(ctx, agentStr, "", relPath)
	}
	if err != nil {
		// Not found is OK — return empty
		slog.Debug("memory interceptor: document not found", "path", path, "agent", agentStr)
		return "", true, nil
	}

	return content, true, nil
}

// MemoryWriteResult holds the outcome of a memory write operation.
type MemoryWriteResult struct {
	Handled     bool
	KGTriggered bool
}

// WriteFile attempts to write a memory file to the DB (+ re-index chunks for .md files).
// Non-.md files are stored but NOT indexed/chunked/embedded,
// matching TS behavior where only .md files are indexed.
// Returns MemoryWriteResult with Handled=true if this was a memory path, KGTriggered=true if KG extraction was started.
func (m *MemoryInterceptor) WriteFile(ctx context.Context, path, content string) (MemoryWriteResult, error) {
	if !isMemoryPath(path, m.workspace) {
		return MemoryWriteResult{}, nil
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return MemoryWriteResult{}, nil // no agent context
	}

	// Normalize absolute path to workspace-relative for DB storage
	relPath := normalizeToRelative(path, m.workspace)

	userID := store.MemoryUserID(ctx)
	agentStr := agentID.String()

	// Write document to DB
	if err := m.memStore.PutDocument(ctx, agentStr, userID, relPath, content); err != nil {
		return MemoryWriteResult{Handled: true}, err
	}

	// Only index .md files (chunk + embed). Non-.md files (JSON, etc.) are stored
	// as key-value documents but not searchable via memory_search.
	if strings.HasSuffix(relPath, ".md") {
		if err := m.memStore.IndexDocument(ctx, agentStr, userID, relPath); err != nil {
			slog.Warn("memory interceptor: index failed after write", "path", path, "error", err)
			// Non-fatal: document was saved, indexing will catch up
		}
	}

	// Trigger KG extraction in background if configured
	kgTriggered := false
	if m.kgExtractFn != nil && content != "" {
		go m.kgExtractFn(context.WithoutCancel(ctx), agentStr, userID, content)
		kgTriggered = true
	}

	return MemoryWriteResult{Handled: true, KGTriggered: kgTriggered}, nil
}

// ListFiles lists memory documents from the DB when path is the memory directory.
// Returns (listing, true, nil) if handled, or ("", false, nil) if not a memory path.
func (m *MemoryInterceptor) ListFiles(ctx context.Context, path string) (string, bool, error) {
	if !isMemoryDir(path, m.workspace) {
		return "", false, nil
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return "", false, nil
	}

	userID := store.MemoryUserID(ctx)
	docs, err := m.memStore.ListDocuments(ctx, agentID.String(), userID)
	if err != nil {
		return "", true, err
	}

	if len(docs) == 0 {
		return "", true, nil
	}

	var sb strings.Builder
	for _, doc := range docs {
		fmt.Fprintf(&sb, "[FILE] %s\n", doc.Path)
	}
	return sb.String(), true, nil
}
