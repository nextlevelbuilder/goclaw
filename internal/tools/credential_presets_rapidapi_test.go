package tools

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestRapidAPIPresetContract(t *testing.T) {
	preset := GetPreset("rapidapi")
	if preset == nil {
		t.Fatal("rapidapi preset is missing")
	}
	if preset.BinaryName != "rapidapi" {
		t.Fatalf("BinaryName = %q, want rapidapi", preset.BinaryName)
	}

	envNames := make([]string, 0, len(preset.EnvVars))
	for _, envVar := range preset.EnvVars {
		envNames = append(envNames, envVar.Name)
	}
	if !slices.Contains(envNames, "RAPIDAPI_KEY") {
		t.Fatalf("rapidapi preset env vars = %v, missing RAPIDAPI_KEY", envNames)
	}
}

func TestRapidAPIPresetBlocksVerboseSecretLeakFlags(t *testing.T) {
	preset := GetPreset("rapidapi")
	if preset == nil {
		t.Fatal("rapidapi preset is missing")
	}
	denyVerbose, err := json.Marshal(preset.DenyVerbose)
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"--verbose"},
		{"--debug"},
		{"-v"},
	} {
		if got := matchesBinaryVerbose(args, denyVerbose); got == "" {
			t.Fatalf("matchesBinaryVerbose(%v) did not block verbose/debug flag", args)
		}
	}
}

func TestRequiredCredentialEnvVarsFromPresets(t *testing.T) {
	if got := requiredCredentialEnvVars("rapidapi"); !slices.Contains(got, "RAPIDAPI_KEY") {
		t.Fatalf("requiredCredentialEnvVars(rapidapi) = %v, missing RAPIDAPI_KEY", got)
	}
	if got := requiredCredentialEnvVars("gh"); !slices.Contains(got, "GH_TOKEN") {
		t.Fatalf("requiredCredentialEnvVars(gh) = %v, missing GH_TOKEN", got)
	}
}
