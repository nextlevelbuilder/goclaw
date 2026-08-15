package tools

import (
	"context"
	"testing"
)

type fakeSystemConfigStore struct {
	data map[string]string
}

func (f *fakeSystemConfigStore) Get(_ context.Context, key string) (string, error) {
	return f.data[key], nil
}
func (f *fakeSystemConfigStore) Set(_ context.Context, key, value string) error {
	if f.data == nil {
		f.data = map[string]string{}
	}
	f.data[key] = value
	return nil
}
func (f *fakeSystemConfigStore) Delete(_ context.Context, key string) error {
	delete(f.data, key)
	return nil
}
func (f *fakeSystemConfigStore) List(_ context.Context) (map[string]string, error) {
	return f.data, nil
}

func TestResolveVoiceAndModel_SystemConfigsFallback(t *testing.T) {
	tool := NewTtsTool(nil)
	sc := &fakeSystemConfigStore{data: map[string]string{
		"tts.edge.voice": "vi-VN-HoaiMyNeural",
		"tts.edge.model": "edge-tts-1",
	}}
	tool.SetSystemConfigStore(sc)

	v, m, _ := tool.resolveVoiceAndModel(context.Background(), "edge", "", "")
	if v != "vi-VN-HoaiMyNeural" {
		t.Errorf("voice fallback failed: got %q, want vi-VN-HoaiMyNeural", v)
	}
	if m != "edge-tts-1" {
		t.Errorf("model fallback failed: got %q, want edge-tts-1", m)
	}
}

func TestResolveVoiceAndModel_ArgWinsOverSystemConfigs(t *testing.T) {
	tool := NewTtsTool(nil)
	tool.SetSystemConfigStore(&fakeSystemConfigStore{data: map[string]string{
		"tts.edge.voice": "vi-VN-HoaiMyNeural",
	}})

	v, _, _ := tool.resolveVoiceAndModel(context.Background(), "edge", "en-US-AriaNeural", "")
	if v != "en-US-AriaNeural" {
		t.Errorf("arg voice must win over system_configs: got %q", v)
	}
}

func TestResolveVoiceAndModel_NoStoreNoFallback(t *testing.T) {
	tool := NewTtsTool(nil)
	v, m, _ := tool.resolveVoiceAndModel(context.Background(), "edge", "", "")
	if v != "" || m != "" {
		t.Errorf("expected empty fallback when no system_configs wired, got voice=%q model=%q", v, m)
	}
}

func TestResolveVoiceAndModel_EmptyProviderSkipsFallback(t *testing.T) {
	tool := NewTtsTool(nil)
	tool.SetSystemConfigStore(&fakeSystemConfigStore{data: map[string]string{
		"tts..voice": "should-not-match",
	}})
	v, m, _ := tool.resolveVoiceAndModel(context.Background(), "", "", "")
	if v != "" || m != "" {
		t.Errorf("empty provider must skip lookup, got voice=%q model=%q", v, m)
	}
}
