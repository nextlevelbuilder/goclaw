package common

import "testing"

func TestInboundMeta_ToMap_AllFields(t *testing.T) {
	m := InboundMeta{
		MessageID:         "abc",
		Platform:          PlatformZaloOA,
		SenderDisplayName: "Alice",
	}
	got := m.ToMap()
	want := map[string]string{
		"message_id":          "abc",
		"platform":            "zalo_oa",
		"sender_display_name": "Alice",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestInboundMeta_ToMap_OmitsEmptyOptionals(t *testing.T) {
	m := InboundMeta{Platform: PlatformZaloBot}
	got := m.ToMap()
	if _, ok := got["message_id"]; ok {
		t.Error("empty MessageID should be omitted")
	}
	if _, ok := got["sender_display_name"]; ok {
		t.Error("empty SenderDisplayName should be omitted")
	}
	if got["platform"] != "zalo_bot" {
		t.Errorf("platform = %q, want zalo_bot", got["platform"])
	}
}
