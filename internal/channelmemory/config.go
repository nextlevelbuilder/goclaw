package channelmemory

import (
	"encoding/json"
	"regexp"
	"slices"
	"time"
)

var DefaultAllowedTypes = []string{"people", "projects", "decisions", "todos", "preferences", "events"}

type Config struct {
	Enabled         bool     `json:"enabled"`
	ReviewMode      bool     `json:"review_mode"`
	IntervalMinutes int      `json:"interval_minutes"`
	MessageCap      int      `json:"message_cap"`
	RetentionHours  int      `json:"retention_hours"`
	AllowedTypes    []string `json:"allowed_types"`
	ExcludeUsers    []string `json:"exclude_users"`
	ExcludePatterns []string `json:"exclude_patterns"`
	MinMessages     int      `json:"min_messages"`
	GroupOnly       bool     `json:"group_only"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		ReviewMode:      true,
		IntervalMinutes: 360,
		MessageCap:      100,
		RetentionHours:  168,
		AllowedTypes:    slices.Clone(DefaultAllowedTypes),
		MinMessages:     5,
		GroupOnly:       true,
	}
}

func ParseConfig(raw json.RawMessage) Config {
	cfg := DefaultConfig()
	if len(raw) == 0 {
		return cfg
	}
	var root struct {
		PassiveMemory *Config `json:"passive_memory"`
	}
	if err := json.Unmarshal(raw, &root); err != nil || root.PassiveMemory == nil {
		return cfg
	}
	in := root.PassiveMemory
	cfg.Enabled = in.Enabled
	cfg.ReviewMode = in.ReviewMode
	cfg.IntervalMinutes = clampInt(in.IntervalMinutes, 15, 10080, cfg.IntervalMinutes)
	cfg.MessageCap = clampInt(in.MessageCap, 10, 1000, cfg.MessageCap)
	cfg.RetentionHours = clampInt(in.RetentionHours, 1, 720, cfg.RetentionHours)
	cfg.AllowedTypes = normalizeAllowedTypes(in.AllowedTypes)
	cfg.ExcludeUsers = boundedStrings(in.ExcludeUsers, 50, 255)
	cfg.ExcludePatterns = boundedPatterns(in.ExcludePatterns, 20, 255)
	cfg.MinMessages = clampInt(in.MinMessages, 2, 100, cfg.MinMessages)
	cfg.GroupOnly = true
	return cfg
}

func MergeIntoInstanceConfig(raw json.RawMessage, cfg Config) json.RawMessage {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil || root == nil {
		root = make(map[string]any)
	}
	root["passive_memory"] = cfg
	out, _ := json.Marshal(root)
	return out
}

func (c Config) Interval() time.Duration {
	return time.Duration(c.IntervalMinutes) * time.Minute
}

func normalizeAllowedTypes(in []string) []string {
	allowed := make([]string, 0, len(DefaultAllowedTypes))
	for _, v := range in {
		if slices.Contains(DefaultAllowedTypes, v) && !slices.Contains(allowed, v) {
			allowed = append(allowed, v)
		}
	}
	if len(allowed) == 0 {
		return slices.Clone(DefaultAllowedTypes)
	}
	return allowed
}

func boundedStrings(in []string, maxCount, maxLen int) []string {
	out := make([]string, 0, min(len(in), maxCount))
	for _, v := range in {
		if v == "" || len(v) > maxLen {
			continue
		}
		out = append(out, v)
		if len(out) >= maxCount {
			break
		}
	}
	return out
}

func boundedPatterns(in []string, maxCount, maxLen int) []string {
	candidates := boundedStrings(in, maxCount, maxLen)
	out := candidates[:0]
	for _, v := range candidates {
		if _, err := regexp.Compile(v); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func clampInt(v, minV, maxV, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
