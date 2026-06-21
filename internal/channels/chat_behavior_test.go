package channels

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestResolveChatBehavior_InheritsGlobalAndChannelOverride(t *testing.T) {
	fixedMode := QuickAckModeFixedTemplate
	global := &config.ChatBehaviorConfig{
		Enabled: new(true),
		QuickAck: &config.QuickAckConfig{
			Enabled:    new(true),
			Mode:       &fixedMode,
			MinDelayMs: new(750),
			Templates:  []string{"On it."},
		},
		FinalSplit: &config.FinalSplitConfig{
			Enabled:     new(true),
			MinChars:    new(1200),
			MaxMessages: new(3),
			DelayMs:     new(400),
		},
	}
	override := &config.ChatBehaviorConfig{
		QuickAck: &config.QuickAckConfig{Enabled: new(false)},
		FinalSplit: &config.FinalSplitConfig{
			MaxMessages: new(2),
		},
	}

	got := ResolveChatBehavior(global, override)

	if !got.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if got.QuickAck.Enabled {
		t.Fatal("QuickAck.Enabled = true, want channel override false")
	}
	if got.QuickAck.MinDelayMs != 750 {
		t.Fatalf("QuickAck.MinDelayMs = %d, want 750", got.QuickAck.MinDelayMs)
	}
	if got.FinalSplit.MaxMessages != 2 {
		t.Fatalf("FinalSplit.MaxMessages = %d, want override 2", got.FinalSplit.MaxMessages)
	}
	if got.FinalSplit.MinChars != 1200 || got.FinalSplit.DelayMs != 400 {
		t.Fatalf("FinalSplit inherited fields = %+v, want min=1200 delay=400", got.FinalSplit)
	}
}

func TestResolveChatBehavior_DefaultQuickAckModeIsLLMGenerated(t *testing.T) {
	global := &config.ChatBehaviorConfig{
		Enabled:  new(true),
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Templates: []string{"Fallback."}},
	}

	got := ResolveChatBehavior(global, nil)

	if got.QuickAck.Mode != QuickAckModeLLMGenerated {
		t.Fatalf("QuickAck.Mode = %q, want %q", got.QuickAck.Mode, QuickAckModeLLMGenerated)
	}
	if ShouldDeliverGeneratedProgress(got, false) {
		t.Fatal("generated progress coupled to quick ack; want independent intermediate_replies gate")
	}
	if !ShouldSendQuickAck(got, false) {
		t.Fatal("fallback quick ack disabled for non-streaming llm_generated mode")
	}
	if got.QuickAck.Templates[0] != "Fallback." {
		t.Fatalf("fallback template = %q, want configured fallback", got.QuickAck.Templates[0])
	}
}

func TestResolveChatBehavior_IntermediateRepliesIndependentFromQuickAck(t *testing.T) {
	mode := IntermediateModeSidecar
	global := &config.ChatBehaviorConfig{
		Enabled: new(true),
		IntermediateReplies: &config.IntermediateRepliesConfig{
			Enabled: new(true),
			Mode:    &mode,
		},
		QuickAck: &config.QuickAckConfig{Enabled: new(false), Templates: []string{"Fallback."}},
	}

	got := ResolveChatBehavior(global, nil)

	if !ShouldDeliverGeneratedProgress(got, false) {
		t.Fatal("intermediate replies disabled when quick ack is off")
	}
	if ShouldSendQuickAck(got, false) {
		t.Fatal("quick ack enabled despite explicit false")
	}
}

func TestResolveChatBehaviorWithAgent_ChannelBeatsAgentBeatsWorkspace(t *testing.T) {
	global := &config.ChatBehaviorConfig{
		Enabled: new(true),
		IntermediateReplies: &config.IntermediateRepliesConfig{
			Enabled:  new(false),
			Provider: "workspace-provider",
		},
		QuickAck: &config.QuickAckConfig{Enabled: new(false), Provider: "workspace-provider"},
	}
	agentOverride := &config.ChatBehaviorConfig{
		IntermediateReplies: &config.IntermediateRepliesConfig{
			Enabled:  new(true),
			Provider: "agent-provider",
		},
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Provider: "agent-provider"},
	}
	channelOverride := &config.ChatBehaviorConfig{
		QuickAck: &config.QuickAckConfig{Enabled: new(false), Provider: "channel-provider"},
	}

	got := ResolveChatBehaviorWithAgent(global, agentOverride, channelOverride)

	if !got.IntermediateReplies.Enabled || got.IntermediateReplies.Provider != "agent-provider" {
		t.Fatalf("intermediate = %+v, want agent override", got.IntermediateReplies)
	}
	if got.QuickAck.Enabled || got.QuickAck.Provider != "channel-provider" {
		t.Fatalf("quick ack = %+v, want channel override", got.QuickAck)
	}
}

func TestResolveChatBehaviorWithAgent_FieldLevelPrecedence(t *testing.T) {
	quickMode := QuickAckModeSidecar
	fixedMode := QuickAckModeFixedTemplate
	intermediateMode := IntermediateModeSidecar
	offMode := IntermediateModeOff
	global := &config.ChatBehaviorConfig{
		Enabled: new(true),
		IntermediateReplies: &config.IntermediateRepliesConfig{
			Enabled:   new(false),
			Mode:      &offMode,
			Provider:  "global-intermediate-provider",
			Model:     "global-intermediate-model",
			TimeoutMs: new(1000),
			MaxTokens: new(10),
			MaxChars:  new(100),
		},
		QuickAck: &config.QuickAckConfig{
			Enabled:    new(false),
			Mode:       &fixedMode,
			MinDelayMs: new(100),
			Provider:   "global-ack-provider",
			Model:      "global-ack-model",
			TimeoutMs:  new(1100),
			MaxTokens:  new(11),
			MaxChars:   new(111),
			Templates:  []string{"global template"},
		},
	}
	agentOverride := &config.ChatBehaviorConfig{
		IntermediateReplies: &config.IntermediateRepliesConfig{
			Enabled:   new(true),
			Mode:      &intermediateMode,
			Provider:  "agent-intermediate-provider",
			Model:     "agent-intermediate-model",
			TimeoutMs: new(2000),
			MaxTokens: new(20),
			MaxChars:  new(200),
		},
		QuickAck: &config.QuickAckConfig{
			Enabled:    new(true),
			Mode:       &quickMode,
			MinDelayMs: new(200),
			Provider:   "agent-ack-provider",
			Model:      "agent-ack-model",
			TimeoutMs:  new(2200),
			MaxTokens:  new(22),
			MaxChars:   new(222),
			Templates:  []string{"agent template"},
		},
	}
	channelOverride := &config.ChatBehaviorConfig{
		IntermediateReplies: &config.IntermediateRepliesConfig{
			Provider:  "channel-intermediate-provider",
			TimeoutMs: new(3000),
		},
		QuickAck: &config.QuickAckConfig{
			Enabled:    new(false),
			Model:      "channel-ack-model",
			MaxTokens:  new(33),
			MinDelayMs: new(300),
		},
	}

	got := ResolveChatBehaviorWithAgent(global, agentOverride, channelOverride)

	if !got.Enabled {
		t.Fatal("Enabled = false, want global enabled")
	}
	if !got.IntermediateReplies.Enabled {
		t.Fatal("Intermediate enabled = false, want agent override true")
	}
	if got.IntermediateReplies.Mode != IntermediateModeSidecar {
		t.Fatalf("Intermediate mode = %q, want agent sidecar", got.IntermediateReplies.Mode)
	}
	if got.IntermediateReplies.Provider != "channel-intermediate-provider" || got.IntermediateReplies.Model != "agent-intermediate-model" {
		t.Fatalf("Intermediate provider/model = %q/%q, want channel provider + agent model", got.IntermediateReplies.Provider, got.IntermediateReplies.Model)
	}
	if got.IntermediateReplies.Timeout != 3*time.Second || got.IntermediateReplies.MaxTokens != 20 || got.IntermediateReplies.MaxChars != 200 {
		t.Fatalf("Intermediate limits = %+v, want channel timeout + agent token/char limits", got.IntermediateReplies)
	}
	if got.QuickAck.Enabled {
		t.Fatal("QuickAck enabled = true, want channel override false")
	}
	if got.QuickAck.Mode != QuickAckModeSidecar {
		t.Fatalf("QuickAck mode = %q, want agent sidecar", got.QuickAck.Mode)
	}
	if got.QuickAck.Provider != "agent-ack-provider" || got.QuickAck.Model != "channel-ack-model" {
		t.Fatalf("QuickAck provider/model = %q/%q, want agent provider + channel model", got.QuickAck.Provider, got.QuickAck.Model)
	}
	if got.QuickAck.MinDelayMs != 300 || got.QuickAck.Timeout != 2200*time.Millisecond || got.QuickAck.MaxTokens != 33 || got.QuickAck.MaxChars != 222 {
		t.Fatalf("QuickAck limits = %+v, want merged channel/agent limits", got.QuickAck)
	}
	if !reflect.DeepEqual(got.QuickAck.Templates, []string{"agent template"}) {
		t.Fatalf("QuickAck templates = %#v, want agent template", got.QuickAck.Templates)
	}
}

func TestChatBehaviorConfigWithIntermediateDefault_UsesLegacyBlockReplyOnlyWhenUnset(t *testing.T) {
	legacyEnabled := true
	explicitDisabled := false
	base := &config.ChatBehaviorConfig{
		IntermediateReplies: &config.IntermediateRepliesConfig{Enabled: &explicitDisabled},
	}

	got := ChatBehaviorConfigWithIntermediateDefault(base, &legacyEnabled)
	if got.IntermediateReplies == nil || got.IntermediateReplies.Enabled == nil || *got.IntermediateReplies.Enabled {
		t.Fatalf("intermediate enabled = %#v, want explicit false to win", got.IntermediateReplies)
	}
	if base.IntermediateReplies.Enabled == nil || *base.IntermediateReplies.Enabled {
		t.Fatalf("mutated source config = %#v", base.IntermediateReplies)
	}

	got = ChatBehaviorConfigWithIntermediateDefault(nil, &legacyEnabled)
	if got == nil || got.IntermediateReplies == nil || got.IntermediateReplies.Enabled == nil || !*got.IntermediateReplies.Enabled {
		t.Fatalf("legacy default not applied: %#v", got)
	}
	if got.Enabled == nil || !*got.Enabled {
		t.Fatalf("legacy block_reply=true did not enable chat behavior: %#v", got)
	}
}

func TestResolveChatBehaviorWithAgent_ChannelBlockReplySeedsIntermediateDefault(t *testing.T) {
	global := &config.ChatBehaviorConfig{Enabled: new(true)}
	globalBlockReply := true
	channelBlockReply := false
	mgr := NewManager(bus.New())
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test", blockReply: &channelBlockReply})

	got := mgr.ResolveChatBehaviorWithAgent("test", ChatBehaviorConfigWithIntermediateDefault(global, &globalBlockReply), nil)

	if got.IntermediateReplies.Enabled {
		t.Fatalf("intermediate enabled = true, want legacy channel block_reply=false to win")
	}
}

func TestParseAgentDeliveryBehaviorConfig(t *testing.T) {
	raw := []byte(`{"unrelated":true,"delivery_behavior":{"enabled":true,"quick_ack":{"enabled":true,"provider":"groq"},"intermediate_replies":{"enabled":false}}}`)

	got := ParseAgentDeliveryBehaviorConfig(raw)

	if got == nil || got.QuickAck == nil || got.QuickAck.Provider != "groq" {
		t.Fatalf("agent delivery behavior = %#v, want quick ack provider", got)
	}
	if got.IntermediateReplies == nil || got.IntermediateReplies.Enabled == nil || *got.IntermediateReplies.Enabled {
		t.Fatalf("intermediate override = %#v, want enabled=false", got.IntermediateReplies)
	}
}

func TestResolveChatBehavior_QuickAckModeOffKeepsFallbackDisabled(t *testing.T) {
	mode := QuickAckModeOff
	global := &config.ChatBehaviorConfig{
		Enabled:  new(true),
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Mode: &mode, Templates: []string{"Fallback."}},
	}

	got := ResolveChatBehavior(global, nil)

	if got.QuickAck.Mode != QuickAckModeOff {
		t.Fatalf("QuickAck.Mode = %q, want off", got.QuickAck.Mode)
	}
	if ShouldDeliverGeneratedProgress(got, false) {
		t.Fatal("generated progress enabled in off mode")
	}
	if ShouldSendQuickAck(got, false) {
		t.Fatal("fallback quick ack enabled in off mode")
	}
}

func TestResolveChatBehavior_ExplicitFixedTemplateMode(t *testing.T) {
	mode := QuickAckModeFixedTemplate
	global := &config.ChatBehaviorConfig{
		Enabled:  new(true),
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Mode: &mode, Templates: []string{"Working."}},
	}

	got := ResolveChatBehavior(global, nil)

	if got.QuickAck.Mode != QuickAckModeFixedTemplate {
		t.Fatalf("QuickAck.Mode = %q, want fixed_template", got.QuickAck.Mode)
	}
	if ShouldDeliverGeneratedProgress(got, false) {
		t.Fatal("generated progress enabled in fixed_template mode")
	}
	if !ShouldSendQuickAck(got, false) {
		t.Fatal("fixed template quick ack disabled")
	}
}

func TestSplitFinalMessages_ConservativeParagraphSplit(t *testing.T) {
	cfg := ResolvedFinalSplitConfig{Enabled: true, MinChars: 20, MaxMessages: 3}
	text := "First part is useful.\n\nSecond part is also useful.\n\nThird part closes it."

	got := SplitFinalMessages(text, cfg)
	want := []string{"First part is useful.", "Second part is also useful.", "Third part closes it."}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitFinalMessages() = %#v, want %#v", got, want)
	}
}

func TestSplitFinalMessages_DoesNotSplitUnsafeMarkdown(t *testing.T) {
	cfg := ResolvedFinalSplitConfig{Enabled: true, MinChars: 10, MaxMessages: 3}
	cases := map[string]string{
		"fenced code":   "Intro.\n\n```go\nfmt.Println(\"hi\")\n```\n\nDone.",
		"table":         "A | B\n--- | ---\n1 | 2\n\nDone.",
		"list":          "Intro.\n\n- one\n- two\n\nDone.",
		"quote":         "Intro.\n\n> quoted\n> text\n\nDone.",
		"json":          "Intro.\n\n{\"ok\": true}\n\nDone.",
		"url paragraph": "Intro.\n\nhttps://example.com/a/b?c=d\n\nDone.",
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			got := SplitFinalMessages(text, cfg)
			if len(got) != 1 || got[0] != text {
				t.Fatalf("SplitFinalMessages() = %#v, want original single message", got)
			}
		})
	}
}

func TestPreviewChatBehavior_NoSideEffects(t *testing.T) {
	fixedMode := QuickAckModeFixedTemplate
	global := &config.ChatBehaviorConfig{
		Enabled:    new(true),
		QuickAck:   &config.QuickAckConfig{Enabled: new(true), Mode: &fixedMode, Templates: []string{"Working."}},
		FinalSplit: &config.FinalSplitConfig{Enabled: new(true), MinChars: new(10), MaxMessages: new(2)},
	}

	got := PreviewChatBehavior(global, nil, ChatBehaviorPreviewOptions{
		Content:      "Part one is long.\n\nPart two is long.",
		IsStreaming:  false,
		HasToolCalls: true,
	})

	if !got.Ack.ShouldSend || got.Ack.Content != "Working." {
		t.Fatalf("Ack preview = %+v, want send Working.", got.Ack)
	}
	if got.Ack.Mode != QuickAckModeFixedTemplate || got.Ack.Source != QuickAckSourceTemplate {
		t.Fatalf("Ack preview mode/source = %q/%q, want fixed_template/template", got.Ack.Mode, got.Ack.Source)
	}
	if len(got.Split.Parts) != 2 {
		t.Fatalf("Split parts = %#v, want two parts", got.Split.Parts)
	}
}

func TestPreviewChatBehavior_GeneratedModeReportsLLMOnly(t *testing.T) {
	global := &config.ChatBehaviorConfig{
		Enabled:  new(true),
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Templates: []string{"Fallback."}},
	}

	got := PreviewChatBehavior(global, nil, ChatBehaviorPreviewOptions{
		Content:      "Part one.\n\nPart two.",
		IsStreaming:  false,
		HasToolCalls: true,
	})

	if !got.Ack.ShouldSend || got.Ack.Mode != QuickAckModeLLMGenerated || got.Ack.Source != QuickAckSourceGenerated {
		t.Fatalf("Ack preview = %+v, want generated-first send decision", got.Ack)
	}
	if got.Ack.Content != "" {
		t.Fatalf("Ack preview content = %q, want empty value for generated mode", got.Ack.Content)
	}
}

func TestPreviewChatBehavior_GeneratedModeWithoutToolCallsStillReportsLLMOnly(t *testing.T) {
	global := &config.ChatBehaviorConfig{
		Enabled:  new(true),
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Templates: []string{"Fallback."}},
	}

	got := PreviewChatBehavior(global, nil, ChatBehaviorPreviewOptions{
		Content:      "Short final answer.",
		IsStreaming:  false,
		HasToolCalls: false,
	})

	if !got.Ack.ShouldSend || got.Ack.Mode != QuickAckModeLLMGenerated || got.Ack.Source != QuickAckSourceGenerated {
		t.Fatalf("Ack preview = %+v, want generated send decision", got.Ack)
	}
	if got.Ack.Content != "" {
		t.Fatalf("Ack preview content = %q, want empty value for generated mode", got.Ack.Content)
	}
}

func TestManagerResolveChatBehavior_UsesChannelOverride(t *testing.T) {
	fixedMode := QuickAckModeFixedTemplate
	global := &config.ChatBehaviorConfig{
		Enabled:  new(true),
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Mode: &fixedMode, Templates: []string{"global"}},
	}
	override := &config.ChatBehaviorConfig{
		QuickAck: &config.QuickAckConfig{Enabled: new(true), Mode: &fixedMode, Templates: []string{"channel"}},
	}
	mgr := NewManager(bus.New())
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test", behavior: override})

	got := mgr.ResolveChatBehavior("test", global)

	if got.QuickAck.Templates[0] != "channel" {
		t.Fatalf("QuickAck template = %q, want channel override", got.QuickAck.Templates[0])
	}
}

type chatBehaviorTestChannel struct {
	name       string
	behavior   *config.ChatBehaviorConfig
	blockReply *bool
}

func (c *chatBehaviorTestChannel) Name() string                                    { return c.name }
func (c *chatBehaviorTestChannel) Type() string                                    { return c.name }
func (c *chatBehaviorTestChannel) Start(context.Context) error                     { return nil }
func (c *chatBehaviorTestChannel) Stop(context.Context) error                      { return nil }
func (c *chatBehaviorTestChannel) Send(context.Context, bus.OutboundMessage) error { return nil }
func (c *chatBehaviorTestChannel) IsRunning() bool                                 { return true }
func (c *chatBehaviorTestChannel) IsAllowed(string) bool                           { return true }
func (c *chatBehaviorTestChannel) ChatBehaviorConfig() *config.ChatBehaviorConfig  { return c.behavior }
func (c *chatBehaviorTestChannel) BlockReplyEnabled() *bool                        { return c.blockReply }
