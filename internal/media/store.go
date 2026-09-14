package media

import (
	"context"
	"strings"
)

// Store is a thin façade over a Backend that preserves the pre-refactor
// public API. External code keeps using *Store with the same method
// signatures; new code SHOULD depend on Backend directly so tests can
// substitute fakes.
type Store struct {
	b Backend
}

// NewStore creates a Store backed by a local filesystem at baseDir.
// Kept as the no-arg entry point so existing callers don't change.
func NewStore(baseDir string) (*Store, error) {
	b, err := NewFSBackend(baseDir)
	if err != nil {
		return nil, err
	}
	return &Store{b: b}, nil
}

// NewStoreWithBackend wraps an explicit Backend (S3, in-memory test fake, …).
func NewStoreWithBackend(b Backend) *Store { return &Store{b: b} }

// Backend exposes the underlying backend for callers that want to bypass
// the legacy façade and use the modern API directly (Open, context-aware
// methods). Keeps the door open for incremental migration without
// breaking the pre-refactor API.
func (s *Store) Backend() Backend { return s.b }

// SaveFile mirrors the pre-refactor signature: returns the media ID and
// the local filesystem path the caller can read immediately.
func (s *Store) SaveFile(sessionKey, srcPath, mime string) (id string, dstPath string, err error) {
	ctx := context.Background()
	id, _, err = s.b.Save(ctx, sessionKey, srcPath, mime)
	if err != nil {
		return "", "", err
	}
	dstPath, err = s.b.LocalPath(ctx, id)
	if err != nil {
		return "", "", err
	}
	return id, dstPath, nil
}

// LoadPath returns a local filesystem path for a saved media ID.
// Remote backends transparently cache the object before returning.
func (s *Store) LoadPath(id string) (string, error) {
	return s.b.LocalPath(context.Background(), id)
}

// DeleteSession removes every media file for a session key.
func (s *Store) DeleteSession(sessionKey string) error {
	return s.b.Delete(context.Background(), sessionKey)
}

// ExtFromMime returns a file extension (with dot) for a MIME type.
// Lives on Store rather than per-backend so both backends agree on the
// extension that ends up in the object key.
func ExtFromMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "video/mp4"):
		return ".mp4"
	case strings.HasPrefix(mime, "audio/ogg"), strings.HasPrefix(mime, "audio/opus"):
		return ".ogg"
	case strings.HasPrefix(mime, "audio/mpeg"):
		return ".mp3"
	case strings.HasPrefix(mime, "audio/wav"):
		return ".wav"
	case strings.HasPrefix(mime, "application/pdf"):
		return ".pdf"
	case strings.HasPrefix(mime, "application/zip"), strings.HasPrefix(mime, "application/x-zip-compressed"):
		return ".zip"
	case strings.HasPrefix(mime, "application/x-tar"):
		return ".tar"
	case strings.HasPrefix(mime, "application/gzip"), strings.HasPrefix(mime, "application/x-gzip"):
		return ".gz"
	case mime == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case mime == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	default:
		return ""
	}
}
