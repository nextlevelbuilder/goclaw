// Package line's hooks.go defines the MessageHook plugin interface that lets
// goclaw deployments subscribe to LINE webhook events without modifying the
// channel adapter itself.
//
// Design intent:
//
//   - Hooks receive flat, typed event structs (AudioEvent, TextEvent,
//     PostbackEvent). They must NOT import linebot SDK types.
//   - Hooks use a LineSender interface (separate from MessageHook) to push
//     replies back to LINE. Channel implements LineSender.
//   - Fan-out semantics: every registered hook receives every event. Hooks
//     that don't care about an event return nil immediately. Hooks MUST be
//     independent — one hook's error must not block other hooks.
//   - Optional Lifecycle interface for hooks that need Start/Stop hooks
//     (e.g. background goroutines). Channel.Start calls Start after its own
//     init; Channel.Stop calls Stop in reverse registration order.
package line

import "context"

// AudioEvent is delivered when a LINE AudioMessage arrives. The channel has
// already downloaded the content to TempPath; the hook is responsible for
// moving/processing it.
type AudioEvent struct {
	UserID      string
	ChatID      string
	MessageID   string
	ContentType string
	TempPath    string
}

// TextEvent is delivered when a LINE TextMessage arrives.
type TextEvent struct {
	UserID     string
	ChatID     string
	Text       string
	ReplyToken string
}

// PostbackEvent is delivered when a LINE Postback event arrives (Flex button
// press, etc.).
type PostbackEvent struct {
	UserID     string
	ChatID     string
	Data       string
	ReplyToken string
}

// MessageHook is implemented by plugins that want to observe LINE events.
//
// Hooks MUST be safe to call concurrently with other hook invocations. Hooks
// MUST NOT depend on side effects from other hooks within the same event
// delivery — the channel calls hooks in registration order, but that is an
// implementation detail, not a contract.
//
// Returning an error from a hook method is logged by the channel but does not
// block other hooks or retry the event.
type MessageHook interface {
	OnAudio(ctx context.Context, ev AudioEvent) error
	OnText(ctx context.Context, ev TextEvent) error
	OnPostback(ctx context.Context, ev PostbackEvent) error
}

// LineSender is the reply-capable interface that hooks use to push messages
// back to LINE. Channel implements this — hooks receive it via their Config
// at construction time, NOT via the MessageHook method signatures, so the
// interface stays focused on event delivery.
type LineSender interface {
	// SendChunks sends one or more plain-text messages to a chat, splitting
	// as needed to stay within LINE's length limits.
	SendChunks(chatID string, chunks []string) error
	// PushFlex pushes a Flex bubble to a chat (no reply token needed).
	PushFlex(chatID, altText string, flexJSON []byte) error
	// ReplyFlex replies to a known reply token with a Flex bubble.
	ReplyFlex(chatID, altText string, flexJSON []byte) error
}

// Lifecycle is optionally implemented by hooks that need Start/Stop hooks
// around the channel's own lifecycle. Channel.Start calls Start(ctx) on
// every registered hook that implements Lifecycle, after its own init is
// done. Channel.Stop calls Stop() in reverse registration order.
//
// Hooks that don't need background work can skip this interface entirely.
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop() error
}
