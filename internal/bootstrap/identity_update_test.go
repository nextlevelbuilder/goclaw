package bootstrap

import "testing"

func TestUpdateIdentityField(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		fieldName string
		newValue  string
		want      string
	}{
		{
			name:      "plain format",
			content:   "# Identity\nName: OldName\nEmoji: 🤖",
			fieldName: "Name",
			newValue:  "NewName",
			want:      "# Identity\nName: NewName\nEmoji: 🤖",
		},
		{
			name:      "LLM markdown format",
			content:   "# Identity\n- **Name:** OldName\n- **Creature:** Fox",
			fieldName: "Name",
			newValue:  "NewName",
			want:      "# Identity\n- **Name:** NewName\n- **Creature:** Fox",
		},
		{
			name:      "field not found inserts after heading",
			content:   "# Identity\nEmoji: 🤖",
			fieldName: "Name",
			newValue:  "NewName",
			want:      "# Identity\nName: NewName\nEmoji: 🤖",
		},
		{
			name:      "empty content inserts at top",
			content:   "",
			fieldName: "Name",
			newValue:  "NewName",
			want:      "Name: NewName\n",
		},
		{
			name:      "empty newValue returns unchanged",
			content:   "# Identity\nName: OldName",
			fieldName: "Name",
			newValue:  "",
			want:      "# Identity\nName: OldName",
		},
		{
			name:      "value with colon (URL) preserved on other fields",
			content:   "# Identity\nName: Bot\nAvatar: https://example.com/avatar.png",
			fieldName: "Name",
			newValue:  "NewBot",
			want:      "# Identity\nName: NewBot\nAvatar: https://example.com/avatar.png",
		},
		{
			name:      "update avatar URL field correctly",
			content:   "# Identity\nName: Bot\nAvatar: https://old.com/img.png",
			fieldName: "Avatar",
			newValue:  "https://new.com/img.png",
			want:      "# Identity\nName: Bot\nAvatar: https://new.com/img.png",
		},
		{
			name:      "LLM markdown avatar with URL",
			content:   "# Identity\n- **Name:** Bot\n- **Avatar:** https://old.com/img.png",
			fieldName: "Avatar",
			newValue:  "https://new.com/img.png",
			want:      "# Identity\n- **Name:** Bot\n- **Avatar:** https://new.com/img.png",
		},
		{
			name:      "preserves other LLM fields",
			content:   "# Identity\n- **Name:** Bot\n- **Creature:** Fox\n- **Purpose:** Help users\n- **Vibe:** Friendly",
			fieldName: "Name",
			newValue:  "NewBot",
			want:      "# Identity\n- **Name:** NewBot\n- **Creature:** Fox\n- **Purpose:** Help users\n- **Vibe:** Friendly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpdateIdentityField(tt.content, tt.fieldName, tt.newValue)
			if got != tt.want {
				t.Errorf("UpdateIdentityField() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
