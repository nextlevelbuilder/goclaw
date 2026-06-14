package telegram

import (
	"context"
	"strings"
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

func TestHandleMeowCommand_OwnerGated(t *testing.T) {
	ctx := context.Background()
	cs := &fakeSysCfg{m: map[string]string{meow.CfgOwnerChatID: "123"}} // configured, NOT verified
	published := 0
	c := &Channel{meowCfg: cs}
	c.meowPublish = func(_ context.Context, handle, date string, force bool) (string, error) {
		published++
		return "published " + handle + " " + date, nil
	}

	// Unverified owner: post rejected, publisher NOT called.
	if r := c.handleMeowCommand(ctx, "123", []string{"post", "@K", "2026-06-16"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("expected rejection before verify, got %q", r)
	}
	if published != 0 {
		t.Fatal("publisher must not run before verification")
	}

	// Verify as owner → success.
	if r := c.handleMeowCommand(ctx, "123", []string{"verify"}); !strings.Contains(r, "verified") {
		t.Fatalf("verify should succeed for owner, got %q", r)
	}

	// Owner post now works → publisher called once.
	if r := c.handleMeowCommand(ctx, "123", []string{"post", "@K", "2026-06-16"}); !strings.Contains(r, "published @K") {
		t.Fatalf("owner post should publish, got %q", r)
	}
	if published != 1 {
		t.Fatalf("publisher should be called once, got %d", published)
	}

	// Non-owner still rejected after owner verification.
	if r := c.handleMeowCommand(ctx, "999", []string{"post", "@K", "2026-06-16"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("non-owner must be rejected, got %q", r)
	}
	if published != 1 {
		t.Fatal("non-owner must not trigger publish")
	}

	// Usage on missing args.
	if r := c.handleMeowCommand(ctx, "123", []string{"post"}); !strings.Contains(r, "Usage") {
		t.Fatalf("expected usage, got %q", r)
	}
}
