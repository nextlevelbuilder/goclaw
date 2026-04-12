package pipeline

import (
	"encoding/json"
	"hash/fnv"
	"strings"
	"sync"
)

type NoveltyTracker struct {
	mu      sync.RWMutex
	entries map[string]*NoveltyEntry
}

type NoveltyEntry struct {
	CallCount        int            `json:"call_count"`
	LastArgsHash     uint64         `json:"last_args_hash"`
	LastResultHash   uint64         `json:"last_result_hash"`
	LastResultLen    int            `json:"last_result_len"`
	LastErrorSig     string         `json:"last_error_sig"`
	ConsecutiveSame  int            `json:"consecutive_same"`
	ShrinkingCount   int            `json:"shrinking_count"`
	ErrorRepeatCount int            `json:"error_repeat_count"`
	SeenArgs         map[uint64]int `json:"-"`
}

func NewNoveltyTracker() *NoveltyTracker {
	return &NoveltyTracker{entries: make(map[string]*NoveltyEntry)}
}

func (nt *NoveltyTracker) CheckExactRepeat(toolName string, args map[string]any) bool {
	if nt == nil {
		return false
	}
	key := ToolTargetKey(toolName, args)
	argsHash := hashValue(args)

	nt.mu.RLock()
	defer nt.mu.RUnlock()
	entry := nt.entries[key]
	if entry == nil || entry.SeenArgs == nil {
		return false
	}
	return entry.SeenArgs[argsHash] > 0
}

func (nt *NoveltyTracker) Record(
	toolName string,
	args map[string]any,
	resultContent string,
	isError bool,
) *NoveltyEntry {
	if nt == nil {
		return &NoveltyEntry{}
	}

	key := ToolTargetKey(toolName, args)
	argsHash := hashValue(args)
	resultHash := hashString(resultContent)
	resultLen := len(strings.TrimSpace(resultContent))
	errSig := errorSignature(resultContent)

	nt.mu.Lock()
	defer nt.mu.Unlock()

	entry := nt.entries[key]
	if entry == nil {
		entry = &NoveltyEntry{SeenArgs: make(map[uint64]int)}
		nt.entries[key] = entry
	}
	if entry.SeenArgs == nil {
		entry.SeenArgs = make(map[uint64]int)
	}

	entry.CallCount++
	entry.LastArgsHash = argsHash
	entry.SeenArgs[argsHash]++

	if entry.LastResultHash != 0 && resultHash != 0 && entry.LastResultHash == resultHash {
		entry.ConsecutiveSame++
	} else if resultHash != 0 {
		entry.ConsecutiveSame = 1
	} else {
		entry.ConsecutiveSame = 0
	}

	if entry.LastResultLen > 0 && resultLen > 0 && resultLen < entry.LastResultLen {
		entry.ShrinkingCount++
	} else if resultLen > 0 {
		entry.ShrinkingCount = 0
	}

	if isError && errSig != "" && errSig == entry.LastErrorSig {
		entry.ErrorRepeatCount++
	} else if isError && errSig != "" {
		entry.ErrorRepeatCount = 1
	} else {
		entry.ErrorRepeatCount = 0
	}

	entry.LastResultHash = resultHash
	entry.LastResultLen = resultLen
	entry.LastErrorSig = errSig

	copy := *entry
	return &copy
}

func hashValue(value any) uint64 {
	data, _ := json.Marshal(value)
	return hashStringBytes(data)
}

func hashString(value string) uint64 {
	return hashStringBytes([]byte(value))
}

func hashStringBytes(value []byte) uint64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write(value)
	return hasher.Sum64()
}

func errorSignature(value string) string {
	signature := strings.ToLower(strings.TrimSpace(value))
	if len(signature) > 100 {
		signature = signature[:100]
	}
	return signature
}
