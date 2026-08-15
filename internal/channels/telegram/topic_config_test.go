package telegram

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

//go:fix inline
func boolPtr(b bool) *bool { return new(b) }

func TestResolveTopicConfig_Defaults(t *testing.T) {
	cfg := config.TelegramConfig{
		GroupPolicy:    "open",
		RequireMention: new(true),
		AllowFrom:      []string{"user1"},
	}

	result := resolveTopicConfig(cfg, "-100123", 0)

	if result.groupPolicy != "open" {
		t.Errorf("groupPolicy = %q, want %q", result.groupPolicy, "open")
	}
	if result.requireMention == nil || *result.requireMention != true {
		t.Errorf("requireMention = %v, want true", result.requireMention)
	}
	if len(result.allowFrom) != 1 || result.allowFrom[0] != "user1" {
		t.Errorf("allowFrom = %v, want [user1]", result.allowFrom)
	}
	if !result.isEnabled() {
		t.Error("isEnabled() = false, want true")
	}
}

func TestResolveTopicConfig_WildcardGroup(t *testing.T) {
	cfg := config.TelegramConfig{
		GroupPolicy: "open",
		Groups: map[string]*config.TelegramGroupConfig{
			"*": {
				GroupPolicy:    "allowlist",
				RequireMention: new(false),
				AllowFrom:      []string{"admin1"},
			},
		},
	}

	result := resolveTopicConfig(cfg, "-100123", 0)

	if result.groupPolicy != "allowlist" {
		t.Errorf("groupPolicy = %q, want %q", result.groupPolicy, "allowlist")
	}
	if result.requireMention == nil || *result.requireMention != false {
		t.Errorf("requireMention = %v, want false", result.requireMention)
	}
	if len(result.allowFrom) != 1 || result.allowFrom[0] != "admin1" {
		t.Errorf("allowFrom = %v, want [admin1]", result.allowFrom)
	}
}

func TestResolveTopicConfig_SpecificGroupOverridesWildcard(t *testing.T) {
	cfg := config.TelegramConfig{
		GroupPolicy: "open",
		Groups: map[string]*config.TelegramGroupConfig{
			"*": {
				GroupPolicy:    "allowlist",
				RequireMention: new(false),
				AllowFrom:      []string{"admin1"},
				SystemPrompt:   "wildcard prompt",
			},
			"-100123": {
				GroupPolicy:  "disabled",
				AllowFrom:    []string{"user2"},
				SystemPrompt: "group prompt",
			},
		},
	}

	result := resolveTopicConfig(cfg, "-100123", 0)

	if result.groupPolicy != "disabled" {
		t.Errorf("groupPolicy = %q, want %q", result.groupPolicy, "disabled")
	}
	// requireMention not set on specific group → inherits wildcard
	if result.requireMention == nil || *result.requireMention != false {
		t.Errorf("requireMention = %v, want false (inherited from wildcard)", result.requireMention)
	}
	if len(result.allowFrom) != 1 || result.allowFrom[0] != "user2" {
		t.Errorf("allowFrom = %v, want [user2]", result.allowFrom)
	}
	if result.systemPrompt != "group prompt" {
		t.Errorf("systemPrompt = %q, want %q", result.systemPrompt, "group prompt")
	}
}

func TestResolveTopicConfig_TopicOverridesGroup(t *testing.T) {
	cfg := config.TelegramConfig{
		GroupPolicy: "open",
		Groups: map[string]*config.TelegramGroupConfig{
			"-100123": {
				RequireMention: new(true),
				SystemPrompt:   "group prompt",
				Skills:         []string{"skill_a", "skill_b"},
				Topics: map[string]*config.TelegramTopicConfig{
					"42": {
						RequireMention: new(false),
						Skills:         []string{"skill_c"},
						SystemPrompt:   "topic prompt",
					},
				},
			},
		},
	}

	result := resolveTopicConfig(cfg, "-100123", 42)

	if result.requireMention == nil || *result.requireMention != false {
		t.Errorf("requireMention = %v, want false (topic override)", result.requireMention)
	}
	if len(result.skills) != 1 || result.skills[0] != "skill_c" {
		t.Errorf("skills = %v, want [skill_c]", result.skills)
	}
	// systemPrompt: concatenated group + topic
	expected := "group prompt\n\ntopic prompt"
	if result.systemPrompt != expected {
		t.Errorf("systemPrompt = %q, want %q", result.systemPrompt, expected)
	}
}

func TestResolveTopicConfig_TopicSystemPromptConcatenation(t *testing.T) {
	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-100123": {
				SystemPrompt: "  group prompt  ",
				Topics: map[string]*config.TelegramTopicConfig{
					"5": {
						SystemPrompt: "  topic prompt  ",
					},
				},
			},
		},
	}

	result := resolveTopicConfig(cfg, "-100123", 5)

	expected := "group prompt\n\ntopic prompt"
	if result.systemPrompt != expected {
		t.Errorf("systemPrompt = %q, want %q", result.systemPrompt, expected)
	}
}

func TestResolveTopicConfig_TopicOnlySystemPrompt(t *testing.T) {
	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-100123": {
				Topics: map[string]*config.TelegramTopicConfig{
					"5": {
						SystemPrompt: "topic only",
					},
				},
			},
		},
	}

	result := resolveTopicConfig(cfg, "-100123", 5)

	if result.systemPrompt != "topic only" {
		t.Errorf("systemPrompt = %q, want %q", result.systemPrompt, "topic only")
	}
}

func TestResolveTopicConfig_DisabledTopic(t *testing.T) {
	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-100123": {
				Enabled: new(true),
				Topics: map[string]*config.TelegramTopicConfig{
					"42": {
						Enabled: new(false),
					},
				},
			},
		},
	}

	// Group is enabled
	groupResult := resolveTopicConfig(cfg, "-100123", 0)
	if !groupResult.isEnabled() {
		t.Error("group isEnabled() = false, want true")
	}

	// Topic 42 is disabled
	topicResult := resolveTopicConfig(cfg, "-100123", 42)
	if topicResult.isEnabled() {
		t.Error("topic isEnabled() = true, want false")
	}
}

func TestResolveTopicConfig_UnknownGroupFallsToWildcard(t *testing.T) {
	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"*": {
				GroupPolicy: "allowlist",
			},
			"-100999": {
				GroupPolicy: "disabled",
			},
		},
	}

	// Unknown group: gets wildcard
	result := resolveTopicConfig(cfg, "-100123", 0)
	if result.groupPolicy != "allowlist" {
		t.Errorf("groupPolicy = %q, want %q (wildcard)", result.groupPolicy, "allowlist")
	}

	// Known group: gets specific
	result2 := resolveTopicConfig(cfg, "-100999", 0)
	if result2.groupPolicy != "disabled" {
		t.Errorf("groupPolicy = %q, want %q (specific)", result2.groupPolicy, "disabled")
	}
}

func TestEffectiveRequireMention(t *testing.T) {
	// nil requireMention → use default
	r := resolvedTopicConfig{}
	if r.effectiveRequireMention(true) != true {
		t.Error("effectiveRequireMention(true) = false, want true")
	}
	if r.effectiveRequireMention(false) != false {
		t.Error("effectiveRequireMention(false) = true, want false")
	}

	// explicit requireMention overrides default
	r2 := resolvedTopicConfig{requireMention: new(false)}
	if r2.effectiveRequireMention(true) != false {
		t.Error("effectiveRequireMention(true) with override=false should be false")
	}
}

func TestResolveTopicConfig_EmptySkillsOverride(t *testing.T) {
	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-100123": {
				Skills: []string{"skill_a"},
				Topics: map[string]*config.TelegramTopicConfig{
					"42": {
						Skills: []string{}, // explicitly empty = no skills
					},
				},
			},
		},
	}

	result := resolveTopicConfig(cfg, "-100123", 42)

	if result.skills == nil {
		t.Error("skills should be non-nil (explicit empty override)")
	}
	if len(result.skills) != 0 {
		t.Errorf("skills = %v, want empty slice", result.skills)
	}
}
