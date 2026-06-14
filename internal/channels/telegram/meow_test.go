package telegram

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/meow"
)

// fakeSysCfg is an in-memory store.SystemConfigStore.
type fakeSysCfg struct{ m map[string]string }

func (f *fakeSysCfg) Get(_ context.Context, k string) (string, error) { return f.m[k], nil }
func (f *fakeSysCfg) Set(_ context.Context, k, v string) error        { f.m[k] = v; return nil }
func (f *fakeSysCfg) Delete(_ context.Context, k string) error        { delete(f.m, k); return nil }
func (f *fakeSysCfg) List(_ context.Context) (map[string]string, error) {
	return f.m, nil
}

func TestMeowOwnerAllowed_ClosedByDefault(t *testing.T) {
	ctx := context.Background()

	// No config store injected → /meow disabled, everyone denied.
	c := &Channel{}
	if c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("nil meowCfg must deny")
	}

	cs := &fakeSysCfg{m: map[string]string{}}
	c.meowCfg = cs

	// Empty config → denied.
	if c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("empty config must deny")
	}

	// Owner set but unverified → denied (incl. the owner).
	cs.m[meow.CfgOwnerChatID] = "123"
	if c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("unverified owner must be denied")
	}

	// Verified → only the owner id passes.
	cs.m[meow.CfgOwnerVerified] = "true"
	if !c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("verified owner should be allowed")
	}
	if c.meowOwnerAllowed(ctx, "999") {
		t.Fatal("non-owner must be denied")
	}
}
