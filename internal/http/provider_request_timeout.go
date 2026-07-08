package http

import (
	"context"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// defaultProviderRequestTimeoutSec is used when the tenant has not configured
// (or has configured invalid values for) providers.request_timeout_sec.
const defaultProviderRequestTimeoutSec = 30

// loadProviderRequestTimeoutSec reads the tenant's providers.request_timeout_sec
// system config. Returns defaultProviderRequestTimeoutSec (30) when unset/invalid.
func loadProviderRequestTimeoutSec(ctx context.Context, sc store.SystemConfigStore) int {
	if sc == nil {
		return defaultProviderRequestTimeoutSec
	}
	raw, err := sc.Get(ctx, "providers.request_timeout_sec")
	if err != nil || raw == "" {
		return defaultProviderRequestTimeoutSec
	}
	timeoutSec, err := strconv.Atoi(raw)
	if err != nil || timeoutSec <= 0 {
		return defaultProviderRequestTimeoutSec
	}
	return timeoutSec
}
