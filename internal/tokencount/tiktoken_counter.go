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

	total := 0
	for _, m := range msgs {
		hash := messageHash(m)

		c.mu.RLock()
		cached, ok := c.msgCache[hash]
		c.mu.RUnlock()

		if ok {
			total += cached
			continue
		}

		count := len(enc.Encode(m.Content, nil, nil)) + PerMessageOverhead
		for _, tc := range m.ToolCalls {
			count += len(enc.Encode(tc.Name, nil, nil))
			count += len(enc.Encode(tc.ID, nil, nil))
		}

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
func messageHash(m providers.Message) uint64 {
	h := fnv.New64a()
	h.Write([]byte(m.Role))
	h.Write([]byte{0}) // separator
	h.Write([]byte(m.Content))
	for _, tc := range m.ToolCalls {
		h.Write([]byte{0})
		h.Write([]byte(tc.ID))
		h.Write([]byte(tc.Name))
	}
	return h.Sum64()
}

// NewTokenCounter creates the best available counter.
// Uses tiktoken if requested, falls back to rune/3 heuristic.
func NewTokenCounter(useTiktoken bool) TokenCounter {
	if useTiktoken {
		return NewTiktokenCounter()
	}
	return NewFallbackCounter()
}
