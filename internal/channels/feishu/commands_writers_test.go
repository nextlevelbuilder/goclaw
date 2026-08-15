package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeConfigPermStore is a minimal in-memory ConfigPermissionStore that
// implements only the methods the writer commands actually call. Other
// methods are no-ops or return nil so the interface is satisfied without
// a full fake implementation.
type fakeConfigPermStore struct {
	mu    sync.Mutex
	perms []store.ConfigPermission
}

func (f *fakeConfigPermStore) CheckPermission(_ context.Context, _ uuid.UUID, _, _, _ string) (bool, error) {
	return false, nil
}

func (f *fakeConfigPermStore) Grant(_ context.Context, perm *store.ConfigPermission) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Replace existing row for idempotency (matches real store's upsert).
	for i, p := range f.perms {
		if p.AgentID == perm.AgentID && p.Scope == perm.Scope && p.ConfigType == perm.ConfigType && p.UserID == perm.UserID {
			f.perms[i] = *perm
			return nil
		}
	}
	f.perms = append(f.perms, *perm)
	return nil
}

func (f *fakeConfigPermStore) Revoke(_ context.Context, agentID uuid.UUID, scope, configType, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.perms[:0]
	for _, p := range f.perms {
		if p.AgentID == agentID && p.Scope == scope && p.ConfigType == configType && p.UserID == userID {
			continue
		}
		kept = append(kept, p)
	}
	f.perms = kept
	return nil
}

func (f *fakeConfigPermStore) List(_ context.Context, agentID uuid.UUID, configType, scope string) ([]store.ConfigPermission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.ConfigPermission
	for _, p := range f.perms {
		if p.AgentID == agentID && p.ConfigType == configType && (scope == "" || p.Scope == scope) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeConfigPermStore) ListFileWriters(ctx context.Context, agentID uuid.UUID, scope string) ([]store.ConfigPermission, error) {
	return f.List(ctx, agentID, store.ConfigTypeFileWriter, scope)
}

// newTestChannel builds a minimal Feishu Channel suitable for writer-command
// unit tests: BaseChannel with a UUID agent key, stub lark client pointing
// at the provided httptest server, and the supplied permission store.
// botOpenID is pre-set to a fake value so writer commands pass the
// "bot identity resolved" guard (real production probes Lark for this).
func newTestChannel(t *testing.T, srvURL string, permStore store.ConfigPermissionStore, agentUUID uuid.UUID) *Channel {
	t.Helper()
	base := channels.NewBaseChannel(channels.TypeFeishu, nil, nil)
	base.SetAgentID(agentUUID.String())
	return &Channel{
		BaseChannel:     base,
		client:          NewLarkClient("app", "secret", srvURL),
		configPermStore: permStore,
		botOpenID:       "ou_fake_bot",
	}
}

// captureReplies returns an httptest server that records every POST message
// sent by the channel so the test can assert the reply content.
func captureReplies(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	replies := make([]string, 0, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") || strings.HasSuffix(r.URL.Path, "/reply") {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			// The Feishu "post" content wraps the reply text — we just record
			// the raw content payload so the test can grep it.
			if content, ok := body["content"].(string); ok {
				mu.Lock()
				replies = append(replies, content)
				mu.Unlock()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"message_id":"om_reply"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &replies
}

// assertReplyContains checks that at least one captured reply contains the
// expected substring. Useful because the "post" content is double-encoded
// JSON with sender name wrappers.
func assertReplyContains(t *testing.T, replies []string, want string) {
	t.Helper()
	for _, r := range replies {
		if strings.Contains(r, want) {
			return
		}
	}
	t.Errorf("no reply contained %q; captured replies: %v", want, replies)
}

// TestWriterCmd_RejectsInDM verifies DMs are rejected with a clear message.
func TestWriterCmd_RejectsInDM(t *testing.T) {
	srv, replies := captureReplies(t)
	ch := newTestChannel(t, srv.URL, &fakeConfigPermStore{}, uuid.New())

	mc := &messageContext{
		ChatID:    "oc_dm_1",
		MessageID: "om_cmd",
		SenderID:  "ou_alice",
		ChatType:  "p2p",
		Content:   "/addwriter",
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled (consumed)")
	}
	assertReplyContains(t, *replies, "only works in group chats")
}

// TestWriterCmd_NotAvailableWhenNoPermStore verifies graceful degradation.
func TestWriterCmd_NotAvailableWhenNoPermStore(t *testing.T) {
	srv, replies := captureReplies(t)
	ch := newTestChannel(t, srv.URL, nil, uuid.New())

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/writers",
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "not available")
}

// TestWriterCmd_BootstrapSelfGrantViaSelfMention verifies the first caller
// in an empty-writer group CAN bootstrap the allowlist, but ONLY when they
// explicitly @mention themselves — no accidental self-grant from a bare
// /addwriter typo.
func TestWriterCmd_BootstrapSelfGrantViaSelfMention(t *testing.T) {
	srv, replies := captureReplies(t)
	perm := &fakeConfigPermStore{}
	agentID := uuid.New()
	ch := newTestChannel(t, srv.URL, perm, agentID)

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/addwriter @_user_1",
		Mentions: []mentionInfo{{Key: "@_user_1", OpenID: "ou_alice", Name: "Alice"}},
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "Added")

	got, _ := perm.ListFileWriters(context.Background(), agentID, "group:feishu:oc_grp_1")
	if len(got) != 1 || got[0].UserID != "ou_alice" {
		t.Errorf("expected 1 writer (ou_alice), got %+v", got)
	}
}

// TestWriterCmd_BareAddWriterShowsUsageHint guards M1: a bare /addwriter
// with no target must NOT silently self-grant. Users typing the command
// exploratorily should see instructions, not capture first-writer.
func TestWriterCmd_BareAddWriterShowsUsageHint(t *testing.T) {
	srv, replies := captureReplies(t)
	perm := &fakeConfigPermStore{}
	agentID := uuid.New()
	ch := newTestChannel(t, srv.URL, perm, agentID)

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/addwriter", // no mention, no reply-to
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "reply to their message")

	// Critical: no grant must be created.
	got, _ := perm.ListFileWriters(context.Background(), agentID, "group:feishu:oc_grp_1")
	if len(got) != 0 {
		t.Errorf("expected 0 writers (no accidental self-grant), got %+v", got)
	}
}

// TestWriterCmd_BotProbeIncomplete guards M2: when botOpenID is empty
// (probe has not resolved yet), writer commands refuse with a retry hint
// rather than risk granting the bot itself as a writer via @mention.
func TestWriterCmd_BotProbeIncomplete(t *testing.T) {
	srv, replies := captureReplies(t)
	ch := newTestChannel(t, srv.URL, &fakeConfigPermStore{}, uuid.New())
	ch.botOpenID = "" // simulate probe-not-yet-complete

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/addwriter @_user_1",
		Mentions: []mentionInfo{{Key: "@_user_1", OpenID: "ou_alice", Name: "Alice"}},
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "Bot identity not yet resolved")
}

// TestWriterCmd_ReplyToTargetResolution guards M4: the reply-to code path
// must correctly fetch the parent message sender and use it as the target.
func TestWriterCmd_ReplyToTargetResolution(t *testing.T) {
	// Custom server: satisfies token + /im/v1/messages/{id} GET (parent lookup)
	// + any outbound message POST (command reply).
	var replies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/open-apis/im/v1/messages/") {
			// Parent message lookup — return Bob as the sender.
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"items":[{"message_id":"om_parent","msg_type":"text","body":{"content":"{\"text\":\"hi\"}"},"sender":{"id":"ou_bob","id_type":"open_id","sender_type":"user"}}]}}`))
			return
		}
		// Outbound command reply.
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if content, ok := body["content"].(string); ok {
			replies = append(replies, content)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"message_id":"om_reply"}}`))
	}))
	defer srv.Close()

	perm := &fakeConfigPermStore{}
	agentID := uuid.New()
	_ = perm.Grant(context.Background(), &store.ConfigPermission{
		AgentID: agentID, Scope: "group:feishu:oc_grp_1",
		ConfigType: store.ConfigTypeFileWriter, UserID: "ou_alice", Permission: "allow",
	})
	ch := newTestChannel(t, srv.URL, perm, agentID)

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/addwriter",
		ParentID: "om_parent", // reply-to Bob's message
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, replies, "Added")

	got, _ := perm.ListFileWriters(context.Background(), agentID, "group:feishu:oc_grp_1")
	if len(got) != 2 {
		t.Errorf("expected 2 writers after reply-to grant, got %d: %+v", len(got), got)
	}
	foundBob := false
	for _, w := range got {
		if w.UserID == "ou_bob" {
			foundBob = true
			break
		}
	}
	if !foundBob {
		t.Errorf("expected bob to be granted via reply-to, got writers: %+v", got)
	}
}

// TestWriterCmd_NonWriterCannotGrant verifies authorization: when writers
// exist, a non-writer caller must be rejected.
func TestWriterCmd_NonWriterCannotGrant(t *testing.T) {
	srv, replies := captureReplies(t)
	perm := &fakeConfigPermStore{}
	agentID := uuid.New()
	// Seed: alice is already a writer.
	_ = perm.Grant(context.Background(), &store.ConfigPermission{
		AgentID: agentID, Scope: "group:feishu:oc_grp_1",
		ConfigType: store.ConfigTypeFileWriter, UserID: "ou_alice", Permission: "allow",
	})
	ch := newTestChannel(t, srv.URL, perm, agentID)

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_bob",
		ChatType: "group", Content: "/addwriter",
		Mentions: []mentionInfo{{Key: "@_user_1", OpenID: "ou_carol", Name: "Carol"}},
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "Only existing file writers")

	// Verify no new grant was created (still just alice).
	got, _ := perm.ListFileWriters(context.Background(), agentID, "group:feishu:oc_grp_1")
	if len(got) != 1 {
		t.Errorf("expected writers unchanged (1), got %d", len(got))
	}
}

// TestWriterCmd_WriterGrantsViaMention verifies an existing writer can add
// a new user via @mention, and the grant row reflects the new target.
func TestWriterCmd_WriterGrantsViaMention(t *testing.T) {
	srv, replies := captureReplies(t)
	perm := &fakeConfigPermStore{}
	agentID := uuid.New()
	_ = perm.Grant(context.Background(), &store.ConfigPermission{
		AgentID: agentID, Scope: "group:feishu:oc_grp_1",
		ConfigType: store.ConfigTypeFileWriter, UserID: "ou_alice", Permission: "allow",
	})
	ch := newTestChannel(t, srv.URL, perm, agentID)

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/addwriter @_user_1",
		Mentions: []mentionInfo{{Key: "@_user_1", OpenID: "ou_bob", Name: "Bob"}},
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "Added")

	got, _ := perm.ListFileWriters(context.Background(), agentID, "group:feishu:oc_grp_1")
	if len(got) != 2 {
		t.Errorf("expected 2 writers after grant, got %d: %+v", len(got), got)
	}
}

// TestWriterCmd_RemoveLastWriterRejected verifies the last-writer guard.
func TestWriterCmd_RemoveLastWriterRejected(t *testing.T) {
	srv, replies := captureReplies(t)
	perm := &fakeConfigPermStore{}
	agentID := uuid.New()
	_ = perm.Grant(context.Background(), &store.ConfigPermission{
		AgentID: agentID, Scope: "group:feishu:oc_grp_1",
		ConfigType: store.ConfigTypeFileWriter, UserID: "ou_alice", Permission: "allow",
	})
	ch := newTestChannel(t, srv.URL, perm, agentID)

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/removewriter",
		Mentions: []mentionInfo{{Key: "@_user_1", OpenID: "ou_alice", Name: "Alice"}},
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "Cannot remove the last")

	got, _ := perm.ListFileWriters(context.Background(), agentID, "group:feishu:oc_grp_1")
	if len(got) != 1 {
		t.Errorf("expected writers unchanged, got %d", len(got))
	}
}

// TestWriterCmd_ListEmpty verifies /writers on a group with no writers
// returns the instructional "no writers configured" message.
func TestWriterCmd_ListEmpty(t *testing.T) {
	srv, replies := captureReplies(t)
	ch := newTestChannel(t, srv.URL, &fakeConfigPermStore{}, uuid.New())

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/writers",
	}
	handled := ch.maybeHandleWriterCommand(context.Background(), mc)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	assertReplyContains(t, *replies, "No file writers configured")
}

// TestWriterCmd_ListPopulated verifies /writers enumerates existing writers.
func TestWriterCmd_ListPopulated(t *testing.T) {
	srv, replies := captureReplies(t)
	perm := &fakeConfigPermStore{}
	agentID := uuid.New()
	meta, _ := json.Marshal(map[string]string{"displayName": "Alice"})
	_ = perm.Grant(context.Background(), &store.ConfigPermission{
		AgentID: agentID, Scope: "group:feishu:oc_grp_1",
		ConfigType: store.ConfigTypeFileWriter, UserID: "ou_alice", Permission: "allow",
		Metadata: meta,
	})
	ch := newTestChannel(t, srv.URL, perm, agentID)

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "/writers",
	}
	ch.maybeHandleWriterCommand(context.Background(), mc)
	assertReplyContains(t, *replies, "File writers for this group")
	assertReplyContains(t, *replies, "Alice")
}

// TestWriterCmd_NonCommandNotConsumed verifies plain text passes through.
func TestWriterCmd_NonCommandNotConsumed(t *testing.T) {
	srv, _ := captureReplies(t)
	ch := newTestChannel(t, srv.URL, &fakeConfigPermStore{}, uuid.New())

	mc := &messageContext{
		ChatID: "oc_grp_1", MessageID: "om_cmd", SenderID: "ou_alice",
		ChatType: "group", Content: "hello bot",
	}
	if ch.maybeHandleWriterCommand(context.Background(), mc) {
		t.Errorf("plain text must not be consumed as command")
	}
}
