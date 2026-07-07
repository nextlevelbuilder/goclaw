package bootstrap

import (
	"reflect"
	"testing"
)

func TestParseTriggerWords(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "plain key value",
			content: "Name: Pasha\nTrigger words: Паша, Слышь, Чмо\nEmoji: 🤖",
			want:    []string{"Паша", "Слышь", "Чмо"},
		},
		{
			name:    "markdown bullet form",
			content: "- **Name:** Pasha\n- **Trigger words:** Паша, Слышь\n",
			want:    []string{"Паша", "Слышь"},
		},
		{
			name:    "case-insensitive key and singular",
			content: "trigger word: Паша",
			want:    []string{"Паша"},
		},
		{
			name:    "drops blanks and trims",
			content: "Trigger words:  Паша ,, Чмо ,  ",
			want:    []string{"Паша", "Чмо"},
		},
		{
			name:    "missing key",
			content: "Name: Pasha\nEmoji: 🤖",
			want:    nil,
		},
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTriggerWords(tc.content)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseTriggerWords() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
