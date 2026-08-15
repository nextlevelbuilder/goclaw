package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	SecureCLIEnvKindSensitive = "sensitive"
	SecureCLIEnvKindValue     = "value"
)

// SecureCLIEnvEntry is the stored per-key env representation; legacy KEY:string maps decode as sensitive.
type SecureCLIEnvEntry struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// SecureCLIEnvResponseEntry is safe to serialize in admin API responses.
type SecureCLIEnvResponseEntry struct {
	Kind   string  `json:"kind"`
	Value  *string `json:"value"`
	Masked bool    `json:"masked"`
}

func ParseSecureCLIEnv(raw []byte) (map[string]SecureCLIEnvEntry, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]SecureCLIEnvEntry{}, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	env := make(map[string]SecureCLIEnvEntry, len(payload))
	for key, item := range payload {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		entry, err := parseSecureCLIEnvEntry(item)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		env[key] = entry
	}
	return env, nil
}

func parseSecureCLIEnvEntry(raw json.RawMessage) (SecureCLIEnvEntry, error) {
	var legacy string
	if err := json.Unmarshal(raw, &legacy); err == nil {
		return SecureCLIEnvEntry{Kind: SecureCLIEnvKindSensitive, Value: legacy}, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] != '{' {
		value, err := secureCLIEnvValueAsString(raw)
		if err != nil {
			return SecureCLIEnvEntry{}, err
		}
		return SecureCLIEnvEntry{Kind: SecureCLIEnvKindSensitive, Value: value}, nil
	}

	var obj struct {
		Kind  string          `json:"kind"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return SecureCLIEnvEntry{}, err
	}

	kind := strings.ToLower(strings.TrimSpace(obj.Kind))
	if kind == "" {
		kind = SecureCLIEnvKindSensitive
	}
	if kind != SecureCLIEnvKindSensitive && kind != SecureCLIEnvKindValue {
		return SecureCLIEnvEntry{}, fmt.Errorf("invalid env kind %q", obj.Kind)
	}

	value, err := secureCLIEnvValueAsString(obj.Value)
	if err != nil {
		return SecureCLIEnvEntry{}, err
	}
	return SecureCLIEnvEntry{Kind: kind, Value: value}, nil
}

func secureCLIEnvValueAsString(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if b {
			return "true", nil
		}
		return "false", nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return fmt.Sprint(f), nil
	}
	return "", fmt.Errorf("env value must be string, number, bool, or null")
}

func SerializeSecureCLIEnv(env map[string]SecureCLIEnvEntry) ([]byte, error) {
	normalized := make(map[string]SecureCLIEnvEntry, len(env))
	for key, entry := range env {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(entry.Kind))
		if kind == "" {
			kind = SecureCLIEnvKindSensitive
		}
		if kind != SecureCLIEnvKindSensitive && kind != SecureCLIEnvKindValue {
			return nil, fmt.Errorf("%s: invalid env kind %q", key, entry.Kind)
		}
		normalized[key] = SecureCLIEnvEntry{Kind: kind, Value: entry.Value}
	}
	return json.Marshal(normalized)
}

func FlattenSecureCLIEnv(raw []byte) (map[string]string, error) {
	entries, err := ParseSecureCLIEnv(raw)
	if err != nil {
		return nil, err
	}
	flat := make(map[string]string, len(entries))
	for key, entry := range entries {
		flat[key] = entry.Value
	}
	return flat, nil
}

func MergeSecureCLIEnv(existingJSON []byte, incoming json.RawMessage) ([]byte, error) {
	existing, err := ParseSecureCLIEnv(existingJSON)
	if err != nil {
		return nil, fmt.Errorf("parse existing env: %w", err)
	}
	incomingEntries, err := ParseSecureCLIEnv(incoming)
	if err != nil {
		return nil, fmt.Errorf("parse incoming env: %w", err)
	}

	out := make(map[string]SecureCLIEnvEntry, len(incomingEntries))
	for key, entry := range incomingEntries {
		if entry.Kind == SecureCLIEnvKindSensitive && entry.Value == "" {
			if prev, ok := existing[key]; ok {
				prev.Kind = SecureCLIEnvKindSensitive
				out[key] = prev
				continue
			}
		}
		out[key] = entry
	}
	return SerializeSecureCLIEnv(out)
}

func SecureCLIEnvKeys(raw []byte) []string {
	env, err := ParseSecureCLIEnv(raw)
	if err != nil {
		return []string{}
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func SanitizeSecureCLIEnv(env map[string]SecureCLIEnvEntry) map[string]SecureCLIEnvResponseEntry {
	out := make(map[string]SecureCLIEnvResponseEntry, len(env))
	for key, entry := range env {
		if entry.Kind == SecureCLIEnvKindValue {
			value := entry.Value
			out[key] = SecureCLIEnvResponseEntry{Kind: SecureCLIEnvKindValue, Value: &value, Masked: false}
			continue
		}
		out[key] = SecureCLIEnvResponseEntry{Kind: SecureCLIEnvKindSensitive, Value: nil, Masked: true}
	}
	return out
}

func SanitizeSecureCLIEnvJSON(raw []byte) map[string]SecureCLIEnvResponseEntry {
	env, err := ParseSecureCLIEnv(raw)
	if err != nil {
		return map[string]SecureCLIEnvResponseEntry{}
	}
	return SanitizeSecureCLIEnv(env)
}
