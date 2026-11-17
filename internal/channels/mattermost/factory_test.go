package mattermost

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestFactory_BuildsChannelFromInstance(t *testing.T) {
	creds := json.RawMessage(`{"bot_token":"tok"}`)
	cfg := json.RawMessage(`{"server_url":"https://chat.example.com/","dm_policy":"open","history_limit":10,"group_dm_as_direct":true}`)

	ch, err := FactoryWithPendingStore(nil)("mm-team", creds, cfg, bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	mm := ch.(*Channel)
	if mm.Name() != "mm-team" {
		t.Fatalf("name = %q", mm.Name())
	}
	if mm.cfg.ServerURL != "https://chat.example.com" {
		t.Fatalf("server URL not normalized: %q", mm.cfg.ServerURL)
	}
	if mm.cfg.GroupPolicy != "pairing" {
		t.Fatalf("DB instances must default group_policy to pairing, got %q", mm.cfg.GroupPolicy)
	}
	if mm.HistoryLimit() != 10 || !mm.RequireMention() {
		t.Fatalf("history_limit=%d require_mention=%v", mm.HistoryLimit(), mm.RequireMention())
	}
	if !mm.flatChannel("G") {
		t.Fatal("group_dm_as_direct must be read from the instance config")
	}
}

func TestFactory_RejectsIncompleteInstance(t *testing.T) {
	cases := map[string][2]string{
		"missing token":      {`{}`, `{"server_url":"https://chat.example.com"}`},
		"missing server url": {`{"bot_token":"tok"}`, `{}`},
		"bad server url":     {`{"bot_token":"tok"}`, `{"server_url":"chat.example.com"}`},
		"bad config json":    {`{"bot_token":"tok"}`, `{`},
	}
	for name, tc := range cases {
		if _, err := FactoryWithPendingStore(nil)("mm", json.RawMessage(tc[0]), json.RawMessage(tc[1]), bus.New(), nil); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
