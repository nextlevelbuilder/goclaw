package security

import "testing"

func TestValidateLauncherArgsDeniesEnvCommandLaunch(t *testing.T) {
	if reason := validateLauncherArgs("env", []string{"bash", "-lc", "id"}); reason == "" {
		t.Fatal("expected env with command args to be denied")
	}
}

func TestValidateLauncherArgsAllowsPlainNonLauncherCommand(t *testing.T) {
	if reason := validateLauncherArgs("git", []string{"status"}); reason != "" {
		t.Fatalf("expected git args to be allowed, got %q", reason)
	}
}
