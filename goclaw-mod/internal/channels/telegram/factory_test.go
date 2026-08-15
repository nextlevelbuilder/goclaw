package telegram

import (
	"encoding/json"
	"testing"
)

const validBotToken = "123456:abcdefghijklmnopqrstuvwxyzABCDE1234"

func TestFactory_PropagatesGroupsAndTopics(t *testing.T) {
	creds := json.RawMessage(`{"token":"` + validBotToken + `"}`)
	cfg := json.RawMessage(`{
		"groups": {
			"-1003957927661": {
				"enabled": false,
				"topics": {
					"13": { "enabled": true }
				}
			}
		}
	}`)

	ch, err := Factory("telegram-test", creds, cfg, nil, nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	tg, ok := ch.(*Channel)
	if !ok {
		t.Fatalf("Factory returned %T, want *Channel", ch)
	}

	if tg.config.Groups == nil {
		t.Fatal("config.Groups is nil — dashboard groups/topics payload was dropped on unmarshal")
	}
	group, ok := tg.config.Groups["-1003957927661"]
	if !ok || group == nil {
		t.Fatal("group -1003957927661 missing from resolved config")
	}
	if group.Enabled == nil || *group.Enabled != false {
		t.Errorf("group.Enabled = %v, want false", group.Enabled)
	}

	allowed := resolveTopicConfig(tg.config, "-1003957927661", 13)
	if !allowed.isEnabled() {
		t.Error("topic 13: isEnabled() = false, want true")
	}
	other := resolveTopicConfig(tg.config, "-1003957927661", 99)
	if other.isEnabled() {
		t.Error("topic 99: isEnabled() = true, want false")
	}
	general := resolveTopicConfig(tg.config, "-1003957927661", telegramGeneralTopicID)
	if general.isEnabled() {
		t.Error("General topic: isEnabled() = true, want false")
	}
}

func TestFactory_NilGroupsWhenAbsent(t *testing.T) {
	creds := json.RawMessage(`{"token":"` + validBotToken + `"}`)
	cfg := json.RawMessage(`{"group_policy":"open"}`)

	ch, err := Factory("telegram-test", creds, cfg, nil, nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	tg := ch.(*Channel)
	if tg.config.Groups != nil {
		t.Errorf("config.Groups = %v, want nil when groups absent", tg.config.Groups)
	}
	def := resolveTopicConfig(tg.config, "-100123", 0)
	if !def.isEnabled() {
		t.Error("isEnabled() = false, want true by default when no overrides set")
	}
}
