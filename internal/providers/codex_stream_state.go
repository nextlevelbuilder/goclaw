package providers

import (
	"fmt"
	"sort"
	"strings"
)

// codexMessageStreamState tracks message items and their text parts during
// SSE streaming from the Codex/Responses API. It enables recovery of assistant
// text from multiple completion event types (output_text.done, output_item.done,
// response.completed) and filters out commentary-phase messages.
//
// Not thread-safe — designed for sequential SSE processing in a single goroutine.
type codexMessageStreamState struct {
	messages map[string]*codexMessageState
}

type codexMessageState struct {
	id          string
	outputIndex int
	phase       string
	parts       map[int]*codexTextPartState
}

type codexTextPartState struct {
	text         string
	emittedBytes int
}

func newCodexMessageStreamState() *codexMessageStreamState {
	return &codexMessageStreamState{messages: make(map[string]*codexMessageState)}
}

func (s *codexMessageStreamState) registerMessageItem(itemID string, outputIndex int, item *codexItem) {
	if item == nil || item.Type != "message" {
		return
	}
	msg := s.ensureMessage(itemID, item.ID, outputIndex)
	if item.Phase != "" {
		msg.phase = item.Phase
	}
	if msg.outputIndex == 0 && outputIndex != 0 {
		msg.outputIndex = outputIndex
	}
	for idx, part := range item.Content {
		if part.Type != "output_text" || part.Text == "" {
			continue
		}
		textPart := msg.ensurePart(idx)
		textPart.text = part.Text
	}
}

func (s *codexMessageStreamState) recordTextDelta(itemID string, outputIndex, contentIndex int, delta string, result *ChatResponse, onChunk func(StreamChunk)) {
	if delta == "" {
		return
	}
	msg := s.ensureMessage(itemID, "", outputIndex)
	part := msg.ensurePart(contentIndex)
	part.text += delta
	if !shouldEmitCodexPhase(msg.phase) {
		return
	}
	msg.flushContiguous(result, onChunk)
}

func (s *codexMessageStreamState) recordFinalText(itemID string, outputIndex, contentIndex int, text string, result *ChatResponse, onChunk func(StreamChunk)) {
	if text == "" {
		return
	}
	msg := s.ensureMessage(itemID, "", outputIndex)
	part := msg.ensurePart(contentIndex)
	prev := part.text
	part.text = text
	if !shouldEmitCodexPhase(msg.phase) {
		return
	}
	part.reconcileCompleted(prev)
	msg.flushContiguous(result, onChunk)
}

func (s *codexMessageStreamState) flushMessage(itemID string, result *ChatResponse, onChunk func(StreamChunk)) {
	msg, ok := s.messages[itemID]
	if !ok || !shouldEmitCodexPhase(msg.phase) {
		return
	}
	msg.flushContiguous(result, onChunk)
}

func (s *codexMessageStreamState) ingestCompletedResponse(resp *codexAPIResponse) {
	if resp == nil {
		return
	}
	for i := range resp.Output {
		item := &resp.Output[i]
		if item.Type != "message" {
			continue
		}
		s.registerMessageItem(item.ID, i, item)
	}
}

func (s *codexMessageStreamState) flushCompletedResponse(result *ChatResponse, onChunk func(StreamChunk)) {
	ordered := s.preferredMessages()
	for _, msg := range ordered {
		msg.flushContiguous(result, onChunk)
	}
}

func (s *codexMessageStreamState) updateResultPhase(result *ChatResponse) {
	if result == nil || result.Phase == "final_answer" {
		return
	}
	for _, msg := range s.messages {
		if msg.phase == "final_answer" {
			result.Phase = "final_answer"
			return
		}
	}
}

// preferredMessages returns messages ordered by outputIndex, preferring
// final_answer phase. Falls back to non-commentary messages if no
// final_answer is found.
func (s *codexMessageStreamState) preferredMessages() []*codexMessageState {
	if len(s.messages) == 0 {
		return nil
	}
	ordered := make([]*codexMessageState, 0, len(s.messages))
	for _, msg := range s.messages {
		if msg.phase == "final_answer" {
			ordered = append(ordered, msg)
		}
	}
	if len(ordered) == 0 {
		for _, msg := range s.messages {
			if msg.phase != "commentary" {
				ordered = append(ordered, msg)
			}
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].outputIndex < ordered[j].outputIndex
	})
	return ordered
}

func codexEventItemKey(eventItemID string, item *codexItem) string {
	if eventItemID != "" {
		return eventItemID
	}
	if item != nil {
		return item.ID
	}
	return ""
}

func (s *codexMessageStreamState) ensureMessage(itemID, fallbackID string, outputIndex int) *codexMessageState {
	key := itemID
	if key == "" {
		key = fallbackID
	}
	if key == "" {
		key = fmt.Sprintf("output:%d", outputIndex)
	}
	if msg, ok := s.messages[key]; ok {
		if msg.outputIndex == 0 && outputIndex != 0 {
			msg.outputIndex = outputIndex
		}
		return msg
	}
	msg := &codexMessageState{
		id:          key,
		outputIndex: outputIndex,
		parts:       make(map[int]*codexTextPartState),
	}
	s.messages[key] = msg
	return msg
}

func (m *codexMessageState) ensurePart(contentIndex int) *codexTextPartState {
	if part, ok := m.parts[contentIndex]; ok {
		return part
	}
	part := &codexTextPartState{}
	m.parts[contentIndex] = part
	return part
}

func (m *codexMessageState) flushContiguous(result *ChatResponse, onChunk func(StreamChunk)) {
	for nextIndex := 0; ; nextIndex++ {
		part, ok := m.parts[nextIndex]
		if !ok {
			return
		}
		part.emitMissing(result, onChunk)
	}
}

func (p *codexTextPartState) emitMissing(result *ChatResponse, onChunk func(StreamChunk)) {
	if p.text == "" || p.emittedBytes >= len(p.text) {
		return
	}
	suffix := p.text[p.emittedBytes:]
	appendCodexContent(result, suffix, onChunk)
	p.emittedBytes = len(p.text)
}

// reconcileCompleted adjusts emittedBytes after the final text replaces delta-
// accumulated text. The SSE protocol guarantees that output_text.done carries
// the same content as the concatenated deltas, so the emitted prefix is always
// a valid prefix of the final text. The guard handles rare edge cases where
// the provider omits some deltas.
func (p *codexTextPartState) reconcileCompleted(previous string) {
	if p.text == "" {
		return
	}
	if len(previous) >= p.emittedBytes && strings.HasPrefix(p.text, previous[:p.emittedBytes]) {
		return
	}
	if p.emittedBytes > len(p.text) {
		p.emittedBytes = len(p.text)
	}
}

func shouldEmitCodexPhase(phase string) bool {
	return phase == "" || phase == "final_answer"
}

func appendCodexContent(result *ChatResponse, text string, onChunk func(StreamChunk)) {
	if text == "" {
		return
	}
	result.Content += text
	if onChunk != nil {
		onChunk(StreamChunk{Content: text})
	}
}
