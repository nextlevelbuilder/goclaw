package store

import "context"

// ConfigSecretsStore manages encrypted config secrets.
// Used for non-LLM/non-channel secrets: gateway token, TTS keys, Brave API key, etc.
type ConfigSecretsStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
	GetAll(ctx context.Context) (map[string]string, error)
}
