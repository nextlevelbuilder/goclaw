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
			content: "Name: Rex\nTrigger words: Alice, Boss, Chief\nEmoji: 🤖",
			want:    []string{"Alice", "Boss", "Chief"},
		},
		{
			name:    "markdown bullet form",
			content: "- **Name:** Rex\n- **Trigger words:** Alice, Boss\n",
			want:    []string{"Alice", "Boss"},
		},
		{
			name:    "case-insensitive key and singular",
			content: "trigger word: Alice",
			want:    []string{"Alice"},
		},
		{
			name:    "drops blanks and trims",
			content: "Trigger words:  Alice ,, Chief ,  ",
			want:    []string{"Alice", "Chief"},
		},
		{
			name:    "missing key",
			content: "Name: Rex\nEmoji: 🤖",
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
