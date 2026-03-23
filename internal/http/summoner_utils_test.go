package http

import "testing"

func TestExtractIdentityName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "extracts inline markdown name",
			content: `# IDENTITY.md - Who Am I?

- **Name:** GoClaw
- **Creature:** AI assistant`,
			want: "GoClaw",
		},
		{
			name: "ignores next bullet when name is blank",
			content: `# IDENTITY.md - Who Am I?

- **Name:**
- **Creature:** AI assistant`,
			want: "",
		},
		{
			name: "rejects inline spillover from another field",
			content: `# IDENTITY.md - Who Am I?

- **Name:** - **Creature:** AI assistant
- **Purpose:** Help users`,
			want: "",
		},
		{
			name: "strips simple markdown wrappers from name",
			content: `# IDENTITY.md - Who Am I?

- **Name:** **GoClaw**
- **Creature:** AI assistant`,
			want: "GoClaw",
		},
		{
			name: "supports plain name format",
			content: `# Identity
Name: GoClaw
Emoji: 🤖`,
			want: "GoClaw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractIdentityName(tt.content); got != tt.want {
				t.Fatalf("extractIdentityName() = %q, want %q", got, tt.want)
			}
		})
	}
}
