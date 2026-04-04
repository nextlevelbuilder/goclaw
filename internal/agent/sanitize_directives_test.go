package agent

import "testing"

func TestStripMessageDirectives(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no tags", "Hello world", "Hello world"},
		{"single tag prefix", "[[reply_to:123]] Hello", "Hello"},
		{"single keyword tag", "[[voice]] Hello", "Hello"},
		{"multiple tags", "[[reply_to:1]] Hello [[silent]]", "Hello"},
		{"tag only", "[[reply_to:abc]]", ""},
		{"tag mid-sentence", "Say [[voice]] something", "Say  something"},

		// TTS tags must be preserved for TTS AutoTagged pipeline
		{"preserve tts bare", "[[tts]] Hello", "[[tts]] Hello"},
		{"preserve tts with param", "[[tts:en]] Hello", "[[tts:en]] Hello"},
		{"preserve tts:text block", "[[tts:text]] Hello [[/tts:text]]", "[[tts:text]] Hello [[/tts:text]]"},
		{"strip non-tts but keep tts", "[[reply_to:1]] [[tts]] Hello", "[[tts]] Hello"},

		// Should NOT match non-directive patterns
		{"no match without word chars", "text [[ ]] more", "text [[ ]] more"},
		{"no match multiline", "text [[\nfoo\n]] more", "text [[\nfoo\n]] more"},

		// Early exit: no [[ at all
		{"fast path no brackets", "just plain text", "just plain text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMessageDirectives(tt.in)
			if got != tt.want {
				t.Errorf("StripMessageDirectives(%q)\n got  %q\n want %q", tt.in, got, tt.want)
			}
		})
	}
}
