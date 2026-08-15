package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// fakeSecretsStore is a minimal in-memory store.ConfigSecretsStore.
type fakeSecretsStore struct {
	values map[string]string
}

func newFakeSecretsStore() *fakeSecretsStore {
	return &fakeSecretsStore{values: map[string]string{}}
}

func (f *fakeSecretsStore) Get(_ context.Context, key string) (string, error) {
	return f.values[key], nil
}
func (f *fakeSecretsStore) Set(_ context.Context, key, value string) error {
	f.values[key] = value
	return nil
}
func (f *fakeSecretsStore) Delete(_ context.Context, key string) error {
	delete(f.values, key)
	return nil
}
func (f *fakeSecretsStore) GetAll(_ context.Context) (map[string]string, error) {
	return f.values, nil
}

const testVoiceCacheTTL = time.Minute

func TestVoicesList_NilCache(t *testing.T) {
	srv := newTestMCPServer()
	registerVoicesCRUDTools(srv, nil, nil)

	result := callTool(t, srv, "goclaw_voices_list", map[string]any{})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "voice cache not available")
}

func TestVoicesList_NoAPIKeyConfigured(t *testing.T) {
	cache := audio.NewVoiceCache(testVoiceCacheTTL, 10)
	secrets := newFakeSecretsStore()
	srv := newTestMCPServer()
	registerVoicesCRUDTools(srv, cache, secrets)

	result := callTool(t, srv, "goclaw_voices_list", map[string]any{})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "API key not found")
}

func TestVoicesList_UnsupportedProvider(t *testing.T) {
	cache := audio.NewVoiceCache(testVoiceCacheTTL, 10)
	secrets := newFakeSecretsStore()
	srv := newTestMCPServer()
	registerVoicesCRUDTools(srv, cache, secrets)

	result := callTool(t, srv, "goclaw_voices_list", map[string]any{"provider": "unsupported-tts"})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "unsupported voice provider")
}

func TestVoicesRefresh_NilCache(t *testing.T) {
	srv := newTestMCPServer()
	registerVoicesCRUDTools(srv, nil, nil)

	result := callTool(t, srv, "goclaw_voices_refresh", map[string]any{})
	assert.True(t, toolIsError(result))
	require.Contains(t, toolResultText(result), "voice cache not available")
}
