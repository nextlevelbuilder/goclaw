package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const requestSecretValue = "sk-request-secret-4321"

// requestSecretStore supports the save scope and the granted-meta lookup.
type requestSecretStore struct {
	store.SecretVaultStore
	entries map[string]*store.SecretVaultEntry
	granted []store.SecretVaultEntry
}

func (s *requestSecretStore) ListGrantedMeta(context.Context, uuid.UUID) ([]store.SecretVaultEntry, error) {
	return s.granted, nil
}

func (s *requestSecretStore) Create(_ context.Context, e *store.SecretVaultEntry, _ store.SecretVaultPayload) error {
	e.ID = uuid.New()
	s.entries[e.Name] = e
	return nil
}

func (s *requestSecretStore) ListGrants(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (s *requestSecretStore) SetGrants(context.Context, uuid.UUID, []uuid.UUID) error { return nil }

func (s *requestSecretStore) AppendAudit(context.Context, *store.SecretVaultAuditEntry) error {
	return nil
}

type requestSecretEnv struct {
	ctx    context.Context
	rt     *secretvault.Runtime
	fm     *secretvault.FormManager
	events chan map[string]any
	tenant uuid.UUID
	st     *requestSecretStore
}

func newRequestSecretEnv(channel string, withUI bool) *requestSecretEnv {
	e := &requestSecretEnv{
		fm: secretvault.NewFormManager(), events: make(chan map[string]any, 8), tenant: uuid.New(),
		st: &requestSecretStore{entries: map[string]*store.SecretVaultEntry{}},
	}
	e.fm.Configure(func(name string, _ uuid.UUID, payload map[string]any) {
		if name == "secretVault.form.requested" {
			e.events <- payload
		}
	}, func(uuid.UUID, string) bool { return withUI })
	e.rt = secretvault.NewRuntime(e.st, uuid.New(), "user1")
	e.rt.Forms, e.rt.SessionKey = e.fm, "sess1"
	ctx := store.WithUserID(store.WithTenantID(context.Background(), e.tenant), "user1")
	ctx = WithToolChannel(ctx, channel)
	e.ctx = secretvault.WithRuntime(ctx, e.rt)
	return e
}

func requestArgs() map[string]any {
	return map[string]any{"name": "gh-token", "kind": "api_key", "domains": []any{"api.github.com"}, "reason": "deploy"}
}

func TestRequestSecret_ReturnsPlaceholderNeverTheValue(t *testing.T) {
	e := newRequestSecretEnv("ws", true)
	done := make(chan *Result, 1)
	go func() { done <- NewRequestSecretTool().Execute(e.ctx, requestArgs()) }()

	var ev map[string]any
	select {
	case ev = <-e.events:
	case <-time.After(2 * time.Second):
		t.Fatal("no form event")
	}
	if ev["name"] != "gh-token" || ev["kind"] != "api_key" {
		t.Fatalf("event %+v", ev)
	}
	err := e.fm.Submit(e.ctx, e.tenant, "user1", ev["id"].(string), "sess1", secretvault.ScopeSave, store.SecretVaultPayload{Secret: requestSecretValue})
	if err != nil {
		t.Fatal(err)
	}
	res := <-done
	if res.IsError || !strings.Contains(res.ForLLM, "{{secret:gh-token}}") {
		t.Fatalf("result %+v", res)
	}
	if strings.Contains(res.ForLLM+res.ForUser+res.Deliverable, requestSecretValue) {
		t.Fatal("tool result leaks the value")
	}
	// The run's redactor learned the value before the tool returned.
	if got := e.rt.Redactor.Redact("echo " + requestSecretValue); strings.Contains(got, requestSecretValue) {
		t.Fatalf("redactor not updated: %q", got)
	}
}

func TestRequestSecret_NonWebChannelAndNoUIFailFast(t *testing.T) {
	for name, env := range map[string]*requestSecretEnv{
		"telegram": newRequestSecretEnv("telegram", true),
		"no ui":    newRequestSecretEnv("ws", false),
	} {
		start := time.Now()
		res := NewRequestSecretTool().Execute(env.ctx, requestArgs())
		if !res.IsError || time.Since(start) > time.Second {
			t.Errorf("%s: want immediate error, got %+v after %s", name, res, time.Since(start))
		}
	}
}

func TestRequestSecret_RejectsInvalidArgsAndMissingRuntime(t *testing.T) {
	e := newRequestSecretEnv("ws", true)
	bad := requestArgs()
	bad["name"] = "Bad Name!"
	if res := NewRequestSecretTool().Execute(e.ctx, bad); !res.IsError {
		t.Error("invalid name accepted")
	}
	bad = requestArgs()
	bad["domains"] = []any{}
	if res := NewRequestSecretTool().Execute(e.ctx, bad); !res.IsError {
		t.Error("missing domains accepted")
	}
	if res := NewRequestSecretTool().Execute(context.Background(), requestArgs()); !res.IsError {
		t.Error("no runtime must be an error")
	}
}

func TestRequestSecret_AlreadyGrantedSkipsTheForm(t *testing.T) {
	e := newRequestSecretEnv("ws", true)
	e.st.granted = []store.SecretVaultEntry{{Name: "gh-token"}}
	res := NewRequestSecretTool().Execute(e.ctx, requestArgs())
	if res.IsError || !strings.Contains(res.ForLLM, "{{secret:gh-token}}") {
		t.Fatalf("result %+v", res)
	}
	select {
	case <-e.events:
		t.Fatal("a form was opened for an already available secret")
	default:
	}
}
