package bitrix24

import "testing"

func TestExtractOpenlineSenderPrefix(t *testing.T) {
	cases := []struct {
		name          string
		in            string
		allowNameOnly bool
		wantPrefix    string
		wantRest      string
	}{
		{
			name:       "id after brackets, no colon (current connector format)",
			in:         "[Thân Công Huy] #7941945550666 @Ngọc Thúy kiểm tra lịch",
			wantPrefix: "[Thân Công Huy] #7941945550666",
			wantRest:   "@Ngọc Thúy kiểm tra lịch",
		},
		{
			name:       "id after brackets, with colon (legacy)",
			in:         "[Thân Công Huy] #1623524631958449211: alo",
			wantPrefix: "[Thân Công Huy] #1623524631958449211",
			wantRest:   "alo",
		},
		{
			name:       "id inside brackets, with colon (legacy)",
			in:         "[Thân Công Huy #1623524631958449211]: alo",
			wantPrefix: "[Thân Công Huy] #1623524631958449211",
			wantRest:   "alo",
		},
		{
			name:       "id inside brackets, no colon",
			in:         "[Thân Công Huy #1623524631958449211] alo",
			wantPrefix: "[Thân Công Huy] #1623524631958449211",
			wantRest:   "alo",
		},
		{
			name:       "id no colon, no name-only flag — still matches",
			in:         "[Minh Zip] #42 chào",
			wantPrefix: "[Minh Zip] #42",
			wantRest:   "chào",
		},
		{
			name:          "name only — openline (allowNameOnly)",
			in:            "[Minh Zip] móa user hỏi hóc xương cá thế nhỉ",
			allowNameOnly: true,
			wantPrefix:    "[Minh Zip]",
			wantRest:      "móa user hỏi hóc xương cá thế nhỉ",
		},
		{
			// id format wins over name-only, so the id is never dropped.
			name:          "id present — not clipped to name-only",
			in:            "[Thân Công Huy] #7941945550666 hello",
			allowNameOnly: true,
			wantPrefix:    "[Thân Công Huy] #7941945550666",
			wantRest:      "hello",
		},
		{
			// NOT an Open Channel → bare name-only ignored so ordinary group
			// chat text starting with "[x] …" is left untouched.
			name:          "name only — ignored when not openline",
			in:            "[Minh Zip] móa user hỏi",
			allowNameOnly: false,
			wantPrefix:    "",
			wantRest:      "[Minh Zip] móa user hỏi",
		},
		{
			name:          "plain message — no tag",
			in:            "chào shop, cho hỏi giá",
			allowNameOnly: true,
			wantPrefix:    "",
			wantRest:      "chào shop, cho hỏi giá",
		},
		{
			name:          "readable mention — not an openline tag",
			in:            "@Ngọc Thúy (ID:62) giúp em",
			allowNameOnly: true,
			wantPrefix:    "",
			wantRest:      "@Ngọc Thúy (ID:62) giúp em",
		},
		// Characterization: current behavior locked before the 3-token refactor.
		// The single number after the brackets is a per-message connector id; it
		// must keep flowing through extractOpenlineSenderPrefix unchanged so the
		// legacy echo path is untouched.
		{
			name:       "legacy 1-number after brackets (msgId) — characterization",
			in:         "[Trung Hee] #7957717404177 alo bot",
			wantPrefix: "[Trung Hee] #7957717404177",
			wantRest:   "alo bot",
		},
		{
			name:       "legacy 1-number inside brackets — characterization",
			in:         "[Trung Hee #7957717404177]: alo",
			wantPrefix: "[Trung Hee] #7957717404177",
			wantRest:   "alo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPrefix, gotRest := extractOpenlineSenderPrefix(tc.in, tc.allowNameOnly)
			if gotPrefix != tc.wantPrefix {
				t.Errorf("prefix = %q, want %q", gotPrefix, tc.wantPrefix)
			}
			if gotRest != tc.wantRest {
				t.Errorf("rest = %q, want %q", gotRest, tc.wantRest)
			}
		})
	}
}

// TestParseOpenlineSenderTag pins the structured 3-token parser. It distinguishes
// the new "[Name] #uid #msgId" layout (two numbers) from the legacy single-number
// msgId layout, the bare name-only layout, and no tag at all. The parser only
// classifies shape — the IS_CONNECTOR forged-tag gate lives in handle.go.
func TestParseOpenlineSenderTag(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantName   string
		wantUID    string
		wantMsgID  string
		wantFormat OpenlineSenderTagFormat
		wantRest   string
	}{
		{
			name:       "3-token standard",
			in:         "[Trung Hee] #111222 #777888 alo bot",
			wantName:   "Trung Hee",
			wantUID:    "111222",
			wantMsgID:  "777888",
			wantFormat: TagFormatThreeToken,
			wantRest:   "alo bot",
		},
		{
			name:       "3-token with trailing colon",
			in:         "[Thân Công Huy] #9999 #8888: kiểm tra",
			wantName:   "Thân Công Huy",
			wantUID:    "9999",
			wantMsgID:  "8888",
			wantFormat: TagFormatThreeToken,
			wantRest:   "kiểm tra",
		},
		{
			// One number → legacy: uid empty, msgId = the only number.
			name:       "legacy 1-number → TagFormatLegacy",
			in:         "[Trung Hee] #7957717404177 alo",
			wantName:   "Trung Hee",
			wantUID:    "",
			wantMsgID:  "7957717404177",
			wantFormat: TagFormatLegacy,
			wantRest:   "alo",
		},
		{
			name:       "legacy 1-number inside brackets → TagFormatLegacy",
			in:         "[Trung Hee #7957717404177]: alo",
			wantName:   "Trung Hee",
			wantUID:    "",
			wantMsgID:  "7957717404177",
			wantFormat: TagFormatLegacy,
			wantRest:   "alo",
		},
		{
			name:       "name only → TagFormatNameOnly",
			in:         "[Minh Zip] hỏi giá",
			wantName:   "Minh Zip",
			wantFormat: TagFormatNameOnly,
			wantRest:   "hỏi giá",
		},
		{
			name:       "no tag → TagFormatNone",
			in:         "chào shop",
			wantFormat: TagFormatNone,
			wantRest:   "chào shop",
		},
		{
			// The shape parses regardless of source — handle.go's IS_CONNECTOR
			// gate is what stops an operator forged tag from minting identity.
			name:       "forged-looking 2-number shape still parses (gate is in handle.go)",
			in:         "[Victim] #111222 #777888 do nó bị văng",
			wantName:   "Victim",
			wantUID:    "111222",
			wantMsgID:  "777888",
			wantFormat: TagFormatThreeToken,
			wantRest:   "do nó bị văng",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOpenlineSenderTag(tc.in)
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if got.UID != tc.wantUID {
				t.Errorf("UID = %q, want %q", got.UID, tc.wantUID)
			}
			if got.MsgID != tc.wantMsgID {
				t.Errorf("MsgID = %q, want %q", got.MsgID, tc.wantMsgID)
			}
			if got.Format != tc.wantFormat {
				t.Errorf("Format = %d, want %d", got.Format, tc.wantFormat)
			}
			if got.Rest != tc.wantRest {
				t.Errorf("Rest = %q, want %q", got.Rest, tc.wantRest)
			}
		})
	}
}
