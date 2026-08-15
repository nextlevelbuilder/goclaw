package minimax

import "testing"

// TestParseMinimaxLabels covers the heuristic that derives gender + language
// hints from MiniMax voice IDs/names. MiniMax API does not return these fields
// explicitly, so we depend on the documented naming convention.
func TestParseMinimaxLabels(t *testing.T) {
	tests := []struct {
		name      string
		voiceID   string
		voiceName string
		want      map[string]string
	}{
		{"male prefix", "male-qn-qingse", "male-qn-qingse", map[string]string{"gender": "male"}},
		{"female prefix", "female-tianmei", "female-tianmei", map[string]string{"gender": "female"}},
		{"english man suffix", "abc", "English_Persuasive_Man", map[string]string{"gender": "male", "language": "English"}},
		{"english lady suffix", "xyz", "English_Graceful_Lady", map[string]string{"gender": "female", "language": "English"}},
		{"english girl suffix", "xyz", "English_radiant_girl", map[string]string{"gender": "female", "language": "English"}},
		{"japanese belle suffix", "abc", "Japanese_Whisper_Belle", map[string]string{"gender": "female", "language": "Japanese"}},
		{"unknown name no prefix", "moss_audio_xyz", "moss_audio_xyz", nil},
		{"empty inputs", "", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMinimaxLabels(tc.voiceID, tc.voiceName)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
