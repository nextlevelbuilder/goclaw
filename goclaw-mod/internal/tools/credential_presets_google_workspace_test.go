package tools

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestGoogleWorkspacePresetContract(t *testing.T) {
	preset := GetPreset("gws")
	if preset == nil {
		t.Fatal("gws preset is missing")
	}
	if preset.BinaryName != "gws" {
		t.Fatalf("BinaryName = %q, want gws", preset.BinaryName)
	}

	envNames := make([]string, 0, len(preset.EnvVars))
	for _, envVar := range preset.EnvVars {
		envNames = append(envNames, envVar.Name)
	}
	for _, want := range []string{
		"GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE",
		"GOOGLE_WORKSPACE_CLI_TOKEN",
		"GOOGLE_WORKSPACE_CLI_CLIENT_ID",
		"GOOGLE_WORKSPACE_CLI_CLIENT_SECRET",
	} {
		if !slices.Contains(envNames, want) {
			t.Fatalf("gws preset env vars = %v, missing %s", envNames, want)
		}
	}
	if slices.Contains(envNames, "GOOGLE_WORKSPACE_CLI_IMPERSONATED_USER") {
		t.Fatalf("gws preset must not expose removed upstream impersonation env var")
	}
}

func TestGoogleWorkspacePresetDenyPatterns(t *testing.T) {
	preset := GetPreset("gws")
	if preset == nil {
		t.Fatal("gws preset is missing")
	}
	denyArgs, err := json.Marshal(preset.DenyArgs)
	if err != nil {
		t.Fatal(err)
	}

	blocked := [][]string{
		{"auth", "setup"},
		{"auth", "login"},
		{"auth", "export", "--unmasked"},
		{"auth", "logout"},
	}
	for _, args := range blocked {
		if got := matchesBinaryDeny(args, denyArgs); got == "" {
			t.Fatalf("matchesBinaryDeny(%v) did not block auth credential command", args)
		}
	}

	allowed := [][]string{
		{"drive", "files", "list", "--params", `{"pageSize":1}`},
		{"gmail", "users", "messages", "list", "--params", `{"userId":"me","maxResults":1}`},
		{"calendar", "events", "list", "--params", `{"calendarId":"primary","maxResults":1}`},
		{"schema", "drive.files.list"},
	}
	for _, args := range allowed {
		if got := matchesBinaryDeny(args, denyArgs); got != "" {
			t.Fatalf("matchesBinaryDeny(%v) = %q, want allowed", args, got)
		}
	}
}
