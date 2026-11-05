package browsertakeover

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type capture struct {
	mu     sync.Mutex
	events []map[string]any
	names  []string
	ready  chan string // request id of each requested event
}

func newManager(connected bool) (*Manager, *capture) {
	c := &capture{ready: make(chan string, 4)}
	m := New()
	m.Configure(func(event string, _ uuid.UUID, payload map[string]any) {
		c.mu.Lock()
		c.names = append(c.names, event)
		c.events = append(c.events, payload)
		c.mu.Unlock()
		if event == protocol.EventBrowserTakeoverRequested {
			c.ready <- payload["id"].(string)
		}
	}, func(uuid.UUID, string) bool { return connected })
	return m, c
}

func spec(tenant uuid.UUID, user, target string) Spec {
	return Spec{Tenant: tenant, User: user, TargetID: target, Host: "login.example.com", Reason: "solve the captcha"}
}

type outcome struct {
	res Result
	err error
}

func start(m *Manager, ctx context.Context, s Spec) <-chan outcome {
	ch := make(chan outcome, 1)
	go func() {
		r, err := m.Request(ctx, s)
		ch <- outcome{r, err}
	}()
	return ch
}

func wait(t *testing.T, ch <-chan outcome) outcome {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(2 * time.Second):
		t.Fatal("request did not finish")
		return outcome{}
	}
}

func TestRequest_DoneByOwner(t *testing.T) {
	m, c := newManager(true)
	tenant := uuid.New()
	ch := start(m, context.Background(), spec(tenant, "u1", "tab1"))
	id := <-c.ready

	if err := m.Done(tenant, "u1", id); err != nil {
		t.Fatal(err)
	}
	if o := wait(t, ch); o.err != nil || o.res.Status != StatusDone {
		t.Fatalf("got %+v", o)
	}
	last := c.events[len(c.events)-1]
	if c.names[len(c.names)-1] != protocol.EventBrowserTakeoverResolved || last["status"] != StatusDone || last["userId"] != "u1" {
		t.Fatalf("resolved event %v %v", c.names, last)
	}
	// The tab is free again.
	ch = start(m, context.Background(), spec(tenant, "u1", "tab1"))
	id = <-c.ready
	_ = m.Cancel(tenant, "u1", id)
	wait(t, ch)
}

func TestRequest_ForeignUserTenantAndUnknownIDAreNotFoundAndKeepRequestOpen(t *testing.T) {
	m, c := newManager(true)
	tenant := uuid.New()
	ch := start(m, context.Background(), spec(tenant, "u1", "tab1"))
	id := <-c.ready

	for name, err := range map[string]error{
		"other user":   m.Done(tenant, "admin", id),
		"other tenant": m.Done(uuid.New(), "u1", id),
		"unknown id":   m.Done(tenant, "u1", uuid.NewString()),
		"cancel other": m.Cancel(tenant, "admin", id),
	} {
		if !errors.Is(err, ErrRequestNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	select {
	case <-ch:
		t.Fatal("a rejected answer must not resolve the request")
	default:
	}
	if err := m.Done(tenant, "u1", id); err != nil {
		t.Fatal(err)
	}
	wait(t, ch)
	if err := m.Done(tenant, "u1", id); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("second answer: %v", err)
	}
}

func TestRequest_FailsFastWithoutUI(t *testing.T) {
	m, _ := newManager(false)
	if _, err := m.Request(context.Background(), spec(uuid.New(), "u1", "tab1")); !errors.Is(err, ErrNoUI) {
		t.Fatalf("no client: %v", err)
	}
	if _, err := New().Request(context.Background(), spec(uuid.New(), "u1", "tab1")); !errors.Is(err, ErrNoUI) {
		t.Fatalf("unconfigured: %v", err)
	}
	m, _ = newManager(true)
	if _, err := m.Request(context.Background(), spec(uuid.New(), "", "tab1")); !errors.Is(err, ErrNoUI) {
		t.Fatalf("no user: %v", err)
	}
}

func TestRequest_OnePendingRequestPerTab(t *testing.T) {
	m, c := newManager(true)
	tenant := uuid.New()
	ch := start(m, context.Background(), spec(tenant, "u1", "tab1"))
	id := <-c.ready

	if _, err := m.Request(context.Background(), spec(tenant, "u1", "tab1")); !errors.Is(err, ErrBusy) {
		t.Fatalf("same tab: %v", err)
	}
	// Another tab, and the same tab id in another tenant, are independent.
	ch2 := start(m, context.Background(), spec(tenant, "u1", "tab2"))
	id2 := <-c.ready
	ch3 := start(m, context.Background(), spec(uuid.New(), "u2", "tab1"))
	id3 := <-c.ready
	_ = id2
	_ = id3
	_ = m.Done(tenant, "u1", id)
	wait(t, ch)
	// Release the others via context-free cancel paths.
	m.mu.Lock()
	var rest []*pending
	for _, p := range m.pending {
		rest = append(rest, p)
	}
	m.mu.Unlock()
	for _, p := range rest {
		_ = m.Cancel(p.spec.Tenant, p.spec.User, p.id)
	}
	wait(t, ch2)
	wait(t, ch3)
}

func TestRequest_TimeoutAndContextCancel(t *testing.T) {
	m, c := newManager(true)
	s := spec(uuid.New(), "u1", "tab1")
	s.Timeout = 30 * time.Millisecond
	ch := start(m, context.Background(), s)
	<-c.ready
	if o := wait(t, ch); o.err != nil || o.res.Status != StatusTimeout {
		t.Fatalf("timeout: %+v", o)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch = start(m, ctx, spec(uuid.New(), "u1", "tab1"))
	<-c.ready
	cancel()
	if o := wait(t, ch); o.err != nil || o.res.Status != StatusCancelled {
		t.Fatalf("cancel: %+v", o)
	}
	m.mu.Lock()
	left := len(m.pending) + len(m.byTab)
	m.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d entries leaked", left)
	}
}

func TestRequest_EventCarriesMetadataOnly(t *testing.T) {
	m, c := newManager(true)
	tenant := uuid.New()
	s := spec(tenant, "u1", "tab1")
	s.AgentKey, s.SessionKey = "agent-x", "sess-1"
	ch := start(m, context.Background(), s)
	id := <-c.ready
	c.mu.Lock()
	ev := c.events[0]
	c.mu.Unlock()
	want := map[string]bool{"id": true, "targetId": true, "host": true, "reason": true, "agentKey": true, "sessionKey": true, "userId": true, "expiresAt": true}
	for k := range ev {
		if !want[k] {
			t.Errorf("unexpected key %q in request event", k)
		}
	}
	_ = m.Cancel(tenant, "u1", id)
	wait(t, ch)
}
