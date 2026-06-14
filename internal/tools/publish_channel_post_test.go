package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeMeowStore implements only the methods the publish tool path touches.
type fakeMeowStore struct {
	store.MeowStore
	ch          *store.MpChannel
	claim       *store.MpContentPost
	claimErr    error
	updateCalls int
}

func (f *fakeMeowStore) GetChannelByHandle(_ context.Context, _ uuid.UUID, _ string) (*store.MpChannel, error) {
	return f.ch, nil
}
func (f *fakeMeowStore) GetChannel(_ context.Context, _, _ uuid.UUID) (*store.MpChannel, error) {
	return f.ch, nil
}
func (f *fakeMeowStore) ClaimPostForPublish(_ context.Context, _, _ uuid.UUID, _ time.Time, _ bool) (*store.MpContentPost, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return f.claim, nil
}
func (f *fakeMeowStore) UpdatePostStatus(_ context.Context, _, _ uuid.UUID, _ string, _ *int64, _ string) error {
	f.updateCalls++
	return nil
}

type fakePoster struct {
	calls   int
	channel string
}

func (p *fakePoster) PublishChannelPost(_ context.Context, channelName, _, _, _ string, _ []channels.PostButton) (int64, error) {
	p.calls++
	p.channel = channelName
	return 555, nil
}

func newToolFixture(t *testing.T) (*PublishChannelPostTool, *fakePoster, *fakeMeowStore) {
	t.Helper()
	root := t.TempDir()
	img := filepath.Join(root, "p.png")
	if err := os.WriteFile(img, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	url := "https://t.me/Bot?startapp"
	bs, _ := json.Marshal([]meow.Button{{Label: "Play now", URL: url}})
	st := &fakeMeowStore{
		ch: &store.MpChannel{
			ID: uuid.New(), TenantID: uuid.New(), Handle: "@TestChan",
			Launched: true, ChatID: func() *string { s := "-100123"; return &s }(), ButtonSet: bs,
		},
		claim: &store.MpContentPost{
			ID: uuid.New(), Status: store.MpPostPublishing,
			KoText: "안녕", EnText: "hi", ImagePath: img, Buttons: bs,
		},
	}
	poster := &fakePoster{}
	tool := NewPublishChannelPostTool(st, poster, "telegram", st.ch.TenantID, []string{root})
	return tool, poster, st
}

func TestPublishChannelPostTool_Execute(t *testing.T) {
	tool, poster, st := newToolFixture(t)

	res := tool.Execute(context.Background(), map[string]any{"handle": "@TestChan", "date": "2026-06-16"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if poster.calls != 1 || poster.channel != "telegram" {
		t.Fatalf("manager not called correctly: calls=%d channel=%q", poster.calls, poster.channel)
	}
	if st.updateCalls != 1 {
		t.Fatalf("expected published persist, got %d update calls", st.updateCalls)
	}
	if !strings.Contains(res.ForLLM, `"message_id":555`) || !strings.Contains(res.ForLLM, "t.me/TestChan/555") {
		t.Fatalf("result missing message id/link: %s", res.ForLLM)
	}
}

func TestPublishChannelPostTool_BadArgs(t *testing.T) {
	tool, poster, _ := newToolFixture(t)
	for _, args := range []map[string]any{
		{"date": "2026-06-16"},                    // missing handle
		{"handle": "@TestChan"},                   // missing date
		{"handle": "@TestChan", "date": "16/6/26"}, // bad date format
	} {
		if res := tool.Execute(context.Background(), args); !res.IsError {
			t.Errorf("expected error for args %v", args)
		}
	}
	if poster.calls != 0 {
		t.Fatal("must not publish on bad args")
	}
}

func TestPublishChannelPostTool_Registration(t *testing.T) {
	tool, _, _ := newToolFixture(t)
	r := NewRegistry()
	r.Register(tool)
	got, ok := r.Get("publish_channel_post")
	if !ok || got.Name() != "publish_channel_post" {
		t.Fatal("publish_channel_post not registered/resolvable")
	}
}
