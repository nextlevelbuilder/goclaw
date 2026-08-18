package teamworkclassify

import (
	"fmt"
	"strings"
)

// Formation describes a dynamic team shape routed from a task: which agent
// roles fill the team and how their work pipelines. It is a routing directive
// only — it describes the intended team, not the live membership.
type Formation struct {
	// Name is a stable formation key ("solo-followup", "debugger-panel",
	// "planner-coder-tester", "architect-review-team").
	Name string
	// Agents lists member role names in execution order.
	Agents []string
	// Pipeline lists stage names; Agents may be grouped by stage.
	Pipeline []string
	// Complexity is the routing hint that selected this formation.
	Complexity string
}

// FormationMode extends Mode without changing existing Mode values. The Mode
// field carries the base mode plus an optional formation suffix, e.g.
// "formation:debugger-panel", and Category groups formations by shape.
type FormationMode struct {
	Mode     string // existing mode + optional formation suffix e.g. "formation:debugger-panel"
	Category string // "solo" | "debugger" | "build" | "review"
}

// Stable formation keys in the Phase 5 catalog. Kept as exported constants so
// callers (gateway methods, tools) can reference formations without string
// literals.
const (
	FormationSoloFollowup       = "solo-followup"
	FormationDebuggerPanel      = "debugger-panel"
	FormationPlannerCoderTester = "planner-coder-tester"
	FormationArchitectReview    = "architect-review-team"
)

// Formation routing categories.
const (
	FormationCategorySolo     = "solo"
	FormationCategoryDebugger = "debugger"
	FormationCategoryBuild    = "build"
	FormationCategoryReview   = "review"
)

// formationPrefix is the suffix attached to a base Mode by ModeFormation.
const formationPrefix = "formation:"

// formationCatalog is the deterministic set of formations available for
// routing. Phase 5 keeps this closed and pure — no LLM or store access — so a
// task with matching complexity always resolves to the same shape.
var formationCatalog = map[string]Formation{
	FormationSoloFollowup: {
		Name:       FormationSoloFollowup,
		Agents:     []string{"main"},
		Pipeline:   []string{"respond"},
		Complexity: "low",
	},
	FormationDebuggerPanel: {
		Name:       FormationDebuggerPanel,
		Agents:     []string{"debug", "review"},
		Pipeline:   []string{"reproduce", "root-cause"},
		Complexity: "medium",
	},
	FormationPlannerCoderTester: {
		Name:       FormationPlannerCoderTester,
		Agents:     []string{"plan", "coder", "tester"},
		Pipeline:   []string{"plan", "implement", "verify"},
		Complexity: "build",
	},
	FormationArchitectReview: {
		Name:       FormationArchitectReview,
		Agents:     []string{"arch", "review"},
		Pipeline:   []string{"design", "review"},
		Complexity: "high",
	},
}

// ErrUnknownFormation reports an override that does not match any catalog entry.
var ErrUnknownFormation = fmt.Errorf("unknown formation")

// SelectFormation routes a task to a formation by complexity hints and an
// explicit override. It is a pure function — no stores, no LLM. An explicit
// override always wins when it names a known formation; an unknown override is
// an error. Complexity is a soft signal, so the mapping is deterministic and
// unknown complexity values fall back to the solo formation rather than
// failing the route.
func SelectFormation(task, complexity, override string) (Formation, error) {
	if strings.TrimSpace(override) != "" {
		f, ok := formationCatalog[strings.ToLower(strings.TrimSpace(override))]
		if !ok {
			return Formation{}, fmt.Errorf("%w %q", ErrUnknownFormation, override)
		}
		return f, nil
	}

	switch normalizeComplexity(complexity) {
	case "high":
		return formationCatalog[FormationArchitectReview], nil
	case "medium":
		return formationCatalog[FormationDebuggerPanel], nil
	case "build":
		return formationCatalog[FormationPlannerCoderTester], nil
	default:
		return formationCatalog[FormationSoloFollowup], nil
	}
}

// normalizeComplexity buckets free-form complexity strings into the three
// deterministic routing tiers plus the low/default tier. Empty input and
// unrecognized values both map to "low".
func normalizeComplexity(complexity string) string {
	switch strings.ToLower(strings.TrimSpace(complexity)) {
	case "high", "complex", "critical", "hard":
		return "high"
	case "medium", "moderate", "normal":
		return "medium"
	case "build", "feature", "full", "multi-role", "multi_role":
		return "build"
	default:
		return "low"
	}
}

// ModeFormation returns the extended mode string for a selected formation,
// e.g. "formation:debugger-panel". It is additive: the base Mode enum values
// are unchanged.
func ModeFormation(f Formation) string {
	return formationPrefix + f.Name
}

// FormationCategory returns the routing category for a formation name.
// Unknown names collapse to the solo category so callers never get an empty
// category.
func FormationCategory(name string) string {
	switch name {
	case FormationDebuggerPanel:
		return FormationCategoryDebugger
	case FormationPlannerCoderTester:
		return FormationCategoryBuild
	case FormationArchitectReview:
		return FormationCategoryReview
	default:
		return FormationCategorySolo
	}
}

// FormationModeFor builds a FormationMode from a base mode and a selected
// formation. The Mode string joins the base mode and the formation suffix so
// downstream consumers can see both the original routing mode and the team
// shape that was chosen.
func FormationModeFor(base Mode, f Formation) FormationMode {
	return FormationMode{
		Mode:     string(base) + ":" + formationPrefix + f.Name,
		Category: FormationCategory(f.Name),
	}
}
