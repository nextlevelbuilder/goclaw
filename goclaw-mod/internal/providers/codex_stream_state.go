package providers

import (
	"crypto/sha256"
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

// codexImageAccum tracks streaming image generation items keyed by item_id.
// It stores the last-seen partial frame (deduplicated via SHA256) and the final result.
// Not thread-safe — designed for sequential SSE processing in a single goroutine.
type codexImageAccum struct {
	outputFormat    string
	lastPartialHash [sha256.Size]byte // SHA256 of last partial_image_b64; zeroed if none
	hasPartial      bool              // true once at least one partial is recorded
	finalB64        string            // filled on response.output_item.done or response.completed
}

// codexImageState tracks all image accumulators for a single streaming response.
type codexImageState struct {
	items       map[string]*codexImageAccum // keyed by item_id
	insertOrder []string                    // preserves emission order for final assembly
}

func newCodexImageState() *codexImageState {
	return &codexImageState{items: make(map[string]*codexImageAccum)}
}

func (s *codexImageState) ensureItem(itemID, outputFormat string) *codexImageAccum {
	if acc, ok := s.items[itemID]; ok {
		return acc
	}
	acc := &codexImageAccum{outputFormat: outputFormat}
	s.items[itemID] = acc
	s.insertOrder = append(s.insertOrder, itemID)
	return acc
}

// recordPartial stores a partial frame, deduplicating by SHA256.
// Returns true if the frame is new (not a duplicate) and was recorded.
func (s *codexImageState) recordPartial(itemID, outputFormat, b64 string) bool {
	if b64 == "" {
		return false
	}
	acc := s.ensureItem(itemID, outputFormat)
	if outputFormat != "" {
		acc.outputFormat = outputFormat
	}
	h := sha256.Sum256([]byte(b64))
	if acc.hasPartial && acc.lastPartialHash == h {
		return false // duplicate frame
	}
	acc.lastPartialHash = h
	acc.hasPartial = true
	return true
}

// recordFinal stores the final base64 image for an item.
func (s *codexImageState) recordFinal(itemID, outputFormat, b64 string) {
	if b64 == "" {
		return
	}
	acc := s.ensureItem(itemID, outputFormat)
	if outputFormat != "" {
		acc.outputFormat = outputFormat
	}
	acc.finalB64 = b64
}

// appendToResponse appends all completed images (those with a final) to result.Images
// in insertion order. Deduplication by item_id is implicit (each item appears once).
func (s *codexImageState) appendToResponse(result *ChatResponse) {
	for _, id := range s.insertOrder {
		acc := s.items[id]
		if acc.finalB64 == "" {
			continue
		}
		result.Images = append(result.Images, ImageContent{
			MimeType: mimeFromFormat(acc.outputFormat),
			Data:     acc.finalB64,
		})
	}
}

// mimeFromFormat converts an output_format string to a MIME type.
// Defaults to "image/png" for unknown or empty formats.
func mimeFromFormat(format string) string {
	switch format {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}
