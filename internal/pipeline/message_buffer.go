package pipeline

import "github.com/nextlevelbuilder/goclaw/internal/providers"

// MessageBuffer wraps the message list with append/replace semantics.
// Sequential pipeline guarantees only one stage writes at a time — no mutex needed.
type MessageBuffer struct {
	system  providers.Message   // system prompt (rebuilt by ContextStage)
	history []providers.Message // conversation history
	pending []providers.Message // new messages this iteration (flushed at checkpoint)
}

// NewMessageBuffer creates a buffer with the given system message.
func NewMessageBuffer(system providers.Message) *MessageBuffer {
	return &MessageBuffer{system: system}
}

// All returns system + history + pending as a single slice for LLM calls.
func (mb *MessageBuffer) All() []providers.Message {
	out := make([]providers.Message, 0, 1+len(mb.history)+len(mb.pending))
	out = append(out, mb.system)
	out = append(out, mb.history...)
	out = append(out, mb.pending...)
	return out
}

// System returns the system message.
func (mb *MessageBuffer) System() providers.Message { return mb.system }

// SetSystem replaces the system message (ContextStage rebuilds it).
func (mb *MessageBuffer) SetSystem(msg providers.Message) { mb.system = msg }

// History returns conversation history (read-only view).
func (mb *MessageBuffer) History() []providers.Message { return mb.history }

// SetHistory replaces history (used when loading from session store).
func (mb *MessageBuffer) SetHistory(msgs []providers.Message) { mb.history = msgs }

// AppendPending adds a new message to the pending buffer.
func (mb *MessageBuffer) AppendPending(msg providers.Message) {
	mb.pending = append(mb.pending, msg)
}

// Pending returns pending messages (read-only view).
func (mb *MessageBuffer) Pending() []providers.Message { return mb.pending }

// FlushPending moves pending messages to history and returns them.
func (mb *MessageBuffer) FlushPending() []providers.Message {
	flushed := mb.pending
	mb.history = append(mb.history, mb.pending...)
	mb.pending = nil
	return flushed
}

// ReplaceHistory replaces history after compaction.
func (mb *MessageBuffer) ReplaceHistory(msgs []providers.Message) {
	mb.history = msgs
	mb.pending = nil // compaction absorbs pending
}

// HistoryLen returns history count (excludes system + pending).
func (mb *MessageBuffer) HistoryLen() int { return len(mb.history) }

// TotalLen returns total message count including system.
func (mb *MessageBuffer) TotalLen() int {
	return 1 + len(mb.history) + len(mb.pending)
}
