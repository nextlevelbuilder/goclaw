package media

import (
	"context"
	"errors"
	"io"
)

// Backend persists session-scoped media files. Implementations decide
// where the bytes live — local filesystem, S3, etc. — but must keep the
// same key shape: {sessionHash}/{id}.{ext}.
//
// LocalPath is intentionally part of the contract because most current
// callers want a filesystem path they can hand to ffmpeg, pypdf, or a
// container bind mount. Remote-only backends are expected to cache the
// object locally on first request and return the cache path.
type Backend interface {
	// Save persists the file at srcPath under sessionKey and returns the
	// generated media ID plus the extension that was applied. The source
	// file SHOULD be removed by the backend on success.
	Save(ctx context.Context, sessionKey, srcPath, mime string) (id string, ext string, err error)

	// Open returns a reader for the bytes of a previously-saved media ID.
	// The caller must close the reader.
	Open(ctx context.Context, id string) (io.ReadCloser, error)

	// LocalPath returns a filesystem path the caller can read directly.
	// Remote backends MAY block while fetching the object into a local
	// cache. Returns ErrNotFound if the ID is unknown.
	LocalPath(ctx context.Context, id string) (string, error)

	// Delete removes every media file persisted under sessionKey.
	Delete(ctx context.Context, sessionKey string) error
}

// ErrNotFound is returned by Backend implementations when a media ID has
// no corresponding object. Callers can match it with errors.Is.
var ErrNotFound = errors.New("media: not found")
