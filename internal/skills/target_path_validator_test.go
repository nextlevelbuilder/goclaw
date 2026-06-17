package skills

import "testing"

func TestValidateSkillTargetPath(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		allowSkillMD bool
		want         string
		wantErr      bool
	}{
		{name: "reference file", raw: `references\troubleshooting.md`, want: "references/troubleshooting.md"},
		{name: "skill markdown allowed", raw: "SKILL.md", allowSkillMD: true, want: "SKILL.md"},
		{name: "skill markdown blocked", raw: "SKILL.md", wantErr: true},
		{name: "absolute path", raw: "/tmp/SKILL.md", wantErr: true},
		{name: "parent traversal", raw: "../outside.md", wantErr: true},
		{name: "hidden file", raw: "references/.env", wantErr: true},
		{name: "system artifact", raw: ".git/config", wantErr: true},
		{name: "windows drive", raw: "C:/Users/secret.md", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateSkillTargetPath(tt.raw, tt.allowSkillMD)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
