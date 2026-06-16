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
