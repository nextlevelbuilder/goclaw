package oa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// TestPollCountFromCfg covers the [1, 10] clamp + zero/negative default.
// Zalo's listrecentchat hard-caps count at 10 (error -210 above).
func TestPollCountFromCfg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want int
	}{
		{-1, 10}, // negative → default
		{0, 10},  // zero → default
		{1, 1},   // floor
		{5, 5},   // identity
		{10, 10}, // ceiling
		{11, 10}, // above ceiling → ceiling (Zalo cap)
		{50, 10},
		{999, 10},
	}
	for _, tc := range cases {
		got := pollCountFromCfg(tc.in)
		if got != tc.want {
			t.Errorf("pollCountFromCfg(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestPollBurndownMaxPagesFromCfg covers the [1, 20] clamp + zero/negative default.
func TestPollBurndownMaxPagesFromCfg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want int
	}{
		{-1, 10}, // negative → default
		{0, 10},  // zero → default
		{1, 1},   // floor (disable burn-down)
		{5, 5},   // identity
		{10, 10}, // identity (default)
		{20, 20}, // ceiling
		{21, 20}, // above ceiling → ceiling
		{999, 20},
	}
	for _, tc := range cases {
		got := pollBurndownMaxPagesFromCfg(tc.in)
		if got != tc.want {
			t.Errorf("pollBurndownMaxPagesFromCfg(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// burnDownServer fakes listrecentchat with per-call bodies so tests can
// drive multi-page burn-down behavior.
type burnDownServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	calls    []burnDownCall // (offset, count) per call, in order
	pages    []string       // body to return per call (nth call returns nth body)
	defaultB string         // returned when calls > len(pages)
	hits     atomic.Int32
}

type burnDownCall struct {
	offset string
	count  string
}

func newBurnDownServer(t *testing.T, pages []string) *burnDownServer {
	t.Helper()
	bs := &burnDownServer{pages: pages, defaultB: `{"error":0,"data":[]}`}
	bs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2.0/oa/listrecentchat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// data={"offset":N,"count":M}
		data := r.URL.Query().Get("data")
		bs.mu.Lock()
		idx := int(bs.hits.Load())
		bs.hits.Add(1)
		bs.calls = append(bs.calls, parseDataParam(data))
		bs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		if idx < len(bs.pages) {
			_, _ = w.Write([]byte(bs.pages[idx]))
			return
		}
		_, _ = w.Write([]byte(bs.defaultB))
	}))
	t.Cleanup(bs.srv.Close)
	return bs
}

func parseDataParam(data string) burnDownCall {
	// Cheap extract of "offset" and "count" without bringing in encoding/json
	// for the test helper. Body is always {"offset":N,"count":M}.
	c := burnDownCall{}
	for _, key := range []string{"offset", "count"} {
		needle := `"` + key + `":`
		i := strings.Index(data, needle)
		if i < 0 {
			continue
		}
		j := i + len(needle)
		end := j
		for end < len(data) && data[end] >= '0' && data[end] <= '9' {
			end++
		}
		val := data[j:end]
		if key == "offset" {
			c.offset = val
		} else {
			c.count = val
		}
	}
	return c
}

func newBurnDownChannel(t *testing.T, bs *burnDownServer, cfg config.ZaloOAConfig) (*Channel, *bus.MessageBus) {
	t.Helper()
	creds := &ChannelCreds{
		AppID: "app", SecretKey: "key", OAID: "oa-1",
		AccessToken: "AT", RefreshToken: "RT", ExpiresAt: time.Now().Add(time.Hour),
	}
	msgBus := bus.New()
	c, err := New("burndown_test", cfg, creds, &fakeStore{}, msgBus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.client.apiBase = bs.srv.URL
	return c, msgBus
}

// drainInbound consumes inbound messages until the bus is empty or budget exceeded.
func drainInbound(t *testing.T, msgBus *bus.MessageBus, max int) []string {
	t.Helper()
	out := make([]string, 0, max)
	for i := 0; i < max+1; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		msg, ok := msgBus.ConsumeInbound(ctx)
		cancel()
		if !ok {
			return out
		}
		out = append(out, msg.Metadata["message_id"]+":"+msg.Content)
	}
	return out
}

// genFullPage produces a JSON listrecentchat response with `n` messages.
// Each message has unique IDs and monotonically-increasing time so cursor
// dedup is exercised correctly.
func genFullPage(prefix string, startTime int64, n int) string {
	var sb strings.Builder
	sb.WriteString(`{"error":0,"data":[`)
	for i := range n {
		if i > 0 {
			sb.WriteString(",")
		}
		// from_id: alternate users to mimic realistic spread; not "oa-1" (avoid self-echo filter)
		userID := "u" + intStr(1+(i%3))
		sb.WriteString(`{"message_id":"`)
		sb.WriteString(prefix)
		sb.WriteString("-")
		sb.WriteString(intStr(i))
		sb.WriteString(`","from_id":"`)
		sb.WriteString(userID)
		sb.WriteString(`","time":`)
		sb.WriteString(int64Str(startTime + int64(i)))
		sb.WriteString(`,"message":"hi `)
		sb.WriteString(intStr(i))
		sb.WriteString(`","type":"text"}`)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

func intStr(n int) string { return int64Str(int64(n)) }
func int64Str(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestPollOnce_BurnDown_PartialPageStops: page 0 returns 10 (full), page 1 returns 6 (partial).
// Expect 2 calls, 16 unique messages dispatched.
func TestPollOnce_BurnDown_PartialPageStops(t *testing.T) {
	t.Parallel()
	bs := newBurnDownServer(t, []string{
		genFullPage("p0", 1000, 10),
		genFullPage("p1", 2000, 6),
	})
	c, msgBus := newBurnDownChannel(t, bs, config.ZaloOAConfig{PollCount: 10, PollBurndownMaxPages: 5})

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	if got := bs.hits.Load(); got != 2 {
		t.Errorf("listrecentchat calls = %d, want 2 (full then partial)", got)
	}
	bs.mu.Lock()
	if len(bs.calls) >= 2 {
		if bs.calls[0].offset != "0" || bs.calls[0].count != "10" {
			t.Errorf("call[0] = (offset=%s,count=%s), want (0,10)", bs.calls[0].offset, bs.calls[0].count)
		}
		if bs.calls[1].offset != "10" || bs.calls[1].count != "10" {
			t.Errorf("call[1] = (offset=%s,count=%s), want (10,10)", bs.calls[1].offset, bs.calls[1].count)
		}
	}
	bs.mu.Unlock()

	got := drainInbound(t, msgBus, 100)
	if len(got) != 16 {
		t.Errorf("inbound count = %d, want 16", len(got))
	}
}

// TestPollOnce_BurnDown_EmptyPageStops: page 0 returns 10 (full), page 1 returns 0 (empty).
// Expect 2 calls, 10 unique messages dispatched.
func TestPollOnce_BurnDown_EmptyPageStops(t *testing.T) {
	t.Parallel()
	bs := newBurnDownServer(t, []string{
		genFullPage("p0", 1000, 10),
		`{"error":0,"data":[]}`,
	})
	c, msgBus := newBurnDownChannel(t, bs, config.ZaloOAConfig{PollCount: 10, PollBurndownMaxPages: 5})

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if got := bs.hits.Load(); got != 2 {
		t.Errorf("listrecentchat calls = %d, want 2", got)
	}
	got := drainInbound(t, msgBus, 100)
	if len(got) != 10 {
		t.Errorf("inbound count = %d, want 10", len(got))
	}
}

// TestPollOnce_BurnDown_MaxPagesCapsAndWarns: pages are saturated (always full),
// burn-down stops at max_pages with a warn log.
func TestPollOnce_BurnDown_MaxPagesCapsAndWarns(t *testing.T) {
	t.Parallel()
	// Five full pages (10 each) then an empty one we should never reach.
	bs := newBurnDownServer(t, []string{
		genFullPage("p0", 1000, 10),
		genFullPage("p1", 2000, 10),
		genFullPage("p2", 3000, 10),
		genFullPage("p3", 4000, 10),
		genFullPage("p4", 5000, 10),
		`{"error":0,"data":[]}`, // should NOT be hit
	})
	c, msgBus := newBurnDownChannel(t, bs, config.ZaloOAConfig{PollCount: 10, PollBurndownMaxPages: 5})

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if got := bs.hits.Load(); got != 5 {
		t.Errorf("listrecentchat calls = %d, want 5 (capped by max_pages)", got)
	}
	got := drainInbound(t, msgBus, 100)
	if len(got) != 50 {
		t.Errorf("inbound count = %d, want 50", len(got))
	}
}

// TestPollOnce_BurnDown_MaxPagesOne_DisablesBurnDown: max_pages=1 → exactly one call,
// no burn-down even on a full page.
func TestPollOnce_BurnDown_MaxPagesOne_DisablesBurnDown(t *testing.T) {
	t.Parallel()
	bs := newBurnDownServer(t, []string{
		genFullPage("p0", 1000, 10),
		genFullPage("p1", 2000, 10), // never reached
	})
	c, msgBus := newBurnDownChannel(t, bs, config.ZaloOAConfig{PollCount: 10, PollBurndownMaxPages: 1})

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if got := bs.hits.Load(); got != 1 {
		t.Errorf("listrecentchat calls = %d, want 1 (max_pages=1 disables burn-down)", got)
	}
	got := drainInbound(t, msgBus, 100)
	if len(got) != 10 {
		t.Errorf("inbound count = %d, want 10", len(got))
	}
}

// TestPollOnce_BurnDown_DefaultsApplyWhenZero: PollCount=0, PollBurndownMaxPages=0
// → default count=10 applied (matches Zalo's API hard cap).
func TestPollOnce_BurnDown_DefaultsApplyWhenZero(t *testing.T) {
	t.Parallel()
	bs := newBurnDownServer(t, []string{
		genFullPage("p0", 1000, 10),
		`{"error":0,"data":[]}`,
	})
	c, _ := newBurnDownChannel(t, bs, config.ZaloOAConfig{}) // both unset

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	bs.mu.Lock()
	if len(bs.calls) > 0 && bs.calls[0].count != "10" {
		t.Errorf("first call count = %s, want 10 (default)", bs.calls[0].count)
	}
	bs.mu.Unlock()
}

// TestPollOnce_BurnDown_NoDoubleDispatchAcrossPages: page 0 messages partially
// reappear in page 1 (new arrivals shifted the window). Cursor dedup must
// drop the overlap so each unique message dispatches exactly once.
func TestPollOnce_BurnDown_NoDoubleDispatchAcrossPages(t *testing.T) {
	t.Parallel()
	// Page 0: 10 messages from u1, time 1000..1009 (full → burndown continues)
	// Page 1: 6 messages — 4 overlapping (1006..1009) + 2 fresh (1010..1011)
	page0 := genSingleUserPage("p0", "u1", 1000, 10)
	page1 := genSingleUserPage("overlap", "u1", 1006, 6)
	bs := newBurnDownServer(t, []string{page0, page1})
	c, msgBus := newBurnDownChannel(t, bs, config.ZaloOAConfig{PollCount: 10, PollBurndownMaxPages: 5})

	if err := c.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	got := drainInbound(t, msgBus, 100)
	// 10 unique from page 0, then page 1 brings 2 NEW (times 1010..1011);
	// the 4 overlapping (1006..1009) are dropped by the cursor.
	if len(got) != 12 {
		t.Errorf("inbound count = %d, want 12 (10 unique + 2 fresh; 4 overlap deduped)", len(got))
	}
}

// genSingleUserPage: all messages from one user_id with monotonic times.
func genSingleUserPage(prefix, userID string, startTime int64, n int) string {
	var sb strings.Builder
	sb.WriteString(`{"error":0,"data":[`)
	for i := range n {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"message_id":"`)
		sb.WriteString(prefix)
		sb.WriteString("-")
		sb.WriteString(intStr(i))
		sb.WriteString(`","from_id":"`)
		sb.WriteString(userID)
		sb.WriteString(`","time":`)
		sb.WriteString(int64Str(startTime + int64(i)))
		sb.WriteString(`,"message":"m`)
		sb.WriteString(intStr(i))
		sb.WriteString(`","type":"text"}`)
	}
	sb.WriteString(`]}`)
	return sb.String()
}
