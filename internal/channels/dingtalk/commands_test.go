package dingtalk

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestParseCommand(t *testing.T) {
	cases := map[string]commandIntent{
		"/new":        cmdNewSession,
		"/reset":      cmdNewSession,
		"/clear":      cmdNewSession,
		"新会话":         cmdNewSession,
		"重新开始":        cmdNewSession,
		"清空对话":        cmdNewSession,
		"  /new  ":    cmdNewSession, // trimmed
		"/stop":       cmdStop,
		"/STOP":       cmdStop, // case-insensitive
		"/stopall":    cmdStopAll,
		"/stop now":   cmdStop, // first token
		"hello":       cmdNone,
		"/newsletter": cmdNone, // not an exact alias
		"新会话啊":        cmdNone, // alias must be exact
		"":            cmdNone,
	}
	for text, want := range cases {
		if got := parseCommand(text); got != want {
			t.Errorf("parseCommand(%q) = %d, want %d", text, got, want)
		}
	}
}

// fakeConfigPerm reports a fixed permission, or an error to exercise fail-closed.
type fakeConfigPerm struct {
	store.ConfigPermissionStore
	allow bool
	err   error
}

func (f fakeConfigPerm) CheckPermission(context.Context, uuid.UUID, string, string, string) (bool, error) {
	return f.allow, f.err
}

type fakeAgentStore struct {
	store.AgentStore
	id uuid.UUID
}

func (f fakeAgentStore) GetByKey(context.Context, string) (*store.AgentData, error) {
	a := &store.AgentData{}
	a.ID = f.id // ID is embedded via BaseModel
	return a, nil
}

func cmdMsg(text string, group bool) *chatbot.BotCallbackDataModel {
	m := &chatbot.BotCallbackDataModel{
		MsgId: "m1", Msgtype: "text", SenderStaffId: "staff-1",
		SessionWebhook: "https://hook.invalid/x",
		Text:           chatbot.BotCallbackDataTextModel{Content: text},
	}
	if group {
		m.ConversationType = conversationTypeGroup
		m.ConversationId = "cid-group"
		m.IsInAtList = true
	} else {
		m.ConversationType = conversationTypeDirect
	}
	return m
}

// A DM /new publishes a reset command and does not run the agent. The reset
// itself is the shared consumer's job; here we only prove the right signal is
// emitted with the right session-routing metadata.
func TestCommand_DMResetPublishesResetSignal(t *testing.T) {
	_, ft, b := startedChannel(t, openPolicy())

	if _, err := ft.deliver(context.Background(), cmdMsg("/new", false)); err != nil {
		t.Fatal(err)
	}
	msg := waitInbound(t, b)

	if msg.Metadata[tools.MetaCommand] != "reset" {
		t.Errorf("MetaCommand = %q, want reset", msg.Metadata[tools.MetaCommand])
	}
	if msg.ChatID != "staff-1" || msg.PeerKind != "direct" {
		t.Errorf("routing = chat %q peer %q", msg.ChatID, msg.PeerKind)
	}
	if msg.Content != "/reset" {
		t.Errorf("Content = %q", msg.Content)
	}
}

func TestCommand_StopAndStopAll(t *testing.T) {
	for text, want := range map[string]string{"/stop": "stop", "/stopall": "stopall"} {
		t.Run(text, func(t *testing.T) {
			_, ft, b := startedChannel(t, openPolicy())
			if _, err := ft.deliver(context.Background(), cmdMsg(text, false)); err != nil {
				t.Fatal(err)
			}
			if got := waitInbound(t, b).Metadata[tools.MetaCommand]; got != want {
				t.Errorf("MetaCommand = %q, want %q", got, want)
			}
		})
	}
}

// A non-command message must fall through to the normal pipeline untouched.
func TestCommand_NonCommandFallsThrough(t *testing.T) {
	_, ft, b := startedChannel(t, openPolicy())
	if _, err := ft.deliver(context.Background(), cmdMsg("hello there", false)); err != nil {
		t.Fatal(err)
	}
	msg := waitInbound(t, b)
	if msg.Metadata[tools.MetaCommand] != "" {
		t.Errorf("a plain message carried a command marker: %q", msg.Metadata[tools.MetaCommand])
	}
	if msg.Content == "" || msg.Content == "/reset" {
		t.Errorf("Content = %q, want the original message", msg.Content)
	}
}

// mayResetGroup is fail-closed: a nil store or a CheckPermission error denies.
// Only an explicit writer=true allows.
func TestMayResetGroup_FailClosed(t *testing.T) {
	agentID := uuid.New()
	in := &inbound{IsGroup: true, SenderID: "staff-1"}
	ctx := context.Background()

	tests := []struct {
		name  string
		agent store.AgentStore
		perm  store.ConfigPermissionStore
		want  bool
	}{
		{"nil perm store", fakeAgentStore{id: agentID}, nil, false},
		{"writer", fakeAgentStore{id: agentID}, fakeConfigPerm{allow: true}, true},
		{"not writer", fakeAgentStore{id: agentID}, fakeConfigPerm{allow: false}, false},
		{"perm check errors", fakeAgentStore{id: agentID}, fakeConfigPerm{err: errors.New("db down")}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := New(Config{ClientID: "k", ClientSecret: "s"}, bus.New(), nil, nil, nil,
				WithConfigPermStore(tc.perm), WithAgentStore(tc.agent))
			if err != nil {
				t.Fatal(err)
			}
			ch.SetAgentID("dt-agent")
			if got := ch.mayResetGroup(ctx, in, "cid-group"); got != tc.want {
				t.Errorf("mayResetGroup = %v, want %v", got, tc.want)
			}
		})
	}
}

// A group /new from a non-writer is refused: no reset signal is published.
func TestCommand_GroupResetDeniedForNonWriter(t *testing.T) {
	msgBus := bus.New()
	ch, err := New(Config{ClientID: "k", ClientSecret: "s", GroupPolicy: "open", DMPolicy: "open"},
		msgBus, nil, nil, nil,
		WithConfigPermStore(fakeConfigPerm{allow: false}), WithAgentStore(fakeAgentStore{id: uuid.New()}))
	if err != nil {
		t.Fatal(err)
	}
	ch.SetAgentID("dt-agent")
	ft := &fakeTransport{}
	ch.newTransport = func(h chatbot.IChatBotMessageHandler) streamTransport {
		ft.handler = h
		return ft
	}
	if err := ch.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })

	if _, err := ft.deliver(context.Background(), cmdMsg("/new", true)); err != nil {
		t.Fatal(err)
	}
	expectNoInbound(t, msgBus)
}

// A group /new from a writer publishes the reset with group routing.
func TestCommand_GroupResetAllowedForWriter(t *testing.T) {
	msgBus := bus.New()
	ch, err := New(Config{ClientID: "k", ClientSecret: "s", GroupPolicy: "open", DMPolicy: "open"},
		msgBus, nil, nil, nil,
		WithConfigPermStore(fakeConfigPerm{allow: true}), WithAgentStore(fakeAgentStore{id: uuid.New()}))
	if err != nil {
		t.Fatal(err)
	}
	ch.SetAgentID("dt-agent")
	ft := &fakeTransport{}
	ch.newTransport = func(h chatbot.IChatBotMessageHandler) streamTransport {
		ft.handler = h
		return ft
	}
	if err := ch.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })

	if _, err := ft.deliver(context.Background(), cmdMsg("/new", true)); err != nil {
		t.Fatal(err)
	}
	msg := waitInbound(t, msgBus)
	if msg.Metadata[tools.MetaCommand] != "reset" {
		t.Errorf("MetaCommand = %q", msg.Metadata[tools.MetaCommand])
	}
	if msg.PeerKind != "group" || msg.ChatID != "cid-group" {
		t.Errorf("routing = peer %q chat %q", msg.PeerKind, msg.ChatID)
	}
}
