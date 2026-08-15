package backup

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	s3UploadPartSize    = 10 << 20 // 10 MB per part
	s3UploadConcurrency = 3
)

// S3Client wraps the AWS SDK v2 S3 client for backup operations.
type S3Client struct {
	client *s3.Client
	bucket string
	prefix string
}

// BackupEntry describes a backup object stored in S3.
type BackupEntry struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// NewS3Client creates an S3Client from the given config.
// Supports custom endpoints for S3-compatible services (MinIO, DO Spaces, R2).
func NewS3Client(cfg *S3Config) (*S3Client, error) {
	if err := ValidateS3Config(cfg); err != nil {
		return nil, err
	}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "backups/"
	}

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // required for MinIO and most S3-compatible services
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)
	return &S3Client{client: client, bucket: cfg.Bucket, prefix: prefix}, nil
}

// Upload streams reader to S3 at the given key (relative to configured prefix).
// Uses multipart upload manager for efficient handling of large files.
func (c *S3Client) Upload(ctx context.Context, key string, reader io.Reader, size int64) error {
	fullKey := c.fullKey(key)
	uploader := manager.NewUploader(c.client, func(u *manager.Uploader) {
		u.PartSize = s3UploadPartSize
		u.Concurrency = s3UploadConcurrency
	})

	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(fullKey),
		Body:          reader,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("s3 upload %q: %w", fullKey, err)
	}
	return nil
}

// Download streams the S3 object at key directly to the writer.
// Uses GetObject for true streaming — avoids buffering the entire object in memory.
func (c *S3Client) Download(ctx context.Context, key string, writer io.Writer) error {
	fullKey := c.fullKey(key)
	resp, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return fmt.Errorf("s3 download %q: %w", fullKey, err)
	}
	defer resp.Body.Close()

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("s3 stream %q: %w", fullKey, err)
	}
	return nil
}

// ListBackups returns all backup objects under the configured prefix, sorted newest first.
func (c *S3Client) ListBackups(ctx context.Context) ([]BackupEntry, error) {
	var entries []BackupEntry
	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(c.prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list objects: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			entry := BackupEntry{Key: *obj.Key}
			if obj.Size != nil {
				entry.Size = *obj.Size
			}
			if obj.LastModified != nil {
				entry.LastModified = *obj.LastModified
			}
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastModified.After(entries[j].LastModified)
	})
	return entries, nil
}

// Delete removes a single object from S3.
func (c *S3Client) Delete(ctx context.Context, key string) error {
	fullKey := c.fullKey(key)
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return fmt.Errorf("s3 delete %q: %w", fullKey, err)
	}
	return nil
}

// TestConnection verifies bucket access via HeadBucket.
func (c *S3Client) TestConnection(ctx context.Context) error {
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		return fmt.Errorf("s3 connection test failed: %w", err)
	}
	return nil
}

// fullKey returns the full S3 key with prefix, avoiding double slashes.
func (c *S3Client) fullKey(key string) string {
	prefix := strings.TrimSuffix(c.prefix, "/")
	key = strings.TrimPrefix(key, "/")
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}
