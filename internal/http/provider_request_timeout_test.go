package http

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubProviderTimeoutStore returns a configured value for
// "providers.request_timeout_sec" and ignores everything else.
type stubProviderTimeoutStore struct {
	value string
}

func (s *stubProviderTimeoutStore) Get(_ context.Context, key string) (string, error) {
	if key == "providers.request_timeout_sec" {
		return s.value, nil
	}
	return "", nil
}
func (s *stubProviderTimeoutStore) Set(_ context.Context, _, _ string) error { return nil }
func (s *stubProviderTimeoutStore) Delete(_ context.Context, _ string) error { return nil }
func (s *stubProviderTimeoutStore) List(_ context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func TestLoadProviderRequestTimeoutSec(t *testing.T) {
	tests := []struct {
		name  string
		store *stubProviderTimeoutStore
		want  int
	}{
		{name: "unset falls back to default", store: &stubProviderTimeoutStore{value: ""}, want: defaultProviderRequestTimeoutSec},
		{name: "valid value parsed", store: &stubProviderTimeoutStore{value: "45"}, want: 45},
		{name: "invalid non-numeric falls back to default", store: &stubProviderTimeoutStore{value: "abc"}, want: defaultProviderRequestTimeoutSec},
		{name: "zero falls back to default", store: &stubProviderTimeoutStore{value: "0"}, want: defaultProviderRequestTimeoutSec},
		{name: "negative falls back to default", store: &stubProviderTimeoutStore{value: "-5"}, want: defaultProviderRequestTimeoutSec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadProviderRequestTimeoutSec(context.Background(), tt.store)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadProviderRequestTimeoutSec_NilStore(t *testing.T) {
	got := loadProviderRequestTimeoutSec(context.Background(), nil)
	assert.Equal(t, defaultProviderRequestTimeoutSec, got)
}
