package channels

import (
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

const (
	reasoningBubbleMaxRunes         = 1200
	reasoningBubbleMaxMessages      = 6
	reasoningBubbleMaxRetainedRunes = reasoningBubbleMaxRunes * reasoningBubbleMaxMessages
	reasoningBubbleFlushInterval    = 900 * time.Millisecond
)

type reasoningBubbleBuffer struct {
	pending       []rune
	retainedRunes int
	emitted       int
	truncated     bool
}

func (b *reasoningBubbleBuffer) append(content string) []string {
	if content == "" || b.emitted >= reasoningBubbleMaxMessages {
		return nil
	}
	for _, r := range content {
		if b.retainedRunes >= reasoningBubbleMaxRetainedRunes {
			b.truncated = true
			break
		}
		b.pending = append(b.pending, r)
		b.retainedRunes++
	}
	if len(b.pending) >= reasoningBubbleMaxRunes || b.truncated {
		return b.flush()
	}
	return nil
}

func (b *reasoningBubbleBuffer) flush() []string {
	if len(b.pending) == 0 || b.emitted >= reasoningBubbleMaxMessages {
		return nil
	}
	var messages []string
	for len(b.pending) > 0 && b.emitted < reasoningBubbleMaxMessages {
		n := min(len(b.pending), reasoningBubbleMaxRunes)
		chunk := string(b.pending[:n])
		b.pending = b.pending[n:]
		if len(b.pending) == 0 && b.truncated {
			chunk += "\n\n_Reasoning truncated._"
		}
		b.emitted++
		messages = append(messages, formatReasoningBubble(chunk, b.emitted))
	}
	if b.emitted >= reasoningBubbleMaxMessages {
		b.pending = nil
	}
	return messages
}

func formatReasoningBubble(content string, index int) string {
	if index <= 1 {
		return "_Reasoning:_\n" + content
	}
	return "_Reasoning continued:_\n" + content
}

func reasoningDelta(previous, next string) string {
	if next == "" || next == previous {
		return ""
	}
	if previous != "" && strings.HasPrefix(next, previous) {
		return next[len(previous):]
	}
	return next
}

func (m *Manager) appendReasoningBubble(runID string, rc *RunContext, content string) {
	var messages []string
	rc.mu.Lock()
	if !rc.ReasoningDelivery.BubbleDelivery {
		rc.mu.Unlock()
		return
	}
	if rc.reasoningBubbles == nil {
		rc.reasoningBubbles = &reasoningBubbleBuffer{}
	}
	messages = rc.reasoningBubbles.append(content)
	if len(messages) > 0 {
		stopReasoningBubbleTimerLocked(rc)
	} else if len(rc.reasoningBubbles.pending) > 0 && rc.reasoningBubbleTimer == nil {
		rc.reasoningBubbleTimer = time.AfterFunc(reasoningBubbleFlushInterval, func() {
			m.flushReasoningBubbles(runID)
		})
	}
	rc.mu.Unlock()
	m.publishReasoningBubbles(rc, messages)
}

func (m *Manager) appendReasoningBubbleFromThinkTagChunk(runID string, rc *RunContext, content string) {
	var delta string
	var done bool

	rc.mu.Lock()
	if rc.thinkingDone || rc.tagParseSkipped {
		rc.mu.Unlock()
		return
	}

	candidate := rc.streamBuffer + content
	split := SplitThinkTags(candidate)
	if split.Thinking == "" {
		rc.tagParseSkipped = true
		rc.mu.Unlock()
		return
	}

	previousThinking := rc.thinkingBuffer
	rc.streamBuffer = candidate
	rc.thinkingBuffer = split.Thinking
	if !split.Partial {
		rc.thinkingDone = true
		rc.streamBuffer = split.Answer
		done = true
	}
	delta = reasoningDelta(previousThinking, split.Thinking)
	rc.mu.Unlock()

	if delta != "" {
		m.appendReasoningBubble(runID, rc, delta)
	}
	if done {
		m.flushReasoningBubbles(runID)
	}
}

func (m *Manager) flushReasoningBubbles(runID string) {
	val, ok := m.runs.Load(runID)
	if !ok {
		return
	}
	rc, ok := val.(*RunContext)
	if !ok {
		return
	}
	m.flushReasoningBubblesForContext(rc)
}

func (m *Manager) flushReasoningBubblesForContext(rc *RunContext) {
	var messages []string
	rc.mu.Lock()
	stopReasoningBubbleTimerLocked(rc)
	if rc.reasoningBubbles != nil {
		messages = rc.reasoningBubbles.flush()
	}
	rc.mu.Unlock()
	m.publishReasoningBubbles(rc, messages)
}

func stopReasoningBubbleTimerLocked(rc *RunContext) {
	if rc.reasoningBubbleTimer != nil {
		rc.reasoningBubbleTimer.Stop()
		rc.reasoningBubbleTimer = nil
	}
}

func (m *Manager) publishReasoningBubbles(rc *RunContext, messages []string) {
	for _, content := range messages {
		msg := bus.OutboundMessage{
			Channel:  rc.ChannelName,
			ChatID:   rc.ChatID,
			Content:  content,
			Metadata: copyRoutingMeta(rc.Metadata),
			TenantID: rc.TenantID,
		}
		if !m.bus.TryPublishOutbound(msg) {
			slog.Warn("reasoning bubble dropped because outbound queue is full", "channel", rc.ChannelName)
		}
	}
}
