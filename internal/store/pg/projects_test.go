package pg

import "testing"

func TestSecretKeyPattern(t *testing.T) {
	tests := []struct {
		key      string
		isSecret bool
	}{
		{"GITLAB_TOKEN", true},
		{"gitlab_token", true},
		{"GitLab_Token", true},
		{"API_KEY", true},
		{"api_key", true},
		{"MY_SECRET", true},
		{"DB_PASSWORD", true},
		{"password", true},
		{"AUTH_TOKEN_V2", true},
		{"X_API_KEY_EXTRA", true},
		{"SECRET_STUFF", true},

		{"GITLAB_PROJECT_ID", false},
		{"GITLAB_PROJECT_PATH", false},
		{"JIRA_PROJECT_KEY", false},
		{"CONFLUENCE_SPACE_KEY", false},
		{"PROJECT_PATH", false},
		{"BOARD_ID", false},
		{"WORKSPACE_DIR", false},
		{"TOKENIZER_TYPE", false}, // contains "TOKEN" substring but not at word boundary
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := secretKeyPattern.MatchString(tt.key)
			if got != tt.isSecret {
				t.Errorf("secretKeyPattern.MatchString(%q) = %v, want %v", tt.key, got, tt.isSecret)
			}
		})
	}
}
