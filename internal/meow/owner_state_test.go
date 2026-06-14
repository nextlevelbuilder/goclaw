package meow

import (
	"context"
	"testing"
)

// fakeConfig is an in-memory OwnerConfigStore.
type fakeConfig struct{ m map[string]string }

func newFakeConfig() *fakeConfig { return &fakeConfig{m: map[string]string{}} }
func (f *fakeConfig) Get(_ context.Context, k string) (string, error) {
	return f.m[k], nil
}
func (f *fakeConfig) Set(_ context.Context, k, v string) error {
	f.m[k] = v
	return nil
}

func TestLoadOwnerGate_ClosedByDefault(t *testing.T) {
	ctx := context.Background()

	// Nil store → closed.
	if LoadOwnerGate(ctx, nil).Allowed("123") {
		t.Fatal("nil config must yield a closed gate")
	}

	cs := newFakeConfig()
	// Empty config → closed (no owner, not verified).
	if LoadOwnerGate(ctx, cs).Allowed("123") {
		t.Fatal("empty config must deny")
	}

	// Owner configured but NOT verified → still closed, even for the owner.
	_ = SetOwnerChatID(ctx, cs, "123")
	if LoadOwnerGate(ctx, cs).Allowed("123") {
		t.Fatal("configured-but-unverified owner must be denied")
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	ctx := context.Background()
	cs := newFakeConfig()
	_ = SetOwnerChatID(ctx, cs, "123")

	// Wrong sender does not verify.
	if ok, _ := VerifyRoundTrip(ctx, cs, "999"); ok {
		t.Fatal("non-owner must not verify")
	}
	if LoadOwnerGate(ctx, cs).Allowed("123") {
		t.Fatal("gate must stay closed after a failed verify")
	}

	// Owner sender verifies → gate opens for the owner only.
	if ok, _ := VerifyRoundTrip(ctx, cs, "123"); !ok {
		t.Fatal("owner sender should verify")
	}
	g := LoadOwnerGate(ctx, cs)
	if !g.Allowed("123") {
		t.Fatal("verified owner should be allowed")
	}
	if g.Allowed("999") {
		t.Fatal("non-owner still denied after verification")
	}
}
