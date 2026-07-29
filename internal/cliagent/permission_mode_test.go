package cliagent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPermissionModeFromConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
		want PermissionMode
	}{
		{"absent", ``, PermissionAuto},
		{"null", `null`, PermissionAuto},
		{"empty object", `{}`, PermissionAuto},
		{"explicit auto", `{"permission_mode":"auto"}`, PermissionAuto},
		{"explicit manual", `{"permission_mode":"manual"}`, PermissionManual},
		{"manual cased/padded", `{"permission_mode":" Manual "}`, PermissionManual},
		{"ask alias", `{"permission_mode":"ask"}`, PermissionManual},
		{"unrecognised value", `{"permission_mode":"whatever"}`, PermissionAuto},
		{"wrong type", `{"permission_mode":true}`, PermissionAuto},
		{"broken json", `{"permission_mode":`, PermissionAuto},
		{"other keys only", `{"binary":"mycli"}`, PermissionAuto},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PermissionModeFromConfig(json.RawMessage(tc.cfg)); got != tc.want {
				t.Fatalf("PermissionModeFromConfig(%s) = %q, want %q", tc.cfg, got, tc.want)
			}
		})
	}
}

// The mode lives in the same config blob as the invocation overrides, so reading
// one must not disturb the other.
func TestPermissionModeCoexistsWithSpecOverrides(t *testing.T) {
	cfg := json.RawMessage(`{"permission_mode":"manual","extra_args":["--quiet"]}`)
	if got := PermissionModeFromConfig(cfg); got != PermissionManual {
		t.Fatalf("mode = %q, want manual", got)
	}
	spec := mustResolve(t, ProviderClaudeCode, string(cfg))
	if len(spec.ExtraArgs) != 1 || spec.ExtraArgs[0] != "--quiet" {
		t.Fatalf("ExtraArgs = %v, want [--quiet]", spec.ExtraArgs)
	}
	argv, err := spec.Command("do the work", PermissionManual)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if !strings.Contains(strings.Join(argv, " "), "--permission-mode default") {
		t.Fatalf("manual argv missing the ask-before-acting flag: %v", argv)
	}
}

// A provider with no manual flag must fail loudly, never fall back to auto.
func TestManualModeOnUnsupportedProviderErrors(t *testing.T) {
	spec := mustResolve(t, ProviderCodex, "")
	if spec.SupportsManualApproval() {
		t.Skip("codex defaults now declare manual_approve_args")
	}
	if _, err := spec.Command("do the work", PermissionManual); !errors.Is(err, ErrManualApprovalUnsupported) {
		t.Fatalf("Command(manual) error = %v, want ErrManualApprovalUnsupported", err)
	}
}
