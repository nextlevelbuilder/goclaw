package audio_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

func TestVoiceCache_SetGetHit(t *testing.T) {
	c := audio.NewVoiceCache(time.Hour, 100)
	tid := uuid.New()
	voices := []audio.Voice{{ID: "v1", Name: "Bella"}}

	c.Set(tid, voices)
	got, ok := c.Get(tid)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 || got[0].ID != "v1" {
		t.Errorf("unexpected voices: %+v", got)
	}
}

func TestVoiceCache_ExpiredMiss(t *testing.T) {
	c := audio.NewVoiceCache(10*time.Millisecond, 100)
	tid := uuid.New()
	c.Set(tid, []audio.Voice{{ID: "v1"}})

	time.Sleep(20 * time.Millisecond)
	_, ok := c.Get(tid)
	if ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestVoiceCache_LRUEviction(t *testing.T) {
	cap := 3
	c := audio.NewVoiceCache(time.Hour, cap)

	ids := make([]uuid.UUID, cap+1)
	for i := range ids {
		ids[i] = uuid.New()
	}
	voices := []audio.Voice{{ID: "x"}}

	// Fill to capacity
	for i := range cap {
		c.Set(ids[i], voices)
	}
	// Access ids[0] to make it recently used
	c.Get(ids[0])

	// Insert one more — should evict the LRU entry (ids[1] was least recently used)
	c.Set(ids[cap], voices)

	_, ok := c.Get(ids[1])
	if ok {
		t.Fatal("ids[1] should have been evicted as LRU")
	}
	// ids[0] and ids[cap] must still be present
	if _, ok := c.Get(ids[0]); !ok {
		t.Error("ids[0] should still be cached (recently accessed)")
	}
	if _, ok := c.Get(ids[cap]); !ok {
		t.Error("ids[cap] should be cached (just inserted)")
	}
}

func TestVoiceCache_Invalidate(t *testing.T) {
	c := audio.NewVoiceCache(time.Hour, 100)
	tid := uuid.New()
	c.Set(tid, []audio.Voice{{ID: "v1"}})
	c.Invalidate(tid)
	_, ok := c.Get(tid)
	if ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestVoiceCache_ConcurrentSafe(t *testing.T) {
	c := audio.NewVoiceCache(time.Hour, 500)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			tid := uuid.New()
			c.Set(tid, []audio.Voice{{ID: "v"}})
			c.Get(tid)
			c.Invalidate(tid)
		})
	}
	wg.Wait()
}
