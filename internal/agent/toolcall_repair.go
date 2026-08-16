package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// toolCallRepairRawArgumentsKey is the ToolCall.Metadata key that carries the
// raw (pre-parse) arguments JSON when a provider could not parse it.
// Providers that retain the raw fragment (streaming accumulators) set this so
// repairToolCallArgs can recover the truncated payload. Repair is a no-op when
// the raw fragment was not retained.
const toolCallRepairRawArgumentsKey = "raw_arguments"

// repairToolCallArgs repairs a tool call before its arguments are consumed:
//   - stray "arg"/"args" field names are moved onto "arguments",
//   - nested {name, arguments:{...}} objects are flattened,
//   - explicit parse errors (truncated JSON) are repaired when the raw
//     arguments survive on the call (strict-safe repairJSON, schema-verified).
//
// It runs before the arguments are unmarshalled at the provider boundary
// (normalizeToolCall chain in loop.go); by the time a ToolCall reaches tool
// execution, wrong field names have already been dropped into map[string]any.
// The ToolCall struct shape is not changed.
//
// Returns true when the call was modified. A repair that still fails leaves
// ParseError in place — the existing think-stage retry hint handles it.
func (l *Loop) repairToolCallArgs(tc *providers.ToolCall) bool {
	if tc == nil {
		return false
	}
	if repairStrayArgFields(tc) {
		// A field-name fix is a deterministic recovery — the call is usable,
		// so any stale parse error (set when the arguments were decoded into
		// the wrong shape) is cleared.
		tc.ParseError = ""
		slog.Warn("tool_call_repair: arguments normalized",
			"tool", tc.Name, "call_id", tc.ID)
		return true
	}
	if tc.ParseError == "" {
		return false
	}
	return l.repairParseError(tc)
}

// repairStrayArgFields normalizes field-name deviation on the arguments map.
// Deterministic, meaning-preserving fixes only — no guessing:
//   - nested {name, arguments:{...}} objects are flattened to the inner
//     arguments object (JSON string or object value),
//   - a single stray "arg"/"args" key is moved onto "arguments" (JSON string
//     values are parsed when they decode into an object, otherwise kept as a
//     single-key wrapper — arguments must be an object at tool execution).
//
// "arguments" wins when several candidates carry distinct values — there is no
// way to disambiguate that shape.
func repairStrayArgFields(tc *providers.ToolCall) bool {
	args := tc.Arguments
	if args == nil {
		return false
	}
	// Nested {name, arguments:{...}} → flatten.
	if inner, ok := args["arguments"].(map[string]any); ok {
		tc.Arguments = inner
		return true
	}
	if s, ok := args["arguments"].(string); ok {
		inner := make(map[string]any)
		if err := json.Unmarshal([]byte(s), &inner); err == nil && inner != nil {
			tc.Arguments = inner
			return true
		}
	}
	// Field-name deviation: "arg"/"args" → "arguments".
	for _, stray := range []string{"arg", "args"} {
		v, ok := args[stray]
		if !ok {
			continue
		}
		if m, ok := v.(map[string]any); ok {
			tc.Arguments = m
		} else if s, ok := v.(string); ok {
			m := make(map[string]any)
			if err := json.Unmarshal([]byte(s), &m); err == nil && m != nil {
				tc.Arguments = m
			} else {
				tc.Arguments = map[string]any{stray: s}
			}
		} else {
			tc.Arguments = map[string]any{stray: v}
		}
		return true
	}
	return false
}

// repairParseError applies the strict-safe generic JSON repair to the raw
// arguments fragment retained by the provider. The repaired payload is only
// accepted when it parses into an object and matches the tool's schema
// (required fields present) — anything else keeps the ParseError so the
// existing think-stage retry hint stays the owner of the failure.
func (l *Loop) repairParseError(tc *providers.ToolCall) bool {
	raw := tc.Metadata[toolCallRepairRawArgumentsKey]
	if raw == "" {
		// The provider did not retain the raw fragment — nothing to repair.
		return false
	}
	if !l.repairAttemptAllowed(repairKey{toolName: tc.Name, schemaHash: l.toolCallSchemaHash(*tc)}, len(raw)) {
		return false
	}
	repaired, ok := repairJSON([]byte(raw))
	if !ok {
		return false
	}
	args := make(map[string]any)
	if err := json.Unmarshal(repaired, &args); err != nil {
		return false
	}
	if params, ok := l.schemaForToolCall(*tc); ok && !schemaMatchesArgs(params, args) {
		slog.Warn("tool_call_repair: repaired args miss schema-required fields — keeping parse error",
			"tool", tc.Name, "call_id", tc.ID)
		return false
	}
	tc.Arguments = args
	tc.ParseError = ""
	slog.Warn("tool_call_repair: truncated arguments repaired",
		"tool", tc.Name, "call_id", tc.ID, "raw_len", len(raw))
	return true
}

// repairKey identifies a repair shape: the tool name plus the hash of its
// schema. Schema is the ground truth for the field-name repair decision, so
// the same tool with a different schema (per-user overlay) gets its own entry.
type repairKey struct {
	toolName   string
	schemaHash string
}

// repairEntry records the last repair attempt for a shape. A repeated attempt
// with the same raw length is skipped so a malformed call shape cannot trigger
// unbounded repair work across iterations (LRU-bounded cache).
type repairEntry struct {
	attempts   int
	lastRawLen int
}

// repairLRUCache is a small LRU of per-shape repair decisions. It only bounds
// attempts; it never decides the repair outcome itself (repairJSON does).
type repairLRUCache struct {
	mu    sync.Mutex
	cap   int
	keys  []string
	attrs map[string]repairEntry
}

const repairCacheCapacity = 1024

func (c *repairLRUCache) touch(k string) {
	for i, old := range c.keys {
		if old == k {
			c.keys = append(c.keys[:i], c.keys[i+1:]...)
			break
		}
	}
	c.keys = append(c.keys, k)
	for len(c.keys) > c.cap {
		old := c.keys[0]
		c.keys = c.keys[1:]
		delete(c.attrs, old)
	}
}

// repairAttemptAllowed gates repair attempts per (toolName, schemaHash).
// A second attempt for the same shape with the same raw length is skipped.
func (l *Loop) repairAttemptAllowed(key repairKey, rawLen int) bool {
	if key.schemaHash == "" {
		// Unknown schema (no registry): the think-stage retry bound
		// (maxTruncRetries) is the loop guard — no cache needed.
		return true
	}
	cache := l.repairCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	k := key.toolName + "/" + key.schemaHash
	if e, ok := cache.attrs[k]; ok && e.lastRawLen == rawLen {
		return false
	}
	cache.attrs[k] = repairEntry{attempts: 1, lastRawLen: rawLen}
	cache.touch(k)
	return true
}

func (l *Loop) repairCache() *repairLRUCache {
	l.repairMu.Lock()
	defer l.repairMu.Unlock()
	if l.repairCacheSet == nil {
		l.repairCacheSet = &repairLRUCache{
			cap:   repairCacheCapacity,
			attrs: make(map[string]repairEntry),
		}
	}
	return l.repairCacheSet
}

// schemaForToolCall resolves the tool's JSON parameters schema from the
// registry. Returns (nil, false) when no registry or tool exists.
func (l *Loop) schemaForToolCall(tc providers.ToolCall) (map[string]any, bool) {
	if l.registry == nil {
		return nil, false
	}
	tool, ok := l.registry.Get(tc.Name)
	if !ok {
		return nil, false
	}
	params := tool.Parameters()
	if params == nil {
		return nil, false
	}
	return params, true
}

// toolCallSchemaHash hashes the tool's parameters schema. The empty string
// means the schema is unknown (repair is not cached in that case).
func (l *Loop) toolCallSchemaHash(tc providers.ToolCall) string {
	params, ok := l.schemaForToolCall(tc)
	if !ok {
		return ""
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// schemaMatchesArgs reports whether args satisfies the schema's required
// fields. Schemas without a required list are always satisfied — strictness
// only applies to fields the schema explicitly demands.
func schemaMatchesArgs(params map[string]any, args map[string]any) bool {
	req, ok := params["required"]
	if !ok {
		return true
	}
	names, ok := requiredNames(req)
	if !ok || len(names) == 0 {
		return true
	}
	for _, name := range names {
		if _, present := args[name]; !present {
			return false
		}
	}
	return true
}

func requiredNames(v any) ([]string, bool) {
	switch list := v.(type) {
	case []string:
		return list, true
	case []any:
		names := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok && s != "" {
				names = append(names, s)
			}
		}
		return names, true
	default:
		return nil, false
	}
}
