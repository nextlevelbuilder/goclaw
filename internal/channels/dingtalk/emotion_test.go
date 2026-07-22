package dingtalk

import (
	"context"
	"net/http"
	"testing"
)

func reactChannel(t *testing.T, cfg Config, api *stubCardAPI) *Channel {
	t.Helper()
	ch := cardChannel(t, cfg, api)
	ch.rememberChat("staff-1", chatMeta{UserID: "staff-1", ConversationID: "cid-dm"})
	return ch
}

// A run reports "thinking" and then one status per tool call. The reaction is
// posted once, not once per status.
func TestEmotion_PostedOnceThenRecalled(t *testing.T) {
	api := newStubCardAPI(t)
	ch := reactChannel(t, baseCfg(), api)
	ctx := context.Background()

	for _, status := range []string{"thinking", "web", "coding"} {
		if err := ch.OnReactionEvent(ctx, "staff-1", "msg-1", status); err != nil {
			t.Fatalf("status %q: %v", status, err)
		}
	}
	if got := len(api.pathsOf(pathEmotionReply)); got != 1 {
		t.Fatalf("posted %d reactions across 3 statuses, want 1", got)
	}

	body := api.pathsOf(pathEmotionReply)[0].Body
	if body["openMsgId"] != "msg-1" {
		t.Errorf("openMsgId = %v", body["openMsgId"])
	}
	// The conversation id cannot come from chatID: a DM's chatID is the sender's
	// staff id.
	if body["openConversationId"] != "cid-dm" {
		t.Errorf("openConversationId = %v, want cid-dm", body["openConversationId"])
	}
	if body["robotCode"] != "robot-1" {
		t.Errorf("robotCode = %v", body["robotCode"])
	}
	te, _ := body["textEmotion"].(map[string]any)
	if te["emotionId"] != emotionID {
		t.Errorf("emotionId = %v", te["emotionId"])
	}

	if err := ch.OnReactionEvent(ctx, "staff-1", "msg-1", "done"); err != nil {
		t.Fatal(err)
	}
	if got := len(api.pathsOf(pathEmotionRecall)); got != 1 {
		t.Fatalf("recalled %d times, want 1", got)
	}
}

func TestEmotion_ErrorStatusAlsoRecalls(t *testing.T) {
	api := newStubCardAPI(t)
	ch := reactChannel(t, baseCfg(), api)
	ctx := context.Background()

	_ = ch.OnReactionEvent(ctx, "staff-1", "msg-1", "thinking")
	_ = ch.OnReactionEvent(ctx, "staff-1", "msg-1", "error")
	if got := len(api.pathsOf(pathEmotionRecall)); got != 1 {
		t.Errorf("a failed run left the reaction stuck on the message")
	}
}

// Recalling a reaction that was never posted must not call the API — DingTalk
// would reject it, and the run would log a spurious failure.
func TestEmotion_RecallWithoutPostIsNoop(t *testing.T) {
	api := newStubCardAPI(t)
	ch := reactChannel(t, baseCfg(), api)

	if err := ch.ClearReaction(context.Background(), "staff-1", "msg-unknown"); err != nil {
		t.Fatal(err)
	}
	if got := len(api.pathsOf(pathEmotionRecall)); got != 0 {
		t.Errorf("recalled %d times for a message that never had a reaction", got)
	}
}

func TestEmotion_DisabledDoesNothing(t *testing.T) {
	api := newStubCardAPI(t)
	cfg := baseCfg()
	cfg.ReactionLevel = ReactionLevelOff
	ch := reactChannel(t, cfg, api)
	ctx := context.Background()

	_ = ch.OnReactionEvent(ctx, "staff-1", "msg-1", "thinking")
	_ = ch.OnReactionEvent(ctx, "staff-1", "msg-1", "done")
	if got := len(api.snapshot()); got != 0 {
		t.Errorf("reaction_level=off issued %d requests: %+v", got, api.snapshot())
	}
}

// A cron or delegate run has no inbound message, so nothing to react to.
func TestEmotion_UnknownChatIsNoop(t *testing.T) {
	api := newStubCardAPI(t)
	ch := cardChannel(t, baseCfg(), api) // no rememberChat

	if err := ch.OnReactionEvent(context.Background(), "staff-9", "msg-1", "thinking"); err != nil {
		t.Fatal(err)
	}
	if got := len(api.pathsOf(pathEmotionReply)); got != 0 {
		t.Errorf("posted a reaction with no conversation id")
	}
}

// Reactions are cosmetic. A failure must not fail the run, and must not leave
// the message marked as reacted — a later status should be able to retry.
func TestEmotion_FailureIsSwallowedAndRetryable(t *testing.T) {
	api := newStubCardAPI(t)
	api.failPath = pathEmotionReply
	ch := reactChannel(t, baseCfg(), api)
	ctx := context.Background()

	if err := ch.OnReactionEvent(ctx, "staff-1", "msg-1", "thinking"); err != nil {
		t.Fatalf("a failed reaction must not fail the run: %v", err)
	}

	api.failPath = ""
	if err := ch.OnReactionEvent(ctx, "staff-1", "msg-1", "web"); err != nil {
		t.Fatal(err)
	}
	if got := len(api.pathsOf(pathEmotionReply)); got != 2 {
		t.Errorf("attempts = %d, want 2 (the first failed and was retried)", got)
	}
}

// The reaction only makes sense while a run is in flight; a terminal status must
// never post one.
func TestEmotion_TerminalStatusNeverPosts(t *testing.T) {
	api := newStubCardAPI(t)
	ch := reactChannel(t, baseCfg(), api)

	_ = ch.OnReactionEvent(context.Background(), "staff-1", "msg-1", "done")
	if got := len(api.pathsOf(pathEmotionReply)); got != 0 {
		t.Errorf("a terminal status posted a reaction")
	}
}

func TestEmotion_UsesPOST(t *testing.T) {
	api := newStubCardAPI(t)
	ch := reactChannel(t, baseCfg(), api)
	_ = ch.OnReactionEvent(context.Background(), "staff-1", "msg-1", "thinking")
	if got := api.methodOf(pathEmotionReply); got != http.MethodPost {
		t.Errorf("method = %q, want POST", got)
	}
}
