package http

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A personal agent belongs to its owner. Nobody else sees it — not an org owner,
// not an admin, not a platform operator listed in GOCLAW_OWNER_IDS.
//
// This is a SOURCE guard, and that is deliberate. The hole it prevents was not a
// wrong comparison but an entire alternative code path: `agentStore.List(ctx, "")`
// returns every agent in the tenant, and both list handlers used to reach for it
// when the caller matched a configured id. A behavioural test proves the path we
// took; only reading the source proves the other path is gone.
//
// It also could not have been caught by running the app: as configured
// (GOCLAW_OWNER_IDS=system) the bypass matched no real user, so it was invisible
// until someone added a person to that variable.
var unscopedList = regexp.MustCompile(`\.List\(\s*[a-zA-Z_.()]*,\s*""\s*\)`)

const unscopedOK = "unscoped-list-ok"

func TestAgentListingIsAlwaysScopedToTheCaller(t *testing.T) {
	for _, path := range []string{
		"agents.go",
		"../gateway/methods/agents.go",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(b)
		// A justified unscoped list must say so ON ITS OWN LINE or the one above.
		// An annotation at the call site is reviewable in a diff; a central ignore
		// list is where the next regression would hide.
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			if !unscopedList.MatchString(line) {
				continue
			}
			// Look back over the CONTIGUOUS comment block above the call, not just one
			// line: a justification worth reading is usually a sentence or three, and
			// requiring the marker on the final line would push the reason above the
			// marker or force a one-liner.
			exempt := strings.Contains(line, unscopedOK)
			for j := i - 1; j >= 0 && !exempt; j-- {
				trimmed := strings.TrimSpace(lines[j])
				if !strings.HasPrefix(trimmed, "//") {
					break
				}
				exempt = strings.Contains(trimmed, unscopedOK)
			}
			if exempt {
				continue
			}
			t.Errorf("%s:%d calls an unscoped agent list — that returns every member's private agents. "+
				"Use ListAccessible(ctx, userID), or annotate the line with %q and say why.",
				path, i+1, unscopedOK)
		}
		// The listing handler must reach ListAccessible, or the check above passes
		// vacuously on a file that stopped listing agents at all.
		if !strings.Contains(src, "ListAccessible(") {
			t.Errorf("%s no longer calls ListAccessible — this guard would pass for the wrong reason", path)
		}
	}
}
