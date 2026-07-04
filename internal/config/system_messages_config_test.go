package config

import "testing"

func TestSystemMsgConfig_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"
	cfg := Default()
	cfg.Messages = SystemMsgConfig{Messages: map[string]LocalizedSystemMessage{
		"pairing.group_required": {
			"en": "Custom group pairing code {{code}}",
			"vi": "Ma ghep nhom {{code}}",
		},
	}}
	cfg.Messages.DefaultLocale = "vi"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if got := loaded.Messages.Messages["pairing.group_required"]["vi"]; got != "Ma ghep nhom {{code}}" {
		t.Errorf("system message round-trip: got %q", got)
	}
	if loaded.Messages.DefaultLocale != "vi" {
		t.Errorf("system message default locale round-trip: got %q", loaded.Messages.DefaultLocale)
	}
}

func TestSystemMsgConfig_CloneAndReplaceFromDeepCopy(t *testing.T) {
	src := Default()
	src.Messages = SystemMsgConfig{Messages: map[string]LocalizedSystemMessage{
		"pairing.approved": {"en": "Approved {{app_name}}"},
	}}
	src.Messages.DefaultLocale = "vi"

	clone := src.Clone()
	if got := clone.Messages.Messages["pairing.approved"]["en"]; got != "Approved {{app_name}}" {
		t.Fatalf("Clone system messages = %#v", clone.Messages)
	}
	if clone.Messages.DefaultLocale != "vi" {
		t.Fatalf("Clone system messages default locale = %q", clone.Messages.DefaultLocale)
	}
	clone.Messages.Messages["pairing.approved"]["en"] = "mutated"
	if got := src.Messages.Messages["pairing.approved"]["en"]; got != "Approved {{app_name}}" {
		t.Fatalf("Clone should deep-copy system messages, src got %q", got)
	}

	dst := Default()
	dst.ReplaceFrom(src)
	if got := dst.Messages.Messages["pairing.approved"]["en"]; got != "Approved {{app_name}}" {
		t.Fatalf("ReplaceFrom system messages = %#v", dst.Messages)
	}
	if dst.Messages.DefaultLocale != "vi" {
		t.Fatalf("ReplaceFrom system messages default locale = %q", dst.Messages.DefaultLocale)
	}
	dst.Messages.Messages["pairing.approved"]["en"] = "mutated"
	if got := src.Messages.Messages["pairing.approved"]["en"]; got != "Approved {{app_name}}" {
		t.Fatalf("ReplaceFrom should deep-copy system messages, src got %q", got)
	}
}
