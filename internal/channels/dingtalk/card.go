package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AI Card endpoints, all on api.dingtalk.com.
const (
	pathCardInstances = "/v1.0/card/instances"
	pathCardDeliver   = "/v1.0/card/instances/deliver"
	pathCardStreaming = "/v1.0/card/streaming"
)

// cardTemplateID is DingTalk's stock streaming-card template. It is a published
// constant, not per-app.
const cardTemplateID = "02fcf2f4-5e02-4a85-b672-46d1f715543e.schema"

// flowStatus values the card template understands. DingTalk also defines "1"
// (processing) and "4" (executing); this channel has no state that maps to them.
const (
	flowStatusInputing = "2"
	flowStatusFinished = "3"
	flowStatusFailed   = "5"
)

// cardContentKey is the template field the streamed markdown lands in.
const cardContentKey = "msgContent"

// cardUpdateThrottle bounds how often one card is repainted. Every LLM chunk
// would otherwise become an HTTP request.
const cardUpdateThrottle = 800 * time.Millisecond

// DingTalk's card API is rate limited per app. The documented ceiling is around
// 40 QPS; the upstream connector halves it, and so do we — a repaint dropped to
// a rate limit is a visible stutter.
const (
	cardMaxQPS       = 20
	cardMinInterval  = time.Second / cardMaxQPS
	cardQPSBackoff   = 2 * time.Second
	qpsLimitCodeFrag = "QpsLimit"
)

// cardRateLimiter paces card writes for one app.
//
// The upstream connector keeps one bucket for the whole process. Per-channel is
// both simpler and more correct: DingTalk meters by app, and a GoClaw channel
// instance is exactly one app.
type cardRateLimiter struct {
	mu          sync.Mutex
	nextAllowed time.Time
	interval    time.Duration
	backoffFor  time.Duration
}

func newCardRateLimiter() *cardRateLimiter {
	return &cardRateLimiter{interval: cardMinInterval, backoffFor: cardQPSBackoff}
}

// wait blocks until the next write is permitted.
func (l *cardRateLimiter) wait(ctx context.Context, now func() time.Time) error {
	l.mu.Lock()
	t := now()
	if l.nextAllowed.After(t) {
		delay := l.nextAllowed.Sub(t)
		l.nextAllowed = l.nextAllowed.Add(l.interval)
		l.mu.Unlock()

		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.nextAllowed = t.Add(l.interval)
	l.mu.Unlock()
	return nil
}

// backoff pushes the next permitted write out after a rate-limit rejection.
func (l *cardRateLimiter) backoff(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := now().Add(l.backoffFor); until.After(l.nextAllowed) {
		l.nextAllowed = until
	}
}

// isQPSLimit reports whether DingTalk rejected a write for rate limiting.
func isQPSLimit(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusForbidden && strings.Contains(apiErr.Code, qpsLimitCodeFrag)
}

// newOutTrackID mints the client-generated card identity. It doubles as the
// cardInstanceId in every later call.
func newOutTrackID() string {
	return fmt.Sprintf("card_%d_%s", time.Now().UnixMilli(), randomBase36(8))
}

func randomBase36(n int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(b)
}

// cardStream is the per-run AI Card handle returned by CreateStream.
//
// One card is one run. The framework stores this on RunContext, so concurrent
// runs in the same conversation never share a card.
type cardStream struct {
	ch         *Channel
	outTrackID string

	mu       sync.Mutex
	lastText string
	lastPush time.Time
	done     bool
}

var _ interface {
	Update(ctx context.Context, text string)
	Stop(ctx context.Context) error
	MessageID() int
} = (*cardStream)(nil)

// Update repaints the card, at most once per cardUpdateThrottle.
//
// A skipped repaint is not lost: the text is retained and Stop flushes it. The
// clock is advanced before the network call, not after, so a slow request does
// not let a concurrent Update slip through the throttle behind it.
func (s *cardStream) Update(ctx context.Context, text string) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.lastText = text

	now := s.ch.now()
	if !s.lastPush.IsZero() && now.Sub(s.lastPush) < cardUpdateThrottle {
		s.mu.Unlock()
		return
	}
	s.lastPush = now
	s.mu.Unlock()

	if err := s.push(ctx, text, false); err != nil {
		slog.Debug("dingtalk card update failed",
			"channel", s.ch.Name(), "card", s.outTrackID, "error", err)
	}
}

// Stop drives the card to a terminal state.
//
// This is called by the framework on run.completed, run.failed AND
// run.cancelled, which is what makes it the right place to guarantee the card
// never freezes: a card left at INPUTING spins forever in the DingTalk UI, and
// the upstream connector needed a dedicated repair RPC (fixStuckCards) because
// of exactly that.
//
// Stop carries no error, so a failed run cannot be distinguished from a
// completed one here and both land on FINISHED. A terminal card with a slightly
// generous status beats a card frozen mid-thought; the framework separately
// publishes a human-readable error message for failed runs.
func (s *cardStream) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return nil
	}
	s.done = true
	text := s.lastText
	s.mu.Unlock()

	return s.finish(ctx, text, flowStatusFinished)
}

// abort drives an unfinished card to FAILED. Used when the gateway shuts down
// mid-run: the answer never arrived, and claiming FINISHED would lie about it.
func (s *cardStream) abort(ctx context.Context) error {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return nil
	}
	s.done = true
	text := s.lastText
	s.mu.Unlock()

	return s.finish(ctx, text, flowStatusFailed)
}

// repaint rewrites a finished card with the final, formatted answer.
//
// The streamed text is raw model output; Send() receives the same answer after
// the pipeline has formatted it. Rewriting in place is what keeps the card from
// being followed by a duplicate message.
func (s *cardStream) repaint(ctx context.Context, text string) error {
	body := map[string]any{
		"outTrackId": s.outTrackID,
		"cardData": map[string]any{
			"cardParamMap": map[string]string{
				cardContentKey: text,
				"flowStatus":   flowStatusFinished,
			},
		},
		"cardUpdateOptions": map[string]any{
			"updateCardDataByKey": true,
		},
	}
	return s.ch.cardWrite(ctx, http.MethodPut, pathCardInstances, body, nil, nil)
}

// MessageID has no meaning for cards: DingTalk identifies them by outTrackId, a
// string. Send() finds the card through the channel's card map instead.
func (s *cardStream) MessageID() int { return 0 }

// push writes one streaming frame.
//
// isFull is always true: DingTalk expects the whole text each time, not a delta.
// Trailing newlines are stripped on non-final frames or the card flickers.
func (s *cardStream) push(ctx context.Context, text string, finalize bool) error {
	if !finalize {
		text = strings.TrimRight(text, "\n")
	}

	body := map[string]any{
		"outTrackId": s.outTrackID,
		"guid":       fmt.Sprintf("%d_%s", time.Now().UnixMilli(), randomBase36(6)),
		"key":        cardContentKey,
		"content":    text,
		"isFull":     true,
		"isFinalize": finalize,
		"isError":    false,
	}
	return s.ch.cardWrite(ctx, http.MethodPut, pathCardStreaming, body, nil, func(b map[string]any) {
		// A retry must carry a fresh guid; reusing it makes DingTalk treat the
		// frame as a duplicate and drop it.
		b["guid"] = fmt.Sprintf("%d_%s", time.Now().UnixMilli(), randomBase36(6))
	})
}

// finish flushes the last text and stamps a terminal flowStatus.
func (s *cardStream) finish(ctx context.Context, text, status string) error {
	if err := s.push(ctx, text, true); err != nil {
		// Even if the final content frame fails, the status must be stamped, or
		// the card is stuck.
		slog.Warn("dingtalk final card frame failed; still stamping terminal status",
			"channel", s.ch.Name(), "card", s.outTrackID, "error", err)
	}

	body := map[string]any{
		"outTrackId": s.outTrackID,
		"cardData": map[string]any{
			"cardParamMap": map[string]string{
				"flowStatus": status,
			},
		},
		"cardUpdateOptions": map[string]any{
			"updateCardDataByKey": true,
		},
	}
	err := s.ch.cardWrite(ctx, http.MethodPut, pathCardInstances, body, nil, nil)
	s.ch.liveCards.Delete(s.outTrackID)
	return err
}

// setInputing marks the card as typing. Sent with the first frame.
func (s *cardStream) setInputing(ctx context.Context, text string) error {
	body := map[string]any{
		"outTrackId": s.outTrackID,
		"cardData": map[string]any{
			"cardParamMap": map[string]string{
				"flowStatus":        flowStatusInputing,
				cardContentKey:      text,
				"staticMsgContent":  "",
				"sys_full_json_obj": `{"order":["` + cardContentKey + `"]}`,
				"config":            `{"autoLayout":true}`,
			},
		},
	}
	return s.ch.cardWrite(ctx, http.MethodPut, pathCardInstances, body, nil, nil)
}

// cardWrite performs a rate-limited card API call, retrying once on a QPS
// rejection. mutate, when set, refreshes any per-attempt fields before a retry.
func (c *Channel) cardWrite(ctx context.Context, method, path string, body map[string]any, out any,
	mutate func(map[string]any)) error {

	if err := c.cardLimiter.wait(ctx, c.now); err != nil {
		return err
	}

	err := c.client.doAPI(ctx, method, path, body, out)
	if err == nil || !isQPSLimit(err) {
		return err
	}

	c.cardLimiter.backoff(c.now)
	if mutate != nil {
		mutate(body)
	}
	if err := c.cardLimiter.wait(ctx, c.now); err != nil {
		return err
	}
	return c.client.doAPI(ctx, method, path, body, out)
}

// createCard creates a card instance and delivers it into the conversation.
func (c *Channel) createCard(ctx context.Context, meta chatMeta) (*cardStream, error) {
	s := &cardStream{ch: c, outTrackID: newOutTrackID()}

	create := map[string]any{
		"cardTemplateId": cardTemplateID,
		"outTrackId":     s.outTrackID,
		"cardData": map[string]any{
			"cardParamMap": map[string]string{"config": `{"autoLayout":true}`},
		},
		"callbackType":          "STREAM",
		"imGroupOpenSpaceModel": map[string]any{"supportForward": true},
		"imRobotOpenSpaceModel": map[string]any{"supportForward": true},
	}
	if err := c.cardWrite(ctx, http.MethodPost, pathCardInstances, create, nil, nil); err != nil {
		return nil, fmt.Errorf("dingtalk: create card: %w", err)
	}

	deliver := map[string]any{
		"outTrackId": s.outTrackID,
		"userIdType": 1,
	}
	if meta.IsGroup {
		// The double slash in openSpaceId is literal.
		deliver["openSpaceId"] = "dtv1.card//IM_GROUP." + meta.ConversationID
		deliver["imGroupOpenDeliverModel"] = map[string]any{"robotCode": c.cfg.ClientID}
	} else {
		deliver["openSpaceId"] = "dtv1.card//IM_ROBOT." + meta.UserID
		deliver["imRobotOpenDeliverModel"] = map[string]any{
			"spaceType": "IM_ROBOT",
			"robotCode": c.cfg.ClientID,
			"extension": map[string]string{"dynamicSummary": "true"},
		}
	}
	if err := c.cardWrite(ctx, http.MethodPost, pathCardDeliver, deliver, nil, nil); err != nil {
		return nil, fmt.Errorf("dingtalk: deliver card: %w", err)
	}

	if err := s.setInputing(ctx, ""); err != nil {
		slog.Debug("dingtalk card inputing status failed",
			"channel", c.Name(), "card", s.outTrackID, "error", err)
	}

	c.liveCards.Store(s.outTrackID, s)
	return s, nil
}
