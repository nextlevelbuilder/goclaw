package agent

import (
	"strings"
	"testing"
)

// TestSelfEvolveSectionMentionsCapabilities verifies that the self-evolution
// system prompt section includes CAPABILITIES.md as an evolvable file.
func TestSelfEvolveSectionMentionsCapabilities(t *testing.T) {
	lines := buildSelfEvolveSection()
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "CAPABILITIES.md") {
		t.Error("buildSelfEvolveSection() should mention CAPABILITIES.md as evolvable")
	}
	if !strings.Contains(joined, "SOUL.md") {
		t.Error("buildSelfEvolveSection() should still mention SOUL.md")
	}
	// Protected files must NOT be mentioned as writable
	if strings.Contains(joined, "IDENTITY.md") && !strings.Contains(joined, "MUST NOT") {
		t.Error("IDENTITY.md should only appear in the MUST NOT change list")
	}
}
