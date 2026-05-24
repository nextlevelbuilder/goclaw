package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type decisionConfigPermStore struct {
	allowed    bool
	err        error
	gotScope   string
	gotType    string
	gotUserID  string
	gotAgentID uuid.UUID
}

func (s *decisionConfigPermStore) CheckPermission(_ context.Context, agentID uuid.UUID, scope, configType, userID string) (bool, error) {
	s.gotAgentID = agentID
	s.gotScope = scope
	s.gotType = configType
	s.gotUserID = userID
	return s.allowed, s.err
}

func (s *decisionConfigPermStore) Grant(context.Context, *ConfigPermission) error { return nil }
func (s *decisionConfigPermStore) Revoke(context.Context, uuid.UUID, string, string, string) error {
	return nil
}
func (s *decisionConfigPermStore) List(context.Context, uuid.UUID, string, string) ([]ConfigPermission, error) {
	return nil, nil
}
func (s *decisionConfigPermStore) ListFileWriters(context.Context, uuid.UUID, string) ([]ConfigPermission, error) {
	return nil, nil
}

func TestValidConfigType(t *testing.T) {
	for _, configType := range []string{
		ConfigTypeFileWriter,
		ConfigTypeHeartbeat,
		ConfigTypeCron,
		ConfigTypeContextFiles,
		ConfigTypeWildcard,
	} {
		if !ValidConfigType(configType) {
			t.Fatalf("expected %q to be valid", configType)
		}
	}
	if ValidConfigType("workspace") {
		t.Fatal("unexpected valid config type")
	}
}

func TestValidConfigScope(t *testing.T) {
	for _, scope := range []string{
		"agent",
		"*",
		"group:*",
		"group:zalo:123",
		"group:telegram:-100",
		"guild:discord:456",
	} {
		if !ValidConfigScope(scope) {
			t.Fatalf("expected %q to be valid", scope)
		}
	}
	for _, scope := range []string{"", "dm:zalo:123", "workspace", "topic:telegram:1"} {
		if ValidConfigScope(scope) {
			t.Fatalf("expected %q to be invalid", scope)
		}
	}
}

func TestCheckConfigPermissionDecision(t *testing.T) {
	agentID := uuid.New()
	permStore := &decisionConfigPermStore{allowed: true}

	decision, err := CheckConfigPermissionDecision(
		context.Background(),
		permStore,
		agentID,
		"group:zalo:123",
		ConfigTypeContextFiles,
		"*",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allowed {
		t.Fatal("expected decision to allow")
	}
	if decision.Reason == "" {
		t.Fatal("expected reason")
	}
	if permStore.gotAgentID != agentID || permStore.gotScope != "group:zalo:123" || permStore.gotType != ConfigTypeContextFiles || permStore.gotUserID != "*" {
		t.Fatalf("unexpected check args: %#v", permStore)
	}
}

func TestCheckConfigPermissionDecisionReturnsStableDeniedShapeOnStoreError(t *testing.T) {
	agentID := uuid.New()
	permStore := &decisionConfigPermStore{err: errors.New("db down")}

	decision, err := CheckConfigPermissionDecision(
		context.Background(),
		permStore,
		agentID,
		"group:zalo:123",
		ConfigTypeFileWriter,
		"user-1",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if decision.Allowed {
		t.Fatal("store errors must not render as allowed")
	}
	if decision.Reason != "permission check failed" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}
