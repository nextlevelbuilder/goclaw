package skills

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
)

// TestEditionGate_LitePreventsRegistration mirrors the wiring logic in
// cmd/gateway_packages_wiring.go and asserts that the pip/npm checkers are
// NOT registered when edition.Current().SupportsPipNpm == false (Lite desktop).
//
// This is the unit-level guard for P2A-H6: "Lite edition runs useless pip/npm
// checkers". The wiring file gates registration like:
//
//	if edition.Current().SupportsPipNpm {
//	    registry.RegisterChecker(NewPipUpdateChecker())
//	    registry.RegisterExecutor(NewPipUpdateExecutor())
//	    registry.RegisterChecker(NewNpmUpdateChecker())
//	    registry.RegisterExecutor(NewNpmUpdateExecutor())
//	}
func TestEditionGate_LitePreventsRegistration(t *testing.T) {
	// Temporarily set edition to Lite; restore Standard on exit.
	edition.SetCurrent(edition.Lite)
	t.Cleanup(func() { edition.SetCurrent(edition.Standard) })

	// Replicate wiring logic.
	registry := NewUpdateRegistry(nil, "", time.Hour)

	// Always register github (no edition gate in wiring).
	// Use a fakeChecker so we don't need a real GitHubInstaller.
	registry.RegisterChecker(&fakeChecker{source: "github", available: true})

	// Gate pip+npm behind edition flag — same condition as wiring.
	if edition.Current().SupportsPipNpm {
		registry.RegisterChecker(NewPipUpdateChecker())
		registry.RegisterExecutor(NewPipUpdateExecutor())
		registry.RegisterChecker(NewNpmUpdateChecker())
		registry.RegisterExecutor(NewNpmUpdateExecutor())
	}

	sources := registry.Sources()

	if len(sources) != 1 || sources[0] != "github" {
		t.Errorf("Lite edition: want sources=[github], got %v", sources)
	}

	// pip and npm must not appear.
	for _, s := range sources {
		if s == "pip" || s == "npm" {
			t.Errorf("Lite edition: unexpected source %q in registry", s)
		}
	}
}

// TestEditionGate_StandardAllowsRegistration verifies the positive case:
// Standard edition registers all three sources.
func TestEditionGate_StandardAllowsRegistration(t *testing.T) {
	edition.SetCurrent(edition.Standard)
	t.Cleanup(func() { edition.SetCurrent(edition.Standard) })

	registry := NewUpdateRegistry(nil, "", time.Hour)
	registry.RegisterChecker(&fakeChecker{source: "github", available: true})

	if edition.Current().SupportsPipNpm {
		registry.RegisterChecker(NewPipUpdateChecker())
		registry.RegisterExecutor(NewPipUpdateExecutor())
		registry.RegisterChecker(NewNpmUpdateChecker())
		registry.RegisterExecutor(NewNpmUpdateExecutor())
	}

	sources := registry.Sources() // sorted: github, npm, pip
	want := map[string]bool{"github": true, "pip": true, "npm": true}
	for _, s := range sources {
		delete(want, s)
	}
	if len(want) != 0 {
		t.Errorf("Standard edition: missing sources %v in %v", want, sources)
	}
}
