package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// chatScopedTaskStore serves ListByParent and records that it was asked. The
// embedded interface is nil on purpose: if the implementation reaches for
// ListBySession — the scope this deliberately moved away from — the test panics
// instead of quietly passing.
type chatScopedTaskStore struct {
	store.SubagentTaskStore
	rows       []store.SubagentTaskData
	listCalls  int
	askedAgent uuid.UUID
}

func (s *chatScopedTaskStore) ListByParent(_ context.Context, rootAgentID uuid.UUID, _ string) ([]store.SubagentTaskData, error) {
	s.listCalls++
	s.askedAgent = rootAgentID
	return s.rows, nil
}

func delegationRow(chatID, sessionKey, target string, created time.Time) store.SubagentTaskData {
	return store.SubagentTaskData{
		BaseModel:    store.BaseModel{ID: uuid.New(), CreatedAt: created},
		Status:       TaskStatusCompleted,
		SessionKey:   &sessionKey,
		OriginChatID: &chatID,
		Metadata: map[string]any{
			asyncCompletionKindKey:      asyncCompletionKindDelegate,
			delegateCompletionTargetKey: target,
		},
	}
}

func spawnRow(chatID string) store.SubagentTaskData {
	return store.SubagentTaskData{
		BaseModel:    store.BaseModel{ID: uuid.New(), CreatedAt: time.Unix(1, 0)},
		Status:       TaskStatusCompleted,
		OriginChatID: &chatID,
		Metadata:     map[string]any{asyncCompletionKindKey: asyncCompletionKindSubagent},
	}
}

func listCtx(chatID string) context.Context {
	ctx := store.WithTenantID(context.Background(), uuid.New())
	ctx = store.WithAgentID(ctx, uuid.New())
	return WithToolChatID(ctx, chatID)
}

func decodeList(t *testing.T, res *Result) []map[string]any {
	t.Helper()
	if res == nil || res.IsError {
		t.Fatalf("list result = %#v", res)
	}
	var payload struct {
		Delegations []map[string]any `json:"delegations"`
		Count       int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(res.ForLLM), &payload); err != nil {
		t.Fatalf("decode %q: %v", res.ForLLM, err)
	}
	if payload.Count != len(payload.Delegations) {
		t.Errorf("count %d disagrees with %d listed", payload.Count, len(payload.Delegations))
	}
	return payload.Delegations
}

func newListTool(rows []store.SubagentTaskData) (*DelegateTool, *chatScopedTaskStore) {
	s := &chatScopedTaskStore{rows: rows}
	tool := NewDelegateTool(nil, noopAgentCRUD{}, nil, nil)
	tool.SetTaskStore(s)
	return tool, s
}

// The chat is the unit of visibility: a delegation started in one chat is not
// carried into another chat with the same agent, and spawned subagents sharing
// the table are not delegations.
func TestDelegateListIsScopedToItsChat(t *testing.T) {
	tool, _ := newListTool([]store.SubagentTaskData{
		delegationRow("chat-a", "session-1", "brain", time.Unix(200, 0)),
		delegationRow("chat-b", "session-1", "brain", time.Unix(300, 0)),
		spawnRow("chat-a"),
	})

	got := decodeList(t, tool.executeListCompletions(listCtx("chat-a")))
	if len(got) != 1 {
		t.Fatalf("listed %d entries, want only this chat's delegation: %v", len(got), got)
	}
	if got[0]["agent"] != "brain" || got[0]["status"] != TaskStatusCompleted {
		t.Errorf("entry = %v", got[0])
	}
	if got[0]["delegation_id"] == "" || got[0]["created_at"] == "" {
		t.Errorf("entry is missing the fields needed to re-acquire a handle: %v", got[0])
	}
}

// The case the whole action exists for. Deferring long work, clearing the
// session and coming back to ask for status is ordinary use; scoping by session
// key would return nothing exactly then.
func TestDelegateListSurvivesSessionReset(t *testing.T) {
	tool, _ := newListTool([]store.SubagentTaskData{
		delegationRow("chat-a", "session-before-reset", "brain", time.Unix(100, 0)),
	})

	// Same chat, a session that did not exist when the delegation was created.
	ctx := WithToolSessionKey(listCtx("chat-a"), "session-after-reset")
	got := decodeList(t, tool.executeListCompletions(ctx))
	if len(got) != 1 {
		t.Fatalf("listed %d entries after a session reset; the delegation is still "+
			"in this chat and must remain recoverable", len(got))
	}
}

// Without a chat there is nothing to scope to, and listing everything this agent
// ever did would cross the boundary the predicate exists to draw.
func TestDelegateListRefusesWithoutChat(t *testing.T) {
	tool, s := newListTool([]store.SubagentTaskData{
		delegationRow("chat-a", "session-1", "brain", time.Unix(100, 0)),
	})

	ctx := store.WithAgentID(store.WithTenantID(context.Background(), uuid.New()), uuid.New())
	res := tool.executeListCompletions(ctx)
	if res == nil || !res.IsError {
		t.Fatalf("list without a chat returned %#v", res)
	}
	if s.listCalls != 0 {
		t.Errorf("store was queried %d times despite having no chat to scope to", s.listCalls)
	}
}

func TestDelegateListStopsAtItsLimit(t *testing.T) {
	rows := make([]store.SubagentTaskData, 0, delegateListLimit+5)
	for i := range delegateListLimit + 5 {
		rows = append(rows, delegationRow("chat-a", "session-1", fmt.Sprintf("agent-%d", i), time.Unix(int64(i), 0)))
	}
	tool, _ := newListTool(rows)

	if got := decodeList(t, tool.executeListCompletions(listCtx("chat-a"))); len(got) != delegateListLimit {
		t.Fatalf("listed %d entries, want the cap of %d", len(got), delegateListLimit)
	}
}
