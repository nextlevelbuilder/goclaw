package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestRegisterParams_TYPE verifies registerParams forwards cfg.BotType
// verbatim to Bitrix24's imbot.register TYPE field. Hardcoding "B" here
// was the old behavior — now both "B" and "O" must flow through so
// Open Channel bots can be registered.
func TestRegisterParams_TYPE(t *testing.T) {
	cases := []struct {
		name  string
		bType string
	}{
		{"standard_B", "B"},
		{"open_channel_O", "O"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeStore()
			resetWebhookRouterForTest()
			defer resetWebhookRouterForTest()

			fn := FactoryWithPortalStore(fs, "")
			cfg := json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n","public_url":"https://example.test","bot_type":"` + tc.bType + `"}`)
			ch, err := fn("b1", nil, cfg, nil, nil)
			if err != nil {
				t.Fatalf("factory: %v", err)
			}
			bc := ch.(*Channel)
			params := bc.registerParams(context.Background())
			got, ok := params["TYPE"].(string)
			if !ok {
				t.Fatalf("TYPE missing or wrong type in params: %+v", params["TYPE"])
			}
			if got != tc.bType {
				t.Errorf("TYPE = %q; want %q (hardcode regression?)", got, tc.bType)
			}
		})
	}
}

func TestIntFromResult_PlainInt(t *testing.T) {
	r := &RawResult{Result: json.RawMessage(`42`)}
	if got := intFromResult(r); got != 42 {
		t.Errorf("plain int: got %d; want 42", got)
	}
}

func TestIntFromResult_ObjectBOTID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"BOT_ID", `{"BOT_ID": 123}`, 123},
		{"bot_id lowercase", `{"bot_id": 9}`, 9},
		{"ID", `{"ID": 55}`, 55},
		{"id lowercase", `{"id": 77}`, 77},
		{"string numeric", `{"BOT_ID": "321"}`, 321}, // json.Number also parses quoted numerics
		{"negative", `{"BOT_ID": -1}`, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &RawResult{Result: json.RawMessage(tc.body)}
			if got := intFromResult(r); got != tc.want {
				t.Errorf("%s: got %d; want %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestIntFromResult_NilOrEmpty(t *testing.T) {
	if got := intFromResult(nil); got != 0 {
		t.Errorf("nil result: got %d; want 0", got)
	}
	if got := intFromResult(&RawResult{}); got != 0 {
		t.Errorf("empty result: got %d; want 0", got)
	}
}

func TestResponseContainsBotID_ArrayForm(t *testing.T) {
	r := &RawResult{Result: json.RawMessage(`[
		{"BOT_ID": 11, "CODE": "a"},
		{"BOT_ID": 22, "CODE": "b"},
		{"BOT_ID": 33, "CODE": "c"}
	]`)}
	if !responseContainsBotID(r, 22) {
		t.Error("expected bot 22 found in array form")
	}
	if responseContainsBotID(r, 44) {
		t.Error("bot 44 should NOT be in the list")
	}
}

func TestResponseContainsBotID_MapForm(t *testing.T) {
	r := &RawResult{Result: json.RawMessage(`{
		"11": {"CODE": "a"},
		"22": {"CODE": "b"}
	}`)}
	if !responseContainsBotID(r, 11) {
		t.Error("bot 11 should be found by numeric map key")
	}
	if responseContainsBotID(r, 99) {
		t.Error("bot 99 should not be found")
	}
}

func TestResponseContainsBotID_MapWithInnerBotID(t *testing.T) {
	// Some older portals key by bot_code, not by id — inner BOT_ID carries the id.
	r := &RawResult{Result: json.RawMessage(`{
		"support_bot": {"BOT_ID": 88, "CODE": "support_bot"}
	}`)}
	if !responseContainsBotID(r, 88) {
		t.Error("bot 88 should be found by inner BOT_ID")
	}
}

func TestResponseContainsBotID_InvalidInputs(t *testing.T) {
	if responseContainsBotID(nil, 1) {
		t.Error("nil result should return false")
	}
	if responseContainsBotID(&RawResult{Result: json.RawMessage(`[]`)}, 1) {
		t.Error("empty array should return false")
	}
	if responseContainsBotID(&RawResult{Result: json.RawMessage(`[{"BOT_ID":1}]`)}, 0) {
		t.Error("botID <= 0 should return false")
	}
}

func TestFindBotIDByCodeInResponse_ArrayForm(t *testing.T) {
	r := &RawResult{Result: json.RawMessage(`[
		{"BOT_ID": 11, "CODE": "support_bot"},
		{"BOT_ID": 22, "CODE": "faq_bot"}
	]`)}
	if got := findBotIDByCodeInResponse(r, "faq_bot"); got != 22 {
		t.Errorf("faq_bot: got %d; want 22", got)
	}
	if got := findBotIDByCodeInResponse(r, "missing"); got != 0 {
		t.Errorf("missing code: got %d; want 0", got)
	}
}

func TestFindBotIDByCodeInResponse_MapFormKeyFallback(t *testing.T) {
	// Map keyed by bot_id (string numeric). CODE matches but no inner BOT_ID
	// field — falls back to parsing the numeric object key.
	r := &RawResult{Result: json.RawMessage(`{
		"99": {"CODE": "legacy_bot"}
	}`)}
	if got := findBotIDByCodeInResponse(r, "legacy_bot"); got != 99 {
		t.Errorf("legacy_bot: got %d; want 99 (from numeric map key)", got)
	}
}

func TestFindBotIDByCodeInResponse_EmptyInputs(t *testing.T) {
	if got := findBotIDByCodeInResponse(nil, "x"); got != 0 {
		t.Errorf("nil result: got %d; want 0", got)
	}
	if got := findBotIDByCodeInResponse(&RawResult{Result: json.RawMessage(`[]`)}, ""); got != 0 {
		t.Errorf("empty code: got %d; want 0", got)
	}
}

func TestExtractBotID_QuotedStringID(t *testing.T) {
	row := map[string]json.RawMessage{
		"BOT_ID": json.RawMessage(`"123"`),
	}
	if got := extractBotID(row); got != 123 {
		t.Errorf("quoted id: got %d; want 123", got)
	}
}

func TestExtractBotID_NoMatch(t *testing.T) {
	row := map[string]json.RawMessage{
		"OTHER_FIELD": json.RawMessage(`1`),
	}
	if got := extractBotID(row); got != 0 {
		t.Errorf("no match: got %d; want 0", got)
	}
}

func TestAtoiSafe(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"  ", 0},
		{"42", 42},
		{"  42  ", 42},
		{"-5", 0},        // negative rejected
		{"12a", 0},       // non-digit rejected
		{"9999999", 9999999},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := atoiSafe(tc.in); got != tc.want {
				t.Errorf("atoiSafe(%q) = %d; want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsDuplicateCodeError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated APIError", &APIError{Code: "QUERY_LIMIT_EXCEEDED"}, false},
		{"ERROR_ARGUMENT + code exists",
			&APIError{Code: "ERROR_ARGUMENT", Description: "Bot code already exists"}, true},
		{"ERROR_REGISTER_BOT + duplicate",
			&APIError{Code: "ERROR_REGISTER_BOT", Description: "duplicate bot code"}, true},
		{"plain APIError already-exist",
			&APIError{Code: "", Description: "already exists on portal"}, true},
		{"plain error string",
			errors.New("duplicate code rejected"), true},
		{"unrelated plain error",
			errors.New("database timeout"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDuplicateCodeError(tc.err); got != tc.want {
				t.Errorf("isDuplicateCodeError(%v) = %v; want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestContainsFold(t *testing.T) {
	if !containsFold("DUPLICATE CODE", "duplicate") {
		t.Error("case-insensitive match failed")
	}
	if containsFold("xyz", "abc") {
		t.Error("should not match unrelated string")
	}
}

func TestRowCodeMatches(t *testing.T) {
	row := map[string]json.RawMessage{
		"CODE":   json.RawMessage(`"support_bot"`),
		"BOT_ID": json.RawMessage(`42`),
	}
	if !rowCodeMatches(row, "support_bot") {
		t.Error("CODE match should succeed")
	}
	if rowCodeMatches(row, "other_bot") {
		t.Error("wrong code should not match")
	}

	rowLower := map[string]json.RawMessage{
		"code": json.RawMessage(`"x"`),
	}
	if !rowCodeMatches(rowLower, "x") {
		t.Error("lowercase 'code' field should also match")
	}
}
