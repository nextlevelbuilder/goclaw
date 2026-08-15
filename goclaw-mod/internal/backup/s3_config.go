package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// S3Config holds the configuration for S3 (or S3-compatible) backup storage.
type S3Config struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint,omitempty"` // MinIO, DO Spaces, R2, etc.
	Prefix          string `json:"prefix"`             // key prefix in bucket (default "backups/")
}

// Config secrets keys for S3 backup credentials.
const (
	S3KeyAccessKeyID     = "backup.s3.access_key_id"
	S3KeySecretAccessKey = "backup.s3.secret_access_key"
	S3KeyBucket          = "backup.s3.bucket"
	S3KeyRegion          = "backup.s3.region"
	S3KeyEndpoint        = "backup.s3.endpoint"
	S3KeyPrefix          = "backup.s3.prefix"
)

// LoadS3Config reads S3 credentials from the encrypted config_secrets store.
// Returns an error if any required field (access_key_id, secret_access_key, bucket) is missing.
// Returns (nil, nil) if no S3 config has been stored yet.
func LoadS3Config(ctx context.Context, secrets store.ConfigSecretsStore) (*S3Config, error) {
	get := func(key string) (string, bool, error) {
		val, err := secrets.Get(ctx, key)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", false, nil
			}
			return "", false, fmt.Errorf("get %q: %w", key, err)
		}
		return val, val != "", nil
	}

	accessKey, hasAK, err := get(S3KeyAccessKeyID)
	if err != nil {
		return nil, err
	}
	if !hasAK {
		return nil, nil // not configured
	}

	secretKey, _, err := get(S3KeySecretAccessKey)
	if err != nil {
		return nil, err
	}

	bucket, _, err := get(S3KeyBucket)
	if err != nil {
		return nil, err
	}

	region, _, err := get(S3KeyRegion)
	if err != nil {
		return nil, err
	}

	endpoint, _, err := get(S3KeyEndpoint)
	if err != nil {
		return nil, err
	}

	prefix, _, err := get(S3KeyPrefix)
	if err != nil {
		return nil, err
	}

	if region == "" {
		region = "us-east-1"
	}
	if prefix == "" {
		prefix = "backups/"
	}

	return &S3Config{
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		Bucket:          bucket,
		Region:          region,
		Endpoint:        endpoint,
		Prefix:          prefix,
	}, nil
}

// SaveS3Config writes all S3 config fields to the encrypted config_secrets store.
func SaveS3Config(ctx context.Context, secrets store.ConfigSecretsStore, cfg *S3Config) error {
	if err := ValidateS3Config(cfg); err != nil {
		return err
	}

	fields := map[string]string{
		S3KeyAccessKeyID:     cfg.AccessKeyID,
		S3KeySecretAccessKey: cfg.SecretAccessKey,
		S3KeyBucket:          cfg.Bucket,
		S3KeyRegion:          cfg.Region,
		S3KeyEndpoint:        cfg.Endpoint,
		S3KeyPrefix:          cfg.Prefix,
	}
	for key, val := range fields {
		if err := secrets.Set(ctx, key, val); err != nil {
			return fmt.Errorf("save %q: %w", key, err)
		}
	}
	return nil
}

// ValidateS3Config checks that all required fields are present.
func ValidateS3Config(cfg *S3Config) error {
	if cfg == nil {
		return errors.New("s3 config is nil")
	}
	if cfg.AccessKeyID == "" {
		return errors.New("access_key_id is required")
	}
	if cfg.SecretAccessKey == "" {
		return errors.New("secret_access_key is required")
	}
	if cfg.Bucket == "" {
		return errors.New("bucket is required")
	}
	return nil
}
