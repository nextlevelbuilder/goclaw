package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
)

const (
	s3UploadPartSize    = 10 << 20 // 10 MB
	s3UploadConcurrency = 3
	s3ManifestSubprefix = "_manifests"
)

// S3Backend stores media objects in an S3 bucket under a configurable
// prefix. Object layout mirrors FSBackend:
//
//	{prefix}/{sessionHash}/{id}.{ext}     — payload
//	{prefix}/_manifests/{id}              — sessionHash/{id}.{ext} pointer
//
// Manifests let LocalPath / Open resolve a bare media ID into its full
// S3 key without scanning the whole bucket; they're tiny (a few dozen
// bytes) and cleaned up alongside the session on Delete.
type S3Backend struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
	prefix   string
	cacheDir string
}

// NewS3Backend builds an S3Backend from the supplied config. Credentials
// come from the default SDK chain (env vars, ~/.aws/credentials, IAM
// role on EC2/ECS/EKS) — there is intentionally no static-key field on
// Config; production deployments should use an instance profile.
func NewS3Backend(ctx context.Context, cfg Config) (*S3Backend, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("media s3: bucket is required")
	}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("media s3: load aws config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // MinIO / most S3-compatible services
		})
	}
	client := s3.NewFromConfig(awsCfg, s3Opts...)

	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "goclaw-media-cache")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("media s3: create cache dir: %w", err)
	}

	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = s3UploadPartSize
		u.Concurrency = s3UploadConcurrency
	})

	return &S3Backend{
		client:   client,
		uploader: uploader,
		bucket:   cfg.Bucket,
		prefix:   strings.TrimSuffix(cfg.Prefix, "/"),
		cacheDir: cacheDir,
	}, nil
}

func (b *S3Backend) Save(ctx context.Context, sessionKey, srcPath, mime string) (string, string, error) {
	mediaID := uuid.New().String()
	ext := ExtFromMime(mime)
	if ext == "" {
		ext = filepath.Ext(srcPath)
	}

	sessionHash := s3SessionHash(sessionKey)
	objectKey := b.objectKey(sessionHash, mediaID, ext)

	f, err := os.Open(srcPath)
	if err != nil {
		return "", "", fmt.Errorf("media s3: open src: %w", err)
	}
	defer f.Close()

	if _, err := b.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(b.bucket),
		Key:         aws.String(objectKey),
		Body:        f,
		ContentType: aws.String(mime),
	}); err != nil {
		return "", "", fmt.Errorf("media s3: upload %q: %w", objectKey, err)
	}

	// Manifest write makes the {id → object key} lookup O(1); without it
	// LocalPath would have to scan the whole bucket prefix.
	manifestKey := b.manifestKey(mediaID)
	manifestBody := sessionHash + "/" + mediaID + ext
	if _, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(b.bucket),
		Key:         aws.String(manifestKey),
		Body:        strings.NewReader(manifestBody),
		ContentType: aws.String("text/plain"),
	}); err != nil {
		// Best-effort cleanup: drop the just-uploaded object so we don't
		// leak orphans on a partial save.
		_, _ = b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(b.bucket),
			Key:    aws.String(objectKey),
		})
		return "", "", fmt.Errorf("media s3: write manifest %q: %w", manifestKey, err)
	}

	// Match FSBackend's "consume src" contract so callers don't have to
	// remember which backend they're talking to.
	_ = os.Remove(srcPath)
	return mediaID, ext, nil
}

func (b *S3Backend) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	relKey, err := b.resolveManifest(ctx, id)
	if err != nil {
		return nil, err
	}
	resp, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.qualify(relKey)),
	})
	if err != nil {
		if isS3NoSuchKey(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("media s3: get object %q: %w", relKey, err)
	}
	return resp.Body, nil
}

func (b *S3Backend) LocalPath(ctx context.Context, id string) (string, error) {
	relKey, err := b.resolveManifest(ctx, id)
	if err != nil {
		return "", err
	}
	cachePath := filepath.Join(b.cacheDir, filepath.FromSlash(relKey))
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", fmt.Errorf("media s3: create cache dir: %w", err)
	}

	resp, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.qualify(relKey)),
	})
	if err != nil {
		if isS3NoSuchKey(err) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return "", fmt.Errorf("media s3: get object %q: %w", relKey, err)
	}
	defer resp.Body.Close()

	// Write to a sibling temp file then rename, so a partial download
	// never gets surfaced as a valid cache entry.
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), "media-*.part")
	if err != nil {
		return "", fmt.Errorf("media s3: create cache tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("media s3: write cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("media s3: close cache tmp: %w", err)
	}
	if err := os.Rename(tmpName, cachePath); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("media s3: finalize cache: %w", err)
	}
	return cachePath, nil
}

func (b *S3Backend) Delete(ctx context.Context, sessionKey string) error {
	sessionHash := s3SessionHash(sessionKey)
	sessionPrefix := b.qualify(sessionHash + "/")

	// Collect every payload key in the session — we'll need the bare IDs
	// to wipe their manifests too.
	var payloadKeys []s3types.ObjectIdentifier
	var ids []string
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(sessionPrefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("media s3: list %q: %w", sessionPrefix, err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			payloadKeys = append(payloadKeys, s3types.ObjectIdentifier{Key: obj.Key})
			ids = append(ids, idFromKey(*obj.Key))
		}
	}

	if err := b.deleteKeys(ctx, payloadKeys); err != nil {
		return err
	}

	manifestKeys := make([]s3types.ObjectIdentifier, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		manifestKeys = append(manifestKeys, s3types.ObjectIdentifier{Key: aws.String(b.manifestKey(id))})
	}
	if err := b.deleteKeys(ctx, manifestKeys); err != nil {
		slog.Warn("media s3: manifest cleanup had failures (non-fatal)", "session_hash", sessionHash, "error", err)
	}

	return nil
}

func (b *S3Backend) deleteKeys(ctx context.Context, keys []s3types.ObjectIdentifier) error {
	for start := 0; start < len(keys); start += 1000 {
		end := start + 1000
		if end > len(keys) {
			end = len(keys)
		}
		_, err := b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(b.bucket),
			Delete: &s3types.Delete{Objects: keys[start:end], Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("media s3: delete batch: %w", err)
		}
	}
	return nil
}

func (b *S3Backend) resolveManifest(ctx context.Context, id string) (string, error) {
	resp, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(b.manifestKey(id)),
	})
	if err != nil {
		if isS3NoSuchKey(err) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return "", fmt.Errorf("media s3: read manifest %q: %w", id, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return "", fmt.Errorf("media s3: read manifest body %q: %w", id, err)
	}
	rel := strings.TrimSpace(string(body))
	if rel == "" || strings.Contains(rel, "..") {
		return "", fmt.Errorf("media s3: manifest %q has invalid body", id)
	}
	return rel, nil
}

func (b *S3Backend) objectKey(sessionHash, id, ext string) string {
	return b.qualify(sessionHash + "/" + id + ext)
}

func (b *S3Backend) manifestKey(id string) string {
	return b.qualify(s3ManifestSubprefix + "/" + id)
}

func (b *S3Backend) qualify(rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	if b.prefix == "" {
		return rel
	}
	return b.prefix + "/" + rel
}

// idFromKey reverses objectKey for a payload object — used during
// session delete to know which manifests to clean up.
func idFromKey(key string) string {
	base := filepath.Base(key)
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

func s3SessionHash(sessionKey string) string {
	h := sha256.Sum256([]byte(sessionKey))
	return fmt.Sprintf("%x", h[:6])
}

// isS3NoSuchKey matches the SDK's typed NoSuchKey / NotFound errors that
// callers translate into media.ErrNotFound.
func isS3NoSuchKey(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *s3types.NotFound
	return errors.As(err, &nf)
}

// Ensure S3Backend satisfies Backend at compile time.
var _ Backend = (*S3Backend)(nil)
