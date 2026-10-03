package http

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type systemConfigStoreStub struct {
	getValue string
	getErr   error
}

func (s systemConfigStoreStub) Get(context.Context, string) (string, error) {
	return s.getValue, s.getErr
}

func (systemConfigStoreStub) Set(context.Context, string, string) error {
	return nil
}

func (systemConfigStoreStub) Delete(context.Context, string) error {
	return nil
}

func (systemConfigStoreStub) List(context.Context) (map[string]string, error) {
	return nil, nil
}

func TestSystemConfigsHandleGetMissingKeyReturnsEmptyValue(t *testing.T) {
	h := NewSystemConfigsHandler(systemConfigStoreStub{getErr: store.ErrSystemConfigNotFound}, nil)
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/system-configs/alert.background.provider_error", nil)
	req.SetPathValue("key", "alert.background.provider_error")
	rec := httptest.NewRecorder()

	h.handleGet(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, stdhttp.StatusOK)
	}

	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["key"] != "alert.background.provider_error" || got["value"] != "" {
		t.Fatalf("response = %#v", got)
	}
}

func TestSystemConfigsHandleGetStoreErrorReturnsServerError(t *testing.T) {
	h := NewSystemConfigsHandler(systemConfigStoreStub{getErr: errors.New("system config get: tenant_id required")}, nil)
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/system-configs/background.provider", nil)
	req.SetPathValue("key", "background.provider")
	rec := httptest.NewRecorder()

	h.handleGet(rec, req)

	if rec.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, stdhttp.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "tenant_id required") {
		t.Fatalf("response body = %q", rec.Body.String())
	}
}
