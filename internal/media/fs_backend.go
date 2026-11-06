package media

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// FSBackend stores media files on the local filesystem under
// {baseDir}/{sessionHash}/{id}.{ext}. It is the historical and default
// implementation of Backend; behaviour is preserved exactly from the
// pre-refactor media.Store so existing deployments see no change.
type FSBackend struct {
	baseDir string
}

// NewFSBackend creates an FSBackend rooted at baseDir. The directory is
// created if it doesn't exist.
func NewFSBackend(baseDir string) (*FSBackend, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("media fs: create base dir: %w", err)
	}
	return &FSBackend{baseDir: baseDir}, nil
}

func (b *FSBackend) Save(_ context.Context, sessionKey, srcPath, mime string) (string, string, error) {
	dir := b.sessionDir(sessionKey)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", fmt.Errorf("media fs: create session dir: %w", err)
	}

	mediaID := uuid.New().String()
	ext := ExtFromMime(mime)
	if ext == "" {
		ext = filepath.Ext(srcPath)
	}
	dstPath := filepath.Join(dir, mediaID+ext)

	// Same-filesystem rename is cheap; fall back to copy+remove across
	// devices (e.g. when the workspace volume differs from tmpfs).
	if err := os.Rename(srcPath, dstPath); err == nil {
		return mediaID, ext, nil
	}
	if err := copyFile(srcPath, dstPath); err != nil {
		return "", "", fmt.Errorf("media fs: copy file: %w", err)
	}
	_ = os.Remove(srcPath)
	return mediaID, ext, nil
}

func (b *FSBackend) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	p, err := b.LocalPath(ctx, id)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (b *FSBackend) LocalPath(_ context.Context, id string) (string, error) {
	// Media files are stored as {sessionHash}/{id}.{ext}. The session
	// hash is not part of the public ID, so we glob across sessions —
	// IDs are uuid.New so the chance of collision is negligible.
	matches, err := filepath.Glob(filepath.Join(b.baseDir, "*", id+".*"))
	if err != nil {
		return "", fmt.Errorf("media fs: glob for %s: %w", id, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return matches[0], nil
}

func (b *FSBackend) Delete(_ context.Context, sessionKey string) error {
	dir := b.sessionDir(sessionKey)
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("media fs: failed to delete session dir", "dir", dir, "error", err)
		return err
	}
	return nil
}

func (b *FSBackend) sessionDir(sessionKey string) string {
	h := sha256.Sum256([]byte(sessionKey))
	hash := fmt.Sprintf("%x", h[:6]) // 12 hex chars, filesystem-safe
	return filepath.Join(b.baseDir, hash)
}

// copyFile copies src to dst using buffered I/O.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// Ensure FSBackend satisfies Backend at compile time.
var _ Backend = (*FSBackend)(nil)
