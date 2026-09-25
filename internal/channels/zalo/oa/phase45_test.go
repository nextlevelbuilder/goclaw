package oa

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- poll cursor ---

func TestPollCursor_AdvanceOnlyNewer(t *testing.T) {
	c := newPollCursor(10)
	if !c.Advance("u1", 100) {
		t.Fatal("first advance must move")
	}
	if c.Advance("u1", 100) {
		t.Error("equal ts must not move")
	}
	if c.Advance("u1", 99) {
		t.Error("older ts must not move")
	}
	if !c.Advance("u1", 101) {
		t.Error("newer ts must move")
	}
	if got := c.Get("u1"); got != 101 {
		t.Errorf("Get = %d, want 101", got)
	}
	if !c.IsDirty() {
		t.Error("cursor must be dirty after advance")
	}
}

func TestPollCursor_LRUEviction(t *testing.T) {
	c := newPollCursor(3)
	for i := range 5 {
		c.Advance("u"+string(rune('0'+i)), int64(i+1))
	}
	if c.Get("u0") != 0 || c.Get("u1") != 0 {
		t.Error("oldest entries must be evicted")
	}
	if c.Get("u4") != 5 {
		t.Error("newest entry must survive")
	}
}

func TestPollCursor_SnapshotAndLoad(t *testing.T) {
	c := newPollCursor(10)
	c.Advance("a", 10)
	c.Advance("b", 20)
	snap := c.Snapshot()
	if snap["a"] != 10 || snap["b"] != 20 {
		t.Errorf("snapshot = %v", snap)
	}
	c2 := newPollCursor(10)
	c2.loadFromMap(snap)
	if c2.Get("a") != 10 || c2.Get("b") != 20 {
		t.Error("loadFromMap failed")
	}
	if c2.IsDirty() {
		t.Error("loadFromMap must clear dirty")
	}
}

func TestParseCursorFromConfig(t *testing.T) {
	out := parseCursorFromConfig([]byte(`{"dm_policy":"open","poll_cursor":{"a":1,"b":2}}`))
	if out["a"] != 1 || out["b"] != 2 {
		t.Errorf("out = %v", out)
	}
	if out := parseCursorFromConfig([]byte(`{"dm_policy":"open"}`)); len(out) != 0 {
		t.Errorf("missing cursor: %v", out)
	}
	if out := parseCursorFromConfig(nil); len(out) != 0 {
		t.Errorf("nil: %v", out)
	}
	if out := parseCursorFromConfig([]byte(`not json`)); len(out) != 0 {
		t.Errorf("malformed: %v", out)
	}
}

// --- seen ids ---

func TestSeenMessageIDs(t *testing.T) {
	s := newSeenMessageIDs(2)
	if s.SeenOrAdd("a") {
		t.Error("first must not be seen")
	}
	if !s.SeenOrAdd("a") {
		t.Error("second must be seen")
	}
	s.SeenOrAdd("b")
	s.SeenOrAdd("c")
	if s.SeenOrAdd("a") {
		t.Error("evicted a must not be seen")
	}
}

// --- poll helpers ---

func TestPollClampHelpers(t *testing.T) {
	if pollIntervalFromCfg(3) != defaultPollInterval {
		t.Error("floor clamp failed")
	}
	if pollIntervalFromCfg(999) != 120*time.Second {
		t.Error("ceiling clamp failed")
	}
	if pollCountFromCfg(0) != defaultPollCount {
		t.Error("count default failed")
	}
	if pollCountFromCfg(999) != pollCountCeil {
		t.Error("count ceiling failed")
	}
	if pollBurndownMaxPagesFromCfg(0) != defaultPollBurndownMaxPages {
		t.Error("burndown default failed")
	}
	if pollBurndownMaxPagesFromCfg(999) != pollBurndownMaxPagesCeil {
		t.Error("burndown ceiling failed")
	}
}

// --- signature ---

func TestComputeOASignature(t *testing.T) {
	got := computeOASignature("app", "body", "123", "sec")
	if len(got) != 64 {
		t.Fatalf("sig len = %d", len(got))
	}
	// Determinism + sensitivity
	if computeOASignature("app", "body", "123", "sec") != got {
		t.Error("not deterministic")
	}
	if computeOASignature("app", "body2", "123", "sec") == got {
		t.Error("body must affect sig")
	}
}

func TestSignatureVerify_Strict(t *testing.T) {
	body := []byte(`{"timestamp":"1700000000000","event_name":"user_send_text"}`)
	sig := computeOASignature("app", string(body), "1700000000000", "sec")
	v := newOASignatureVerifier("app", "sec", SignatureModeStrict, 0)

	h := http.Header{}
	h.Set(zaloOASignatureHeader, sig)
	if err := v.Verify(h, body); err != nil {
		t.Fatalf("valid bare-hex sig rejected: %v", err)
	}

	// Official doc form: "mac=<hex>" must also verify.
	h.Set(zaloOASignatureHeader, "mac="+sig)
	if err := v.Verify(h, body); err != nil {
		t.Fatalf("valid mac=-prefixed sig rejected: %v", err)
	}
	h.Set(zaloOASignatureHeader, "deadbeef")
	if err := v.Verify(h, body); err != common.ErrSignatureMismatch {
		t.Errorf("bad sig: %v", err)
	}

	h.Del(zaloOASignatureHeader)
	if err := v.Verify(h, body); err == nil {
		t.Error("missing sig must fail strict")
	}
}

func TestSignatureVerify_LogOnly(t *testing.T) {
	body := []byte(`{"timestamp":"1700000000000"}`)
	v := newOASignatureVerifier("app", "sec", SignatureModeLogOnly, 0)
	h := http.Header{}
	h.Set(zaloOASignatureHeader, "wrong")
	if err := v.Verify(h, body); err != nil {
		t.Errorf("log_only must accept bad sig: %v", err)
	}
	// Replay window violation also accepted in log_only.
	old := []byte(`{"timestamp":"1"}`)
	if err := v.Verify(h, old); err != nil {
		t.Errorf("log_only must accept replay: %v", err)
	}
}

func TestSignatureVerify_Disabled(t *testing.T) {
	v := newOASignatureVerifier("app", "", SignatureModeDisabled, 0)
	if err := v.Verify(http.Header{}, []byte(`{}`)); err != nil {
		t.Errorf("disabled must accept: %v", err)
	}
}

func TestSignatureVerify_ReplayWindow(t *testing.T) {
	now := time.Now()
	ok := []byte(`{"timestamp":"` + itoa(now.UnixMilli()) + `"}`)
	old := []byte(`{"timestamp":"1"}`) // seconds-scale: 1970
	v := newOASignatureVerifier("app", "sec", SignatureModeStrict, clampReplayWindowSeconds(0))
	sig := computeOASignature("app", string(ok), itoa(now.UnixMilli()), "sec")
	h := http.Header{}
	h.Set(zaloOASignatureHeader, sig)
	if err := v.Verify(h, ok); err != nil {
		t.Fatalf("fresh event rejected: %v", err)
	}
	sigOld := computeOASignature("app", string(old), "1", "sec")
	h.Set(zaloOASignatureHeader, sigOld)
	if err := v.Verify(h, old); err == nil {
		t.Error("1970 event must fail replay window")
	}
}

func TestClampReplayWindowSeconds(t *testing.T) {
	if clampReplayWindowSeconds(0) != defaultReplayWindow {
		t.Error("default failed")
	}
	if clampReplayWindowSeconds(10) != 60*time.Second {
		t.Error("floor failed")
	}
	if clampReplayWindowSeconds(99999) != 3600*time.Second {
		t.Error("ceiling failed")
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
func TestHandleWebhookEvent_TextDispatch(t *testing.T) {
	ch := newTestOAChannel(t)
	raw := json.RawMessage(`{"event_name":"user_send_text","sender":{"id":"u1","display_name":"N"},"message":{"message_id":"m1","text":"hi"}}`)
	if err := ch.HandleWebhookEvent(context.Background(), raw); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := ch.Bus().ConsumeInbound(ctx)
	if !ok {
		t.Fatal("text event not dispatched")
	}
	if msg.SenderID != "u1" || msg.Content != "hi" {
		t.Errorf("msg = %+v", msg)
	}
}

func newTestOAChannel(t *testing.T) *Channel {
	t.Helper()
	ch, err := New("test-oa", config.ZaloOAConfig{Transport: "polling", DMPolicy: "open"},
		&ChannelCreds{AppID: "app", SecretKey: "sec", OAID: "oa-1"},
		&noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.instanceID = [16]byte{1}
	return ch
}

func TestHandleWebhookEvent_SelfEchoDropped(t *testing.T) {
	ch := newTestOAChannel(t)
	raw := json.RawMessage(`{"event_name":"user_send_text","sender":{"id":"oa-1"},"message":{"message_id":"m1","text":"echo"}}`)
	if err := ch.HandleWebhookEvent(context.Background(), raw); err != nil {
		t.Fatalf("self-echo must not error: %v", err)
	}
}

func TestHandleWebhookEvent_OutboundMirrorDropped(t *testing.T) {
	ch := newTestOAChannel(t)
	raw := json.RawMessage(`{"event_name":"oa_send_text","sender":{"id":"oa-1"},"message":{"message_id":"m1","text":"sent"}}`)
	if err := ch.HandleWebhookEvent(context.Background(), raw); err != nil {
		t.Fatalf("outbound mirror must not error: %v", err)
	}
}

func TestHandleWebhookEvent_UnknownEvent(t *testing.T) {
	ch := newTestOAChannel(t)
	raw := json.RawMessage(`{"event_name":"weird_event","sender":{"id":"u1"}}`)
	if err := ch.HandleWebhookEvent(context.Background(), raw); err != nil {
		t.Fatalf("unknown event must not error: %v", err)
	}
}

func TestHandleWebhookEvent_BootstrapDrops(t *testing.T) {
	ch := newTestOAChannel(t)
	ch.cfg.WebhookSignatureMode = SignatureModeStrict // no WebhookSecretKey → bootstrap
	raw := json.RawMessage(`{"event_name":"user_send_text","sender":{"id":"u1"},"message":{"message_id":"m1","text":"hi"}}`)
	if err := ch.HandleWebhookEvent(context.Background(), raw); err != nil {
		t.Fatalf("bootstrap drop must not error: %v", err)
	}
	if !ch.inBootstrap() {
		t.Error("channel should be in bootstrap mode")
	}
	if ch.BootstrapDroppedForTest() != 1 {
		t.Errorf("dropped count = %d, want 1", ch.BootstrapDroppedForTest())
	}
}

func TestMessageIDExtractor(t *testing.T) {
	ex := oaMessageIDExtractor{}
	if got := ex.ExtractMessageID(json.RawMessage(`{"message":{"message_id":"m1"}}`)); got != "m1" {
		t.Errorf("message_id: %q", got)
	}
	if got := ex.ExtractMessageID(json.RawMessage(`{"message":{"msg_id":"m2"}}`)); got != "m2" {
		t.Errorf("msg_id: %q", got)
	}
	if got := ex.ExtractMessageID(json.RawMessage(`{"message":{}}`)); got != "" {
		t.Errorf("empty: %q", got)
	}
}

// --- reactions ---

func TestResolveReactionEmoji(t *testing.T) {
	if resolveReactionEmoji("thinking") != reactionIconLike {
		t.Errorf("thinking = %q", resolveReactionEmoji("thinking"))
	}
	if resolveReactionEmoji("done") != reactionIconHeart {
		t.Errorf("done = %q", resolveReactionEmoji("done"))
	}
	if resolveReactionEmoji("error") != reactionIconWorry {
		t.Errorf("error = %q", resolveReactionEmoji("error"))
	}
	if resolveReactionEmoji("tool") != "" {
		t.Error("tool must not map")
	}
}

func TestBuildReactionBody(t *testing.T) {
	body := buildReactionBody("u1", "m1", reactionIconLike)
	want := `{"recipient":{"user_id":"u1"},"sender_action":{"react_icon":"/-strong","react_message_id":"m1"}}`
	if got := mustJSON(t, body); got != want {
		t.Errorf("buildReactionBody = %s, want %s", got, want)
	}
}

func TestOnReactionEvent_OffIsNoop(t *testing.T) {
	ch := newTestOAChannel(t)
	ch.cfg.ReactionLevel = "off"
	if err := ch.OnReactionEvent(context.Background(), "u1", "m1", "thinking"); err != nil {
		t.Fatalf("off must no-op: %v", err)
	}
	if _, ok := ch.reactions.Load("u1:m1"); ok {
		t.Error("controller created despite off")
	}
}

func TestOnReactionEvent_MinimalFiltersIntermediate(t *testing.T) {
	ch := newTestOAChannel(t)
	ch.cfg.ReactionLevel = "minimal"
	if err := ch.OnReactionEvent(context.Background(), "u1", "m1", "thinking"); err != nil {
		t.Fatalf("minimal thinking: %v", err)
	}
	if _, ok := ch.reactions.Load("u1:m1"); ok {
		t.Error("intermediate status must be filtered in minimal mode")
	}
	if err := ch.OnReactionEvent(context.Background(), "u1", "m1", "done"); err != nil {
		t.Fatalf("minimal done: %v", err)
	}
	if _, ok := ch.reactions.Load("u1:m1"); !ok {
		t.Error("terminal status must create controller in minimal mode")
	}
	ch.Stop(context.Background())
}

// --- factory ---

func TestFactory_RequiresAppIDAndSecret(t *testing.T) {
	f := Factory(&noopCIStore{})
	_, err := f("z1", json.RawMessage(`{"token":"bot-token"}`), json.RawMessage(`{}`), bus.New(), nil)
	if err == nil {
		t.Fatal("bot-token creds must be rejected")
	}
	ch, err := f("z1", json.RawMessage(`{"app_id":"a","secret_key":"s"}`), json.RawMessage(`{"transport":"polling"}`), bus.New(), nil)
	if err != nil {
		t.Fatalf("valid creds: %v", err)
	}
	oa, ok := ch.(*Channel)
	if !ok {
		t.Fatalf("type = %T", ch)
	}
	if oa.Type() != "zalo_oa" {
		t.Errorf("Type = %q", oa.Type())
	}
	if oa.cfg.DMPolicy != "pairing" {
		t.Errorf("default DMPolicy = %q, want pairing", oa.cfg.DMPolicy)
	}
}

func TestFactory_SeedsCursorFromConfig(t *testing.T) {
	f := Factory(&noopCIStore{})
	cfg := json.RawMessage(`{"transport":"polling","poll_cursor":{"u1":42}}`)
	ch, err := f("z1", json.RawMessage(`{"app_id":"a","secret_key":"s"}`), cfg, bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	oa := ch.(*Channel)
	if oa.cursor.Get("u1") != 42 {
		t.Errorf("cursor seed = %d, want 42", oa.cursor.Get("u1"))
	}
}

func TestFactory_NilStoreRejected(t *testing.T) {
	f := Factory(nil)
	if _, err := f("z1", json.RawMessage(`{"app_id":"a","secret_key":"s"}`), nil, bus.New(), nil); err == nil {
		t.Fatal("nil store must be rejected")
	}
}

// --- interface asserts ---

func TestInterfaceAsserts(t *testing.T) {
	ch := newTestOAChannel(t)
	var _ = map[string]any{}
	_ = ch
}

var _ = store.PairingStore(nil)
