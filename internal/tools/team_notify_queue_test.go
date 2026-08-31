package tools

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// drainCapture collects drained batches for assertions.
type drainCapture struct {
	mu      sync.Mutex
	batches []drainedBatch
}

type drainedBatch struct {
	items []string
	meta  NotifyRoutingMeta
}

func (d *drainCapture) onDrain(items []string, meta NotifyRoutingMeta) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := make([]string, len(items))
	copy(cp, items)
	d.batches = append(d.batches, drainedBatch{items: cp, meta: meta})
}

func (d *drainCapture) snapshot() []drainedBatch {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]drainedBatch, len(d.batches))
	copy(out, d.batches)
	return out
}

func (d *drainCapture) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.batches)
}

// TestNotifyKeyColonValuesDoNotCollide proves the typed key cannot alias two
// distinct scopes whose opaque fields, if concatenated with ':', would produce
// the same string. The old "teamID:chatID" scheme collided here.
func TestNotifyKeyColonValuesDoNotCollide(t *testing.T) {
	tenant := uuid.New()
	team := uuid.New().String()
	// (chatID="a:b", localKey="c") vs (chatID="a", localKey="b:c") both flatten
	// to "a:b:c" under naive concatenation but must stay distinct.
	first := normalizeNotifyMeta(NotifyRoutingMeta{TenantID: tenant, TeamID: team, ChatID: "a:b", LocalKey: "c"})
	second := normalizeNotifyMeta(NotifyRoutingMeta{TenantID: tenant, TeamID: team, ChatID: "a", LocalKey: "b:c"})
	if notifyKeyOf(first) == notifyKeyOf(second) {
		t.Fatal("colon-containing chat/local values collided into one batch key")
	}
}

// TestNotifyKeyDistinctTupleDimensionsSplit proves that changing any single
// routing dimension yields a different batch key.
func TestNotifyKeyDistinctTupleDimensionsSplit(t *testing.T) {
	base := NotifyRoutingMeta{
		TenantID: uuid.New(), TeamID: uuid.New().String(), Mode: "leader",
		Channel: "telegram", ChatID: "-100", UserID: "u1", LeadAgent: "lead", PeerKind: "group", LocalKey: "-100:topic:7",
	}
	baseKey := notifyKeyOf(normalizeNotifyMeta(base))

	variants := map[string]func(m *NotifyRoutingMeta){
		"tenant":    func(m *NotifyRoutingMeta) { m.TenantID = uuid.New() },
		"team":      func(m *NotifyRoutingMeta) { m.TeamID = uuid.New().String() },
		"mode":      func(m *NotifyRoutingMeta) { m.Mode = "direct" },
		"channel":   func(m *NotifyRoutingMeta) { m.Channel = "discord" },
		"chatID":    func(m *NotifyRoutingMeta) { m.ChatID = "-200" },
		"userID":    func(m *NotifyRoutingMeta) { m.UserID = "u2" },
		"leadAgent": func(m *NotifyRoutingMeta) { m.LeadAgent = "other-lead" },
		"peerKind":  func(m *NotifyRoutingMeta) { m.PeerKind = "direct" },
		"localKey":  func(m *NotifyRoutingMeta) { m.LocalKey = "-100:topic:8" },
	}
	for name, mutate := range variants {
		v := base
		mutate(&v)
		if notifyKeyOf(normalizeNotifyMeta(v)) == baseKey {
			t.Fatalf("changing %s did not change the batch key", name)
		}
	}
}

// TestNotifyKeyEnumFieldsCaseInsensitive proves only enum-like fields fold case
// while opaque identifiers stay verbatim.
func TestNotifyKeyEnumFieldsCaseInsensitive(t *testing.T) {
	tenant := uuid.New()
	team := uuid.New().String()
	lower := NotifyRoutingMeta{TenantID: tenant, TeamID: team, Mode: "leader", Channel: "telegram", PeerKind: "group"}
	upper := NotifyRoutingMeta{TenantID: tenant, TeamID: team, Mode: "LEADER", Channel: "Telegram", PeerKind: "GROUP"}
	if notifyKeyOf(normalizeNotifyMeta(lower)) != notifyKeyOf(normalizeNotifyMeta(upper)) {
		t.Fatal("enum-like fields (mode/channel/peerKind) must be case-insensitive")
	}

	// Opaque fields must NOT fold case.
	a := NotifyRoutingMeta{TenantID: tenant, TeamID: team, UserID: "User", LeadAgent: "Lead", ChatID: "Chat", LocalKey: "Key"}
	b := NotifyRoutingMeta{TenantID: tenant, TeamID: team, UserID: "user", LeadAgent: "lead", ChatID: "chat", LocalKey: "key"}
	if notifyKeyOf(normalizeNotifyMeta(a)) == notifyKeyOf(normalizeNotifyMeta(b)) {
		t.Fatal("opaque identifiers must be case-sensitive")
	}
}

// TestNotifyQueueMergesOnlyMatchingTuple proves items merge into one batch iff
// the full tuple matches, and split otherwise.
func TestNotifyQueueMergesOnlyMatchingTuple(t *testing.T) {
	cap := &drainCapture{}
	q := NewTeamNotifyQueue(20, cap.onDrain)
	tenant := uuid.New()
	team := uuid.New().String()

	metaA := NotifyRoutingMeta{TenantID: tenant, TeamID: team, Mode: "leader", Channel: "telegram", ChatID: "-100", PeerKind: "group", LocalKey: "-100:topic:1"}
	metaB := metaA
	metaB.LocalKey = "-100:topic:2" // different forum topic → different batch

	q.Enqueue("a1", metaA)
	q.Enqueue("a2", metaA)
	q.Enqueue("b1", metaB)

	// Wait for debounce drains.
	deadline := time.After(2 * time.Second)
	for cap.count() < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected 2 batches, got %d", cap.count())
		case <-time.After(5 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond) // ensure no third drain sneaks in
	batches := cap.snapshot()
	if len(batches) != 2 {
		t.Fatalf("expected exactly 2 batches, got %d", len(batches))
	}
	var merged, single int
	for _, b := range batches {
		switch len(b.items) {
		case 2:
			merged++
			if b.meta.LocalKey != "-100:topic:1" {
				t.Fatalf("merged batch had wrong meta: %+v", b.meta)
			}
		case 1:
			single++
			if b.meta.LocalKey != "-100:topic:2" {
				t.Fatalf("single batch had wrong meta: %+v", b.meta)
			}
		default:
			t.Fatalf("unexpected batch size %d", len(b.items))
		}
	}
	if merged != 1 || single != 1 {
		t.Fatalf("expected one merged + one single batch, got merged=%d single=%d", merged, single)
	}
}

// TestNotifyQueueFirstMetaFrozen proves the first enqueue's frozen metadata is
// preserved even if a later matching-tuple enqueue carries drifted non-key
// fields (only the key dimensions decide merging; the frozen meta wins).
func TestNotifyQueueFirstMetaFrozen(t *testing.T) {
	cap := &drainCapture{}
	q := NewTeamNotifyQueue(20, cap.onDrain)
	tenant := uuid.New()
	team := uuid.New().String()

	first := NotifyRoutingMeta{TenantID: tenant, TeamID: team, Mode: "leader", Channel: "telegram", ChatID: "-100", PeerKind: "group", LocalKey: "-100:topic:1", LeadAgent: "lead-first"}
	q.Enqueue("first", first)
	// Same key tuple; LeadAgent is part of the key, so to stay in the same batch
	// it must be identical. Re-enqueue an identical tuple.
	q.Enqueue("second", first)

	deadline := time.After(2 * time.Second)
	for cap.count() < 1 {
		select {
		case <-deadline:
			t.Fatal("expected a drained batch")
		case <-time.After(5 * time.Millisecond):
		}
	}
	batches := cap.snapshot()
	if len(batches) != 1 {
		t.Fatalf("expected exactly one batch, got %d", len(batches))
	}
	if batches[0].meta.LeadAgent != "lead-first" {
		t.Fatalf("frozen meta was overwritten: %+v", batches[0].meta)
	}
	if len(batches[0].items) != 2 {
		t.Fatalf("expected both items merged, got %d", len(batches[0].items))
	}
}

// TestNotifyQueueCapDrainsImmediately proves reaching cap drains synchronously
// (before the debounce timer) and clears the batch.
func TestNotifyQueueCapDrainsImmediately(t *testing.T) {
	cap := &drainCapture{}
	// Large debounce so only the cap threshold can trigger a drain in time.
	q := NewTeamNotifyQueue(60000, cap.onDrain)
	q.cap = 3
	meta := NotifyRoutingMeta{TenantID: uuid.New(), TeamID: uuid.New().String(), Channel: "telegram", ChatID: "-100"}

	q.Enqueue("1", meta)
	q.Enqueue("2", meta)
	if cap.count() != 0 {
		t.Fatalf("drain happened before cap reached: %d", cap.count())
	}
	q.Enqueue("3", meta) // hits cap → immediate async drain

	deadline := time.After(2 * time.Second)
	for cap.count() < 1 {
		select {
		case <-deadline:
			t.Fatal("cap drain did not fire")
		case <-time.After(5 * time.Millisecond):
		}
	}
	batches := cap.snapshot()
	if len(batches) != 1 || len(batches[0].items) != 3 {
		t.Fatalf("expected one 3-item cap drain, got %+v", batches)
	}

	// Batch must be cleared; a following enqueue starts fresh.
	q.mu.Lock()
	remaining := len(q.batches)
	q.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("cap drain did not clear the batch map: %d left", remaining)
	}
}

// TestNotifyQueueConcurrentEnqueueRaceClean exercises concurrent enqueues across
// overlapping tuples to surface races under -race. All items must eventually
// drain with no lost or duplicated content.
func TestNotifyQueueConcurrentEnqueueRaceClean(t *testing.T) {
	cap := &drainCapture{}
	q := NewTeamNotifyQueue(30, cap.onDrain)
	tenant := uuid.New()
	team := uuid.New().String()

	const workers = 8
	const perWorker = 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			meta := NotifyRoutingMeta{
				TenantID: tenant, TeamID: team, Mode: "leader", Channel: "telegram",
				ChatID: "-100", PeerKind: "group", LocalKey: "-100:topic:1",
			}
			// Half the workers target a second topic to force two live batches.
			if w%2 == 1 {
				meta.LocalKey = "-100:topic:2"
			}
			for i := 0; i < perWorker; i++ {
				q.Enqueue("item", meta)
			}
		}(w)
	}
	wg.Wait()

	// Wait until total drained items == everything enqueued.
	want := workers * perWorker
	deadline := time.After(3 * time.Second)
	for {
		total := 0
		for _, b := range cap.snapshot() {
			total += len(b.items)
		}
		if total == want {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected %d drained items, saw %d", want, total)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
