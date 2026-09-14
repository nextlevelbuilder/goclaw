package media

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Config selects and parameterises the media Backend. Populate it
// programmatically or via LoadConfigFromEnv.
type Config struct {
	// Backend is "fs" (default) or "s3".
	Backend string

	// FS-only.
	BaseDir string

	// S3-only.
	Bucket   string
	Prefix   string
	Region   string
	Endpoint string // for S3-compatible services (MinIO, R2, DO Spaces)
	CacheDir string
}

// LoadConfigFromEnv pulls the standard GOCLAW_MEDIA_* variables out of
// the environment. Sensible defaults: fs backend, baseDir = defaultBase.
func LoadConfigFromEnv(defaultBase string) Config {
	cfg := Config{
		Backend:  strings.ToLower(strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_BACKEND"))),
		BaseDir:  strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_BASEDIR")),
		Bucket:   strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_S3_BUCKET")),
		Prefix:   strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_S3_PREFIX")),
		Region:   strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_S3_REGION")),
		Endpoint: strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_S3_ENDPOINT")),
		CacheDir: strings.TrimSpace(os.Getenv("GOCLAW_MEDIA_S3_CACHE_DIR")),
	}
	if cfg.Backend == "" {
		cfg.Backend = "fs"
	}
	if cfg.BaseDir == "" {
		cfg.BaseDir = defaultBase
	}
	return cfg
}

// NewBackend builds the Backend selected by cfg.
func NewBackend(ctx context.Context, cfg Config) (Backend, error) {
	switch cfg.Backend {
	case "", "fs":
		return NewFSBackend(cfg.BaseDir)
	case "s3":
		return NewS3Backend(ctx, cfg)
	default:
		return nil, fmt.Errorf("media: unknown backend %q (want fs|s3)", cfg.Backend)
	}
}

// NewStoreFromConfig is the convenience entry point for code that wants
// a *Store (the historical façade) instead of a bare Backend.
func NewStoreFromConfig(ctx context.Context, cfg Config) (*Store, error) {
	b, err := NewBackend(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return NewStoreWithBackend(b), nil
}
