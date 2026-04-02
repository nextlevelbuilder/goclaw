package channels

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeHealthChannel struct {
	*BaseChannel
	startErr        error
	selfMarkFailure bool
}

func newFakeHealthChannel(name string) *fakeHealthChannel {
	return &fakeHealthChannel{
		BaseChannel: NewBaseChannel(name, bus.New(), nil),
	}
}

func (c *fakeHealthChannel) Start(context.Context) error {
	if c.startErr != nil {
		if c.selfMarkFailure {
			info := ClassifyChannelError(c.startErr)
			c.MarkFailed(info.Summary, info.Detail, info.Kind, info.Retryable)
		}
		return c.startErr
	}
	c.SetRunning(true)
	return nil
}

func (c *fakeHealthChannel) Stop(context.Context) error {
	c.SetRunning(false)
	return nil
}

func (c *fakeHealthChannel) Send(context.Context, bus.OutboundMessage) error { return nil }

type loaderHealthChannel struct {
	base            *BaseChannel
	startErr        error
	selfMarkFailure bool
}

func newLoaderHealthChannel(name, channelType string) *loaderHealthChannel {
	base := NewBaseChannel(name, bus.New(), nil)
	base.SetType(channelType)
	return &loaderHealthChannel{base: base}
}

func (c *loaderHealthChannel) Name() string                                    { return c.base.Name() }
func (c *loaderHealthChannel) Type() string                                    { return c.base.Type() }
func (c *loaderHealthChannel) IsRunning() bool                                 { return c.base.IsRunning() }
func (c *loaderHealthChannel) IsAllowed(senderID string) bool                  { return c.base.IsAllowed(senderID) }
func (c *loaderHealthChannel) Send(context.Context, bus.OutboundMessage) error { return nil }
func (c *loaderHealthChannel) Stop(context.Context) error {
	c.base.MarkStopped("Stopped")
	return nil
}
func (c *loaderHealthChannel) Start(context.Context) error {
	if c.startErr != nil {
		if c.selfMarkFailure {
			info := ClassifyChannelError(c.startErr)
			c.base.MarkFailed(info.Summary, info.Detail, info.Kind, info.Retryable)
		}
		return c.startErr
	}
	c.base.MarkHealthy("Connected")
	return nil
}
func (c *loaderHealthChannel) SetType(channelType string) { c.base.SetType(channelType) }
func (c *loaderHealthChannel) HealthSnapshot() ChannelHealth {
	return c.base.HealthSnapshot()
}
func (c *loaderHealthChannel) MarkFailed(summary, detail string, kind ChannelFailureKind, retryable bool) {
	c.base.MarkFailed(summary, detail, kind, retryable)
}

func TestManagerGetStatusIncludesPreRegistrationFailures(t *testing.T) {
	mgr := NewManager(bus.New())

	mgr.RecordFailureForType("telegram-main", TypeTelegram, "", errors.New(`telego: getMe: api: 401 "Unauthorized"`))

	raw, ok := mgr.GetStatus()["telegram-main"]
	if !ok {
		t.Fatal("expected failed instance in status map")
	}
	status, ok := raw.(ChannelHealth)
	if !ok {
		t.Fatalf("expected ChannelHealth entry, got %T", raw)
	}
	if status.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed state, got %q", status.State)
	}
	if status.FailureKind != ChannelFailureKindAuth {
		t.Fatalf("expected auth failure kind, got %q", status.FailureKind)
	}
	if status.Remediation == nil {
		t.Fatal("expected remediation metadata")
	}
	if status.Remediation.Code != ChannelRemediationCodeOpenCredentials {
		t.Fatalf("expected open_credentials remediation, got %q", status.Remediation.Code)
	}
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("expected consecutive failures to be 1, got %d", status.ConsecutiveFailures)
	}
}

func TestClassifyChannelErrorSanitizesSensitiveDetails(t *testing.T) {
	info := ClassifyChannelError(errors.New(`telego: getMe: api: 401 "Unauthorized" token=123456:ABC host=api.telegram.org`))
	if info.Detail == "" {
		t.Fatal("expected sanitized detail")
	}
	for _, secret := range []string{"123456:ABC", "api.telegram.org", "Unauthorized"} {
		if contains := strings.Contains(info.Detail, secret); contains {
			t.Fatalf("expected detail to omit %q, got %q", secret, info.Detail)
		}
	}
}

func TestManagerStartAllPromotesHealthyChannels(t *testing.T) {
	mgr := NewManager(bus.New())
	channel := newFakeHealthChannel("telegram-main")
	mgr.RegisterChannel("telegram-main", channel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll returned error: %v", err)
	}

	raw := mgr.GetStatus()["telegram-main"]
	status, ok := raw.(ChannelHealth)
	if !ok {
		t.Fatalf("expected ChannelHealth entry, got %T", raw)
	}
	if !status.Running {
		t.Fatal("expected running=true")
	}
	if status.State != ChannelHealthStateHealthy {
		t.Fatalf("expected healthy state, got %q", status.State)
	}
	if status.LastHealthyAt.IsZero() {
		t.Fatal("expected last healthy timestamp to be set")
	}
	if status.Remediation != nil {
		t.Fatalf("expected no remediation for healthy channel, got %+v", status.Remediation)
	}
}

func TestManagerStartAllCapturesStartupFailures(t *testing.T) {
	mgr := NewManager(bus.New())
	channel := newFakeHealthChannel("telegram-main")
	channel.startErr = errors.New(`telego: getUpdates: api: 401 "Unauthorized"`)
	mgr.RegisterChannel("telegram-main", channel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll returned error: %v", err)
	}

	raw := mgr.GetStatus()["telegram-main"]
	status, ok := raw.(ChannelHealth)
	if !ok {
		t.Fatalf("expected ChannelHealth entry, got %T", raw)
	}
	if status.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed state, got %q", status.State)
	}
	if status.FailureKind != ChannelFailureKindAuth {
		t.Fatalf("expected auth failure kind, got %q", status.FailureKind)
	}
	if status.FailureCount < 1 {
		t.Fatalf("expected failure count to increment, got %d", status.FailureCount)
	}
	if status.Remediation == nil {
		t.Fatal("expected remediation metadata")
	}
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("expected consecutive failures to be 1, got %d", status.ConsecutiveFailures)
	}
}

func TestManagerStartAllDoesNotDoubleCountSelfReportedStartupFailure(t *testing.T) {
	mgr := NewManager(bus.New())
	channel := newFakeHealthChannel("telegram-main")
	channel.startErr = errors.New(`telego: getUpdates: api: 401 "Unauthorized"`)
	channel.selfMarkFailure = true
	mgr.RegisterChannel("telegram-main", channel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll returned error: %v", err)
	}

	status := mgr.GetStatus()["telegram-main"].(ChannelHealth)
	if status.FailureCount != 1 {
		t.Fatalf("expected one startup failure to be recorded, got %d", status.FailureCount)
	}
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure, got %d", status.ConsecutiveFailures)
	}
}

func TestInstanceLoaderAutoStartPreservesRegisteredFailureState(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	loader := NewInstanceLoader(nil, nil, mgr, msgBus, nil)
	startErr := errors.New(`telego: getUpdates: api: 401 "Unauthorized"`)

	loader.RegisterFactory(TypeTelegram, func(name string, _, _ json.RawMessage, _ *bus.MessageBus, _ store.PairingStore) (Channel, error) {
		ch := newLoaderHealthChannel(name, TypeTelegram)
		ch.startErr = startErr
		return ch, nil
	})

	loader.mu.Lock()
	err := loader.loadInstance(context.Background(), store.ChannelInstanceData{
		Name:        "telegram-auto",
		ChannelType: TypeTelegram,
	}, true)
	loader.mu.Unlock()
	if err != nil {
		t.Fatalf("loadInstance returned error: %v", err)
	}

	status := mgr.GetStatus()["telegram-auto"].(ChannelHealth)
	if status.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed state, got %q", status.State)
	}
	if status.FailureKind != ChannelFailureKindAuth {
		t.Fatalf("expected auth failure kind, got %q", status.FailureKind)
	}
	if status.Remediation == nil {
		t.Fatal("expected remediation metadata")
	}
}

func TestInstanceLoaderAutoStartDoesNotDoubleCountSelfReportedFailure(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	loader := NewInstanceLoader(nil, nil, mgr, msgBus, nil)

	loader.RegisterFactory(TypeTelegram, func(name string, _, _ json.RawMessage, _ *bus.MessageBus, _ store.PairingStore) (Channel, error) {
		ch := newLoaderHealthChannel(name, TypeTelegram)
		ch.startErr = errors.New(`telego: getUpdates: api: 401 "Unauthorized"`)
		ch.selfMarkFailure = true
		return ch, nil
	})

	loader.mu.Lock()
	err := loader.loadInstance(context.Background(), store.ChannelInstanceData{
		Name:        "telegram-auto-self",
		ChannelType: TypeTelegram,
	}, true)
	loader.mu.Unlock()
	if err != nil {
		t.Fatalf("loadInstance returned error: %v", err)
	}

	status := mgr.GetStatus()["telegram-auto-self"].(ChannelHealth)
	if status.FailureCount != 1 {
		t.Fatalf("expected one startup failure to be recorded, got %d", status.FailureCount)
	}
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure, got %d", status.ConsecutiveFailures)
	}
}

func TestInstanceLoaderTracksPreRegistrationFailuresForCleanup(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	loader := NewInstanceLoader(nil, nil, mgr, msgBus, nil)

	loader.RegisterFactory(TypeTelegram, func(string, json.RawMessage, json.RawMessage, *bus.MessageBus, store.PairingStore) (Channel, error) {
		return nil, errors.New("token is required: 123456:ABC")
	})

	loader.mu.Lock()
	err := loader.loadInstance(context.Background(), store.ChannelInstanceData{
		Name:        "telegram-failed",
		ChannelType: TypeTelegram,
	}, false)
	loader.mu.Unlock()
	if err == nil {
		t.Fatal("expected factory failure")
	}

	if _, ok := loader.LoadedNames()["telegram-failed"]; !ok {
		t.Fatal("expected failed instance to remain tracked for cleanup")
	}

	status := mgr.GetStatus()["telegram-failed"].(ChannelHealth)
	if status.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed state, got %q", status.State)
	}
	if status.Detail == "" || status.Detail == "token is required: 123456:ABC" {
		t.Fatalf("expected sanitized detail, got %q", status.Detail)
	}

	loader.Stop(context.Background())

	if _, ok := loader.LoadedNames()["telegram-failed"]; ok {
		t.Fatal("expected cleanup to clear loader tracking")
	}
	if _, ok := mgr.GetStatus()["telegram-failed"]; ok {
		t.Fatal("expected cleanup to remove failed status entry")
	}
}

func TestInstanceLoaderCleansEarlyExitFailuresOnStop(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		configure   func(*InstanceLoader)
		wantSummary string
		wantKind    ChannelFailureKind
	}{
		{
			name:        "unsupported-channel",
			channelType: "unsupported",
			wantSummary: "Unsupported channel type",
			wantKind:    ChannelFailureKindConfig,
		},
		{
			name:        "telegram-missing-creds",
			channelType: TypeTelegram,
			configure: func(loader *InstanceLoader) {
				loader.RegisterFactory(TypeTelegram, func(string, json.RawMessage, json.RawMessage, *bus.MessageBus, store.PairingStore) (Channel, error) {
					return nil, nil
				})
			},
			wantSummary: "Missing credentials",
			wantKind:    ChannelFailureKindConfig,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgBus := bus.New()
			mgr := NewManager(msgBus)
			loader := NewInstanceLoader(nil, nil, mgr, msgBus, nil)
			if tc.configure != nil {
				tc.configure(loader)
			}

			loader.mu.Lock()
			err := loader.loadInstance(context.Background(), store.ChannelInstanceData{
				Name:        tc.name,
				ChannelType: tc.channelType,
			}, false)
			loader.mu.Unlock()
			if err != nil {
				t.Fatalf("loadInstance returned error: %v", err)
			}

			if _, ok := loader.LoadedNames()[tc.name]; !ok {
				t.Fatal("expected failed instance to remain tracked for cleanup")
			}

			status := mgr.GetStatus()[tc.name].(ChannelHealth)
			if status.State != ChannelHealthStateFailed {
				t.Fatalf("expected failed state, got %q", status.State)
			}
			if status.Summary != tc.wantSummary {
				t.Fatalf("expected summary %q, got %q", tc.wantSummary, status.Summary)
			}
			if status.FailureKind != tc.wantKind {
				t.Fatalf("expected failure kind %q, got %q", tc.wantKind, status.FailureKind)
			}

			loader.Stop(context.Background())

			if _, ok := loader.LoadedNames()[tc.name]; ok {
				t.Fatal("expected cleanup to clear loader tracking")
			}
			if _, ok := mgr.GetStatus()[tc.name]; ok {
				t.Fatal("expected cleanup to remove failed status entry")
			}
		})
	}
}

func TestBuildChannelRemediationUsesRealOperatorTargets(t *testing.T) {
	t.Helper()

	cases := []struct {
		name       string
		snapshot   ChannelHealth
		wantCode   ChannelRemediationCode
		wantTarget ChannelRemediationTarget
	}{
		{
			name: "telegram auth opens credentials",
			snapshot: NewFailedChannelHealthForType(
				TypeTelegram,
				"",
				errors.New(`telego: getMe: api: 401 "Unauthorized"`),
			),
			wantCode:   ChannelRemediationCodeOpenCredentials,
			wantTarget: ChannelRemediationTargetCredentials,
		},
		{
			name: "zalo personal auth reauthenticates",
			snapshot: NewFailedChannelHealthForType(
				TypeZaloPersonal,
				"",
				errors.New(`session expired: unauthorized`),
			),
			wantCode:   ChannelRemediationCodeReauth,
			wantTarget: ChannelRemediationTargetReauth,
		},
		{
			name: "missing credentials opens credentials",
			snapshot: NewChannelHealthForType(
				TypeSlack,
				ChannelHealthStateFailed,
				"Missing credentials",
				"Set channels.slack.bot_token in config.",
				ChannelFailureKindConfig,
				false,
			),
			wantCode:   ChannelRemediationCodeOpenCredentials,
			wantTarget: ChannelRemediationTargetCredentials,
		},
		{
			name: "invalid proxy opens advanced",
			snapshot: NewChannelHealthForType(
				TypeTelegram,
				ChannelHealthStateFailed,
				"Configuration is invalid",
				"invalid proxy URL",
				ChannelFailureKindConfig,
				false,
			),
			wantCode:   ChannelRemediationCodeOpenAdvanced,
			wantTarget: ChannelRemediationTargetAdvanced,
		},
		{
			name: "network timeout checks network",
			snapshot: NewFailedChannelHealthForType(
				TypeTelegram,
				"",
				errors.New(`dial tcp 127.0.0.1:443: i/o timeout`),
			),
			wantCode:   ChannelRemediationCodeCheckNetwork,
			wantTarget: ChannelRemediationTargetDetails,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.snapshot = mergeChannelHealth(ChannelHealth{}, tc.snapshot)
			if tc.snapshot.Remediation == nil {
				t.Fatal("expected remediation")
			}
			if tc.snapshot.Remediation.Code != tc.wantCode {
				t.Fatalf("expected remediation code %q, got %q", tc.wantCode, tc.snapshot.Remediation.Code)
			}
			if tc.snapshot.Remediation.Target != tc.wantTarget {
				t.Fatalf("expected remediation target %q, got %q", tc.wantTarget, tc.snapshot.Remediation.Target)
			}
		})
	}
}

func TestMergeChannelHealthTracksFailureTimelineAndRecovery(t *testing.T) {
	firstFailedAt := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	secondFailedAt := firstFailedAt.Add(2 * time.Minute)
	recoveredAt := secondFailedAt.Add(3 * time.Minute)
	thirdFailedAt := recoveredAt.Add(4 * time.Minute)

	first := NewFailedChannelHealthForType(TypeTelegram, "", errors.New(`telego: getUpdates: api: 401 "Unauthorized"`))
	first.CheckedAt = firstFailedAt
	first = mergeChannelHealth(ChannelHealth{}, first)

	second := NewFailedChannelHealthForType(TypeTelegram, "", errors.New(`telego: getUpdates: api: 401 "Unauthorized"`))
	second.CheckedAt = secondFailedAt
	second = mergeChannelHealth(first, second)

	if second.FailureCount != 2 {
		t.Fatalf("expected cumulative failure count 2, got %d", second.FailureCount)
	}
	if second.ConsecutiveFailures != 2 {
		t.Fatalf("expected consecutive failures 2, got %d", second.ConsecutiveFailures)
	}
	if !second.FirstFailedAt.Equal(firstFailedAt) {
		t.Fatalf("expected first failed at %v, got %v", firstFailedAt, second.FirstFailedAt)
	}
	if !second.LastFailedAt.Equal(secondFailedAt) {
		t.Fatalf("expected last failed at %v, got %v", secondFailedAt, second.LastFailedAt)
	}

	recovered := NewChannelHealthForType(TypeTelegram, ChannelHealthStateHealthy, "Connected", "", ChannelFailureKindUnknown, false)
	recovered.CheckedAt = recoveredAt
	recovered = mergeChannelHealth(second, recovered)

	if recovered.ConsecutiveFailures != 0 {
		t.Fatalf("expected consecutive failures reset, got %d", recovered.ConsecutiveFailures)
	}
	if recovered.Remediation != nil {
		t.Fatalf("expected remediation to clear after recovery, got %+v", recovered.Remediation)
	}
	if !recovered.LastHealthyAt.Equal(recoveredAt) {
		t.Fatalf("expected last healthy at %v, got %v", recoveredAt, recovered.LastHealthyAt)
	}
	if recovered.FailureCount != 2 {
		t.Fatalf("expected cumulative failure count to be preserved, got %d", recovered.FailureCount)
	}

	third := NewFailedChannelHealthForType(TypeTelegram, "", errors.New(`telego: getUpdates: api: 401 "Unauthorized"`))
	third.CheckedAt = thirdFailedAt
	third = mergeChannelHealth(recovered, third)

	if third.ConsecutiveFailures != 1 {
		t.Fatalf("expected new failure streak to start at 1, got %d", third.ConsecutiveFailures)
	}
	if !third.FirstFailedAt.Equal(thirdFailedAt) {
		t.Fatalf("expected first failed timestamp to reset to latest incident start, got %v", third.FirstFailedAt)
	}
	if third.FailureCount != 3 {
		t.Fatalf("expected cumulative failure count 3, got %d", third.FailureCount)
	}
}
