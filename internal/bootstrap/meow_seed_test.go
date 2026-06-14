package bootstrap

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// captureMeowStore records UpsertChannel calls; all other methods are no-ops.
// Lets us verify SeedMeowChannels without a database.
type captureMeowStore struct {
	store.MeowStore
	upserts []store.MpChannel
}

func (c *captureMeowStore) UpsertChannel(_ context.Context, ch *store.MpChannel) error {
	ch.ID = uuid.New()
	c.upserts = append(c.upserts, *ch)
	return nil
}

func TestSeedMeowChannels(t *testing.T) {
	cs := &captureMeowStore{}
	n, err := SeedMeowChannels(context.Background(), cs, store.MasterTenantID)
	if err != nil {
		t.Fatalf("SeedMeowChannels: %v", err)
	}
	if n != 8 || len(cs.upserts) != 8 {
		t.Fatalf("expected 8 channels seeded, got n=%d upserts=%d", n, len(cs.upserts))
	}

	byHandle := map[string]store.MpChannel{}
	for _, ch := range cs.upserts {
		if ch.TenantID != store.MasterTenantID {
			t.Errorf("%s seeded under wrong tenant %s", ch.Handle, ch.TenantID)
		}
		if ch.BrandKey == "" || len(ch.ButtonSet) == 0 || string(ch.ButtonSet) == "[]" {
			t.Errorf("%s missing brand_key or buttons", ch.Handle)
		}
		if !ch.Enabled {
			t.Errorf("%s should be enabled", ch.Handle)
		}
		byHandle[ch.Handle] = ch
	}

	// Pre-launch channel must be launched=false; everything else launched=true.
	for h, ch := range byHandle {
		want := h != "@MonkeyMatgo"
		if ch.Launched != want {
			t.Errorf("%s launched=%v, want %v", h, ch.Launched, want)
		}
	}
	if mm, ok := byHandle["@MonkeyMatgo"]; !ok || mm.Launched {
		t.Error("@MonkeyMatgo must be present and launched=false")
	}
	// Mascot channels per the plan.
	for _, h := range []string{"@onedollar_project", "@OneJackpotOfficial", "@monkeytimeofficial", "@MonkeyMatgo"} {
		if !byHandle[h].HasMascot {
			t.Errorf("%s should have has_mascot=true", h)
		}
	}
}
