package common

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestSharedRouter_Singleton(t *testing.T) {
	a := SharedRouter()
	b := SharedRouter()
	if a != b {
		t.Fatalf("SharedRouter must return identical *Router across calls")
	}
}

func TestMountRoute_FirstCallReturnsPath(t *testing.T) {
	r := NewRouter()
	path, h := r.MountRoute()
	if path != WebhookPathPrefix || h != r {
		t.Fatalf("first MountRoute = (%q, %v), want (%q, router)", path, h, WebhookPathPrefix)
	}
}

func TestMountRoute_SecondCallReturnsEmpty(t *testing.T) {
	r := NewRouter()
	_, _ = r.MountRoute()
	path, h := r.MountRoute()
	if path != "" || h != nil {
		t.Fatalf("second MountRoute = (%q, %v), want (\"\", nil)", path, h)
	}
}

func TestMountRoute_ConcurrentSafety(t *testing.T) {
	r := NewRouter()
	var wg sync.WaitGroup
	var mu sync.Mutex
	pathClaims := 0
	for range 100 {
		wg.Go(func() {
			path, _ := r.MountRoute()
			if path != "" {
				mu.Lock()
				pathClaims++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if pathClaims != 1 {
		t.Fatalf("expected exactly 1 path claim under concurrent calls, got %d", pathClaims)
	}
}

// TestMountRoute_StickyAcrossUnregister proves routeHandled does NOT reset
// when instances unregister. Re-mounting the same path on http.ServeMux
// panics, so this invariant is load-bearing for instance_loader.Reload.
func TestMountRoute_StickyAcrossUnregister(t *testing.T) {
	r := NewRouter()
	instID := uuid.New()
	handler := newFakeHandler()

	if err := r.RegisterInstance(instID, handler, uuid.Nil, "sticky"); err != nil {
		t.Fatalf("RegisterInstance: %v", err)
	}
	_, _ = r.MountRoute()
	r.UnregisterInstance(instID)

	path, _ := r.MountRoute()
	if path != "" {
		t.Fatalf("MountRoute must stay sticky after unregister; got %q", path)
	}
}
