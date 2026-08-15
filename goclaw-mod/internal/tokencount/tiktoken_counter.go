package tokencount

import (
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// tokenizerToEncoding maps internal TokenizerID to tiktoken encoding names.
var tokenizerToEncoding = map[TokenizerID]string{
	TokenizerCL100K: "cl100k_base",
	TokenizerO200K:  "o200k_base",
}

// tiktokenCounter implements TokenCounter using tiktoken-go BPE encoding.
// Caches encoders per tokenizer ID and token counts per message content hash.
type tiktokenCounter struct {
	mu       sync.RWMutex
	encoders map[TokenizerID]*tiktoken.Tiktoken
	msgCache map[uint64]int
	fallback *FallbackCounter
}

// NewTiktokenCounter creates a tiktoken-based counter with fallback.
func NewTiktokenCounter() *tiktokenCounter {
	return &tiktokenCounter{
		encoders: make(map[TokenizerID]*tiktoken.Tiktoken),
		msgCache: make(map[uint64]int),
		fallback: NewFallbackCounter(),
	}
}

// Count returns BPE token count for text using the model's tokenizer.
// Falls back to rune/3 heuristic if encoder unavailable.
func (c *tiktokenCounter) Count(model string, text string) int {
	enc := c.encoderForModel(model)
	if enc == nil {
		return c.fallback.Count(model, text)
	}
	return len(enc.Encode(text, nil, nil))
}

// CountMessages returns token count for a message list with per-message overhead.
// Uses FNV-1a content hash cache to avoid re-encoding unchanged messages.
func (c *tiktokenCounter) CountMessages(model string, msgs []providers.Message) int {
	enc := c.encoderForModel(model)
	if enc == nil {
		return c.fallback.CountMessages(model, msgs)
	}

	// The per-message count depends on the tokenizer, so the cache key MUST
	// include the tokenizer identity. Otherwise a message counted with model A's
	// tokenizer (e.g. cl100k for Claude) would return that stale count when the
	// same message is re-counted for model B on a different tokenizer (e.g.
	// o200k for GPT-4o) — most visibly on fallback candidates. Models that share
	// a tokenizer correctly share cache entries.
	tokenizerID := resolveModelInfo(model).TokenizerID

	total := 0
	for _, m := range msgs {
		hash := messageHash(tokenizerID, m)

		c.mu.RLock()
		cached, ok := c.msgCache[hash]
		c.mu.RUnlock()

		if ok {
			total += cached
			continue
		}

		count := len(enc.Encode(m.Content, nil, nil)) + PerMessageOverhead
		// Thinking/reasoning is sent back to the provider on subsequent turns
		// (Anthropic requires it for tool-use passback), so it counts toward input.
		if m.Thinking != "" {
			count += len(enc.Encode(m.Thinking, nil, nil))
		}
		// Tool result correlation id (role="tool" messages).
		if m.ToolCallID != "" {
			count += len(enc.Encode(m.ToolCallID, nil, nil))
		}
		// Raw assistant content blocks (Anthropic thinking-block passback) are
		// serialized into the wire payload verbatim — count their bytes.
		if len(m.RawAssistantContent) > 0 {
			count += len(enc.Encode(string(m.RawAssistantContent), nil, nil))
		}
		for _, tc := range m.ToolCalls {
			count += len(enc.Encode(tc.Name, nil, nil))
			count += len(enc.Encode(tc.ID, nil, nil))
			// Tool call arguments are serialized as JSON into the request and can
			// dominate token cost; the previous counter ignored them entirely.
			count += encodeToolArgs(enc, tc.Arguments)
		}
		// Media (images/videos) carry a per-item token cost when sent inline.
		count += mediaTokenCost(m)

		c.mu.Lock()
		c.msgCache[hash] = count
		c.mu.Unlock()

		total += count
	}
	return total
}

// CountToolSchemas returns BPE token count for the JSON-serialised tool list.
// Falls back to FallbackCounter if the encoder is unavailable.
// Returns 0 for nil or empty slice.
func (c *tiktokenCounter) CountToolSchemas(model string, tools []providers.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	enc := c.encoderForModel(model)
	if enc == nil {
		return c.fallback.CountToolSchemas(model, tools)
	}
	blob, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return len(enc.Encode(string(blob), nil, nil))
}

// ModelContextWindow delegates to FallbackCounter (same prefix-match logic).
func (c *tiktokenCounter) ModelContextWindow(model string) int {
	return c.fallback.ModelContextWindow(model)
}

// ResetCache clears the per-message token cache.
// Called after compaction replaces messages. Encoders are kept.
func (c *tiktokenCounter) ResetCache() {
	c.mu.Lock()
	c.msgCache = make(map[uint64]int)
	c.mu.Unlock()
}

// encoderForModel resolves and caches the tiktoken encoder for a model.
// Returns nil if model uses fallback tokenizer or encoder fails to load.
func (c *tiktokenCounter) encoderForModel(model string) *tiktoken.Tiktoken {
	info := resolveModelInfo(model)
	if info.TokenizerID == TokenizerFallback {
		return nil
	}

	c.mu.RLock()
	enc, ok := c.encoders[info.TokenizerID]
	c.mu.RUnlock()
	if ok {
		return enc
	}

	encodingName, exists := tokenizerToEncoding[info.TokenizerID]
	if !exists {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if enc, ok := c.encoders[info.TokenizerID]; ok {
		return enc
	}

	enc, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		slog.Warn("tiktoken: failed to load encoding, using fallback",
			"encoding", encodingName, "err", err)
		return nil
	}

	c.encoders[info.TokenizerID] = enc
	return enc
}

// resolveModelInfo finds the best matching ModelInfo from DefaultRegistry.
// Uses longest-prefix match. Returns fallback if no match.
func resolveModelInfo(model string) ModelInfo {
	var best string
	for prefix := range DefaultRegistry {
		if len(prefix) > len(best) && len(model) >= len(prefix) && model[:len(prefix)] == prefix {
			best = prefix
		}
	}
	if best != "" {
		return DefaultRegistry[best]
	}
	return ModelInfo{TokenizerID: TokenizerFallback, ContextWindow: 200_000}
}

// messageHash computes FNV-1a hash of message content for cache keying.
// It MUST cover every field that CountMessages encodes; otherwise a payload
// whose thinking/args/tool-result/raw-blocks/media changed but whose Content is
// unchanged would return a stale cached count for a different wire payload.
// The tokenizer ID is folded in first because the token count is
// tokenizer-dependent: the same message yields different counts under cl100k vs
// o200k, so counts must not be shared across tokenizers.
func messageHash(tokenizerID TokenizerID, m providers.Message) uint64 {
	h := fnv.New64a()
	h.Write([]byte(tokenizerID))
	h.Write([]byte{0}) // separator
	h.Write([]byte(m.Role))
	h.Write([]byte{0}) // separator
	h.Write([]byte(m.Content))
	h.Write([]byte{0})
	h.Write([]byte(m.Thinking))
	h.Write([]byte{0})
	h.Write([]byte(m.ToolCallID))
	h.Write([]byte{0})
	h.Write(m.RawAssistantContent)
	for _, tc := range m.ToolCalls {
		h.Write([]byte{0})
		h.Write([]byte(tc.ID))
		h.Write([]byte(tc.Name))
		if b, err := json.Marshal(tc.Arguments); err == nil {
			h.Write(b)
		}
	}
	for _, img := range m.Images {
		h.Write([]byte{0})
		h.Write([]byte(img.MimeType))
		h.Write([]byte(img.URL))
	}
	for _, vid := range m.Videos {
		h.Write([]byte{0})
		h.Write([]byte(vid.MimeType))
		h.Write([]byte(vid.URL))
	}
	return h.Sum64()
}

// encodeToolArgs returns the BPE token count of a tool call's arguments as they
// are serialized into the request payload (JSON). Returns 0 when arguments are
// empty or cannot be marshalled.
func encodeToolArgs(enc *tiktoken.Tiktoken, args map[string]any) int {
	if len(args) == 0 {
		return 0
	}
	b, err := json.Marshal(args)
	if err != nil {
		return 0
	}
	return len(enc.Encode(string(b), nil, nil))
}

// mediaTokenCost approximates the token cost of inline media on a message.
// This is BEST-EFFORT, not a proven bound: a flat per-item estimate can
// under-count a large inline video whose true token cost far exceeds it. It
// exists so inline media is not counted as zero. Tool-internal media that
// travels out-of-band (provider file/transcription APIs) is measured separately
// by size in the caps guard (EstimateOutOfBandMediaTokens); this path only
// covers media actually embedded in the message. Callers that persist media as
// MediaRefs (not inlined) incur no cost here.
func mediaTokenCost(m providers.Message) int {
	const perInlineMediaItem = 1600 // best-effort estimate per inline image/video
	cost := 0
	for _, img := range m.Images {
		if img.Data != "" || img.URL != "" {
			cost += perInlineMediaItem
		}
	}
	for _, vid := range m.Videos {
		if vid.Data != "" || vid.URL != "" {
			cost += perInlineMediaItem
		}
	}
	return cost
}

// NewTokenCounter creates the best available counter.
// Uses tiktoken if requested, falls back to rune/3 heuristic.
func NewTokenCounter(useTiktoken bool) TokenCounter {
	if useTiktoken {
		return NewTiktokenCounter()
	}
	return NewFallbackCounter()
}
