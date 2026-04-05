package agent

import "testing"

func TestCleanRecallQuery_StripsChannelNoise(t *testing.T) {
	input := "[From: @tester (Tester)] alo em - đang có dự án nào. hôm nay có gì mới. check lại"
	got := cleanRecallQuery(input)
	want := "đang có dự án nào. hôm nay có gì mới"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDetectRecallIntent(t *testing.T) {
	// Test project overview intent
	intent := detectRecallIntent("đang có dự án nào. hôm nay có gì mới")
	if intent.Type != intentProjectOverview {
		t.Fatalf("expected project overview intent, got %v", intent.Type)
	}

	// Test that specific-project questions don't trigger overview
	intent = detectRecallIntent("dự án GearVN hôm nay bị block ở đâu")
	if intent.Type == intentProjectOverview {
		t.Fatal("did not expect a specific-project question to trigger overview inventory")
	}

	// Test recent activity intent
	intent = detectRecallIntent("hôm nay có gì mới")
	if intent.Type != intentRecentActivity {
		t.Fatalf("expected recent activity intent, got %v", intent.Type)
	}
	if intent.Temporal != "today" {
		t.Fatalf("expected temporal='today', got %q", intent.Temporal)
	}

	// Test entity lookup
	intent = detectRecallIntent("GoClaw project status")
	if intent.Type != intentEntityLookup {
		t.Fatalf("expected entity lookup intent, got %v", intent.Type)
	}

	// Test general
	intent = detectRecallIntent("how do I configure this")
	if intent.Type != intentGeneral {
		t.Fatalf("expected general intent, got %v", intent.Type)
	}
}

func TestExtractEntityNamesFromProjectInventoryJSON(t *testing.T) {
	payload := `{
	  "mode": "list_all",
	  "entity_type": "project",
	  "count": 2,
	  "has_results": true,
	  "has_project_match": true,
	  "project_names": ["GoClaw", "Warranty Optimization"]
	}`
	names := extractEntityNames(payload)
	if len(names) != 2 || names[0] != "GoClaw" || names[1] != "Warranty Optimization" {
		t.Fatalf("unexpected names: %+v", names)
	}
}
