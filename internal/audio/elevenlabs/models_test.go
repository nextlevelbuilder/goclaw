package elevenlabs

import (
	"strings"
	"testing"
)

func TestValidateModel_AllowsKnownIDs(t *testing.T) {
	t.Parallel()
	allowed := []string{
		"eleven_v3",
		"eleven_flash_v2_5",
		"eleven_multilingual_v2",
		"eleven_turbo_v2_5",
	}
	for _, id := range allowed {
		if err := ValidateModel(id); err != nil {
			t.Errorf("ValidateModel(%q) returned error: %v (want nil)", id, err)
		}
	}
}

func TestValidateModel_RejectsUnknown(t *testing.T) {
	t.Parallel()
	err := ValidateModel("foo")
	if err == nil {
		t.Fatal("ValidateModel(\"foo\") returned nil (want i18n-keyed error)")
	}
	// Error should mention the offending model so operators can spot typos.
	if !strings.Contains(err.Error(), "foo") {
		t.Errorf("error message should contain model id: %q", err.Error())
	}
}

func TestValidateModel_EmptyIsAllowed(t *testing.T) {
	t.Parallel()
	// Empty string means "use configured default" — never a client error.
	if err := ValidateModel(""); err != nil {
		t.Errorf("ValidateModel(\"\") returned error: %v (want nil — empty = use default)", err)
	}
}

func TestAllowedElevenLabsModels_ContainsExpectedSet(t *testing.T) {
	t.Parallel()
	want := []string{
		"eleven_v3",
		"eleven_flash_v2_5",
		"eleven_multilingual_v2",
		"eleven_turbo_v2_5",
	}
	for _, id := range want {
		if _, ok := AllowedElevenLabsModels[id]; !ok {
			t.Errorf("AllowedElevenLabsModels missing %q", id)
		}
	}
}
