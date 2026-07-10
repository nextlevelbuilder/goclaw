package cmd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestCronJobHandlerInjectsPayloadCredentialUserID(t *testing.T) {
	wantCredentialUserID := "tenant-user-123"
	var gotCredentialUserID string

	sched := scheduler.NewScheduler(
		scheduler.DefaultLanes(),
		scheduler.QueueConfig{
			Mode:          scheduler.QueueModeQueue,
			Cap:           1,
			Drop:          scheduler.DropOld,
			DebounceMs:    0,
			MaxConcurrent: 1,
		},
		func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
			gotCredentialUserID = store.CredentialUserIDFromContext(ctx)
			return &agent.RunResult{Content: "ok"}, nil
		},
	)
	defer sched.Stop()

	handler := makeCronJobHandler(
		sched,
		nil,
		&config.Config{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	result, err := handler(&store.CronJob{
		ID:        uuid.NewString(),
		TenantID:  uuid.New(),
		Name:      "credentialed-report",
		AgentID:   "reporter",
		UserID:    "group:telegram:-100123",
		Stateless: true,
		Payload: store.CronPayload{
			Kind:             "agent_turn",
			Message:          "run gh issue list",
			CredentialUserID: wantCredentialUserID,
		},
	})
	if err != nil {
		t.Fatalf("cron handler returned error: %v", err)
	}
	if result == nil || result.Content != "ok" {
		t.Fatalf("cron result = %#v, want content ok", result)
	}
	if gotCredentialUserID != wantCredentialUserID {
		t.Fatalf("credential user ID in scheduled context = %q, want %q", gotCredentialUserID, wantCredentialUserID)
	}
}

func TestCronJobHandlerResolvesGroupDisplayTitle(t *testing.T) {
	var got agent.RunRequest
	sched := scheduler.NewScheduler(
		scheduler.DefaultLanes(),
		scheduler.QueueConfig{Mode: scheduler.QueueModeQueue, Cap: 1, MaxConcurrent: 1},
		func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
			got = req
			return &agent.RunResult{Content: "ok"}, nil
		},
	)
	defer sched.Stop()
	msgBus := bus.New()
	defer msgBus.Close()
	manager := channels.NewManager(nil)
	manager.RegisterChannel("discord-main", cronDisplayTitleChannel{consumerTestChannel: consumerTestChannel{name: "discord-main", channelType: channels.TypeDiscord}, title: "launch-thread / product-planning"})

	handler := makeCronJobHandler(sched, msgBus, &config.Config{}, manager, nil, nil, nil, nil, nil)
	if _, err := handler(&store.CronJob{
		ID:             uuid.NewString(),
		TenantID:       uuid.New(),
		Name:           "thread-report",
		AgentID:        "reporter",
		UserID:         "guild:guild-1:user:user-1",
		Deliver:        true,
		DeliverChannel: "discord-main",
		DeliverTo:      "thread-1",
		Payload:        store.CronPayload{Kind: "agent_turn", Message: "report"},
	}); err != nil {
		t.Fatalf("cron handler: %v", err)
	}
	if got.ChatID != "thread-1" {
		t.Fatalf("chat ID = %q, want stable thread ID", got.ChatID)
	}
	if got.ChatTitle != "launch-thread / product-planning" {
		t.Fatalf("chat title = %q, want qualified title", got.ChatTitle)
	}
}

type cronDisplayTitleChannel struct {
	consumerTestChannel
	title string
}

func (c cronDisplayTitleChannel) ResolveGroupDisplayTitle(context.Context, string) (string, error) {
	return c.title, nil
}

func TestCronOutputContainsNoReplySentinel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "exact", in: "NO_REPLY", want: true},
		{name: "prefix explanation", in: "NO_REPLY - nothing to report", want: true},
		{name: "suffix", in: "No relevant update. NO_REPLY", want: true},
		{name: "terminal glued punctuation", in: "This is directed at Bảo Ly Content, not me. I should stay silent.NO_REPLY", want: true},
		{name: "standalone suffix after space", in: "This is not for me. NO_REPLY", want: true},
		{name: "mid sentence", in: "No changes found. NO_REPLY for this run.", want: true},
		{name: "lowercase", in: "no_reply", want: true},
		{name: "decorative underscore", in: "NO_REPLY_", want: true},
		{name: "glued suffix", in: "NO_REPLYING", want: false},
		{name: "glued prefix", in: "XNO_REPLY", want: false},
		{name: "empty", in: "", want: false},
		{name: "unrelated", in: "no reply needed", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cronOutputContainsNoReplySentinel(tt.in); got != tt.want {
				t.Fatalf("cronOutputContainsNoReplySentinel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestCronJobHandlerSuppressesNoReplyDelivery(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantPublish bool
	}{
		{name: "normal content", content: "daily report ready", wantPublish: true},
		{name: "exact no reply", content: "NO_REPLY", wantPublish: false},
		{name: "suffix no reply", content: "No relevant update. NO_REPLY", wantPublish: false},
		{name: "prefix no reply", content: "NO_REPLY - nothing to report", wantPublish: false},
		{name: "decorative underscore no reply", content: "NO_REPLY_", wantPublish: false},
		{name: "glued token still delivers", content: "NO_REPLYING is a different word", wantPublish: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb := bus.New()
			defer mb.Close()

			sched := scheduler.NewScheduler(
				scheduler.DefaultLanes(),
				scheduler.QueueConfig{
					Mode:          scheduler.QueueModeQueue,
					Cap:           1,
					Drop:          scheduler.DropOld,
					DebounceMs:    0,
					MaxConcurrent: 1,
				},
				func(context.Context, agent.RunRequest) (*agent.RunResult, error) {
					return &agent.RunResult{Content: tt.content}, nil
				},
			)
			defer sched.Stop()

			handler := makeCronJobHandler(
				sched,
				mb,
				&config.Config{},
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
			)

			result, err := handler(&store.CronJob{
				ID:             uuid.NewString(),
				TenantID:       uuid.New(),
				Name:           "delivery-report",
				AgentID:        "reporter",
				UserID:         "user-1",
				Stateless:      true,
				Deliver:        true,
				DeliverChannel: "telegram",
				DeliverTo:      "chat-1",
				Payload: store.CronPayload{
					Kind:    "agent_turn",
					Message: "daily report",
				},
			})
			if err != nil {
				t.Fatalf("cron handler returned error: %v", err)
			}
			if result == nil || result.Content != tt.content {
				t.Fatalf("cron result = %#v, want content %q", result, tt.content)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			got, ok := mb.SubscribeOutbound(ctx)
			if !tt.wantPublish {
				if ok {
					t.Fatalf("unexpected outbound message: %#v", got)
				}
				return
			}
			if !ok {
				t.Fatal("expected outbound message")
			}
			if got.Content != tt.content || got.Channel != "telegram" || got.ChatID != "chat-1" {
				t.Fatalf("outbound message = %#v, want channel telegram chat chat-1 content %q", got, tt.content)
			}
		})
	}
}

// fakeCronSessionStore records Reset calls. The embedded nil SessionStore
// satisfies the interface; the cron handler only calls Reset/Save.
type fakeCronSessionStore struct {
	store.SessionStore
	resetCount int
}

func (f *fakeCronSessionStore) Reset(context.Context, string)      { f.resetCount++ }
func (f *fakeCronSessionStore) Save(context.Context, string) error { return nil }

// A stateless cron run must start fresh by clearing BOTH the goclaw session
// store and the Claude CLI on-disk session; a stateful run must keep both.
func TestCronJobHandler_StatelessResetsSession(t *testing.T) {
	cases := []struct {
		name      string
		stateless bool
		wantReset bool
	}{
		{name: "stateless resets both layers", stateless: true, wantReset: true},
		{name: "stateful keeps session", stateless: false, wantReset: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cliResetKeys []string
			orig := cronCLISessionReset
			cronCLISessionReset = func(_, key string) { cliResetKeys = append(cliResetKeys, key) }
			defer func() { cronCLISessionReset = orig }()

			fakeStore := &fakeCronSessionStore{}

			sched := scheduler.NewScheduler(
				scheduler.DefaultLanes(),
				scheduler.QueueConfig{
					Mode:          scheduler.QueueModeQueue,
					Cap:           1,
					Drop:          scheduler.DropOld,
					DebounceMs:    0,
					MaxConcurrent: 1,
				},
				func(context.Context, agent.RunRequest) (*agent.RunResult, error) {
					return &agent.RunResult{Content: "ok"}, nil
				},
			)
			defer sched.Stop()

			handler := makeCronJobHandler(sched, nil, &config.Config{}, nil, fakeStore, nil, nil, nil, nil)

			if _, err := handler(&store.CronJob{
				ID:        uuid.NewString(),
				TenantID:  uuid.New(),
				Name:      "j",
				AgentID:   "reporter",
				UserID:    "user-1",
				Stateless: tc.stateless,
				Payload:   store.CronPayload{Kind: "agent_turn", Message: "m"},
			}); err != nil {
				t.Fatalf("cron handler error: %v", err)
			}

			if gotStore := fakeStore.resetCount > 0; gotStore != tc.wantReset {
				t.Errorf("session store reset called=%v, want %v", gotStore, tc.wantReset)
			}
			if gotCLI := len(cliResetKeys) > 0; gotCLI != tc.wantReset {
				t.Errorf("CLI session reset called=%v, want %v", gotCLI, tc.wantReset)
			}
		})
	}
}

// fakeTenantStore implements only GetTenant; embedding the interface satisfies
// the rest (calling any other method would nil-panic, which none of these tests do).
type fakeTenantStore struct {
	store.TenantStore
	byID map[uuid.UUID]*store.TenantData
	err  error
}

func (f *fakeTenantStore) GetTenant(_ context.Context, id uuid.UUID) (*store.TenantData, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

func TestCronTenantContext_InjectsSlugForNonMasterTenant(t *testing.T) {
	tid := uuid.Must(uuid.NewV7())
	ts := &fakeTenantStore{byID: map[uuid.UUID]*store.TenantData{
		tid: {ID: tid, Slug: "family-pilot"},
	}}

	ctx := cronTenantContext(context.Background(), ts, tid)

	if got := store.TenantIDFromContext(ctx); got != tid {
		t.Errorf("tenant id = %v, want %v", got, tid)
	}
	// The slug is what tenant-scoped skills-store/workspace paths key off; without
	// it a cron agent turn sees none of its tenant's managed skills.
	if got := store.TenantSlugFromContext(ctx); got != "family-pilot" {
		t.Errorf("tenant slug = %q, want %q (skills-store would resolve to the wrong dir)", got, "family-pilot")
	}
}

func TestCronTenantContext_MasterTenantNeedsNoSlug(t *testing.T) {
	// Master tenant paths resolve to the base dir regardless of slug; the store
	// must not even be consulted.
	ts := &fakeTenantStore{err: fmt.Errorf("GetTenant must not be called for master")}
	ctx := cronTenantContext(context.Background(), ts, store.MasterTenantID)
	if got := store.TenantIDFromContext(ctx); got != store.MasterTenantID {
		t.Errorf("tenant id = %v, want master", got)
	}
}

func TestCronTenantContext_NilStore_TenantIDOnly(t *testing.T) {
	tid := uuid.Must(uuid.NewV7())
	ctx := cronTenantContext(context.Background(), nil, tid)
	if got := store.TenantIDFromContext(ctx); got != tid {
		t.Errorf("tenant id = %v, want %v", got, tid)
	}
	if got := store.TenantSlugFromContext(ctx); got != "" {
		t.Errorf("slug = %q, want empty when store is nil", got)
	}
}

func TestCronTenantContext_LookupError_FallsBackToIDOnly(t *testing.T) {
	tid := uuid.Must(uuid.NewV7())
	ts := &fakeTenantStore{err: fmt.Errorf("db down")}
	ctx := cronTenantContext(context.Background(), ts, tid)
	if got := store.TenantSlugFromContext(ctx); got != "" {
		t.Errorf("slug = %q, want empty on lookup error", got)
	}
	if got := store.TenantIDFromContext(ctx); got != tid {
		t.Errorf("tenant id = %v, want %v (must still scope by id)", got, tid)
	}
}
