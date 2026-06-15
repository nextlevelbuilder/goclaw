package bitrix24

import "testing"

func TestExtractOpenlineSenderPrefix(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantPrefix string
		wantRest   string
	}{
		{
			name:       "id inside brackets",
			in:         "[Thân Công Huy #1623524631958449211]: alo",
			wantPrefix: "[Thân Công Huy] #1623524631958449211:",
			wantRest:   "alo",
		},
		{
			name:       "id after brackets",
			in:         "[Thân Công Huy] #1623524631958449211: alo",
			wantPrefix: "[Thân Công Huy] #1623524631958449211:",
			wantRest:   "alo",
		},
		{
			name:       "extra spaces after colon",
			in:         "[A B] #42:    hello world",
			wantPrefix: "[A B] #42:",
			wantRest:   "hello world",
		},
		{
			name:       "no trailing body",
			in:         "[Khách Lạ #999]:",
			wantPrefix: "[Khách Lạ] #999:",
			wantRest:   "",
		},
		{
			name:       "plain message — no tag",
			in:         "chào shop, cho hỏi giá",
			wantPrefix: "",
			wantRest:   "chào shop, cho hỏi giá",
		},
		{
			name:       "readable mention — not an openline tag",
			in:         "@Ngọc Thúy (ID:62) giúp em",
			wantPrefix: "",
			wantRest:   "@Ngọc Thúy (ID:62) giúp em",
		},
		{
			name:       "bracket without numeric id — no match",
			in:         "[just a note]: text",
			wantPrefix: "",
			wantRest:   "[just a note]: text",
		},
		{
			name:       "non-numeric id — no match",
			in:         "[Name #abc]: text",
			wantPrefix: "",
			wantRest:   "[Name #abc]: text",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPrefix, gotRest := extractOpenlineSenderPrefix(tc.in)
			if gotPrefix != tc.wantPrefix {
				t.Errorf("prefix = %q, want %q", gotPrefix, tc.wantPrefix)
			}
			if gotRest != tc.wantRest {
				t.Errorf("rest = %q, want %q", gotRest, tc.wantRest)
			}
		})
	}
}
