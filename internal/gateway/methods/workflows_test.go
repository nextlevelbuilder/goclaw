package methods

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// The graph column is opaque JSONB by design, so this is the only place a
// malformed or hostile graph can be turned away. Structural validity belongs to
// the compiler; shape and size belong here.
func TestValidateGraph(t *testing.T) {
	ok := []string{
		``,                                  // absent — the field is optional on update
		`{}`,                                // empty graph, a legitimate new workflow
		`{"nodes":[],"edges":[]}`,           //
		`{"nodes":[{"id":"a"}],"edges":[]}`, //
	}
	for _, g := range ok {
		if msg, bad := validateGraph(json.RawMessage(g)); bad {
			t.Errorf("validateGraph(%q) rejected a valid graph: %s", g, msg)
		}
	}

	bad := map[string]string{
		`[]`:         "an array is not a graph object",
		`"nodes"`:    "a bare string is not a graph object",
		`42`:         "a number is not a graph object",
		`null`:       "null is not a graph object",
		`{"nodes":`:  "truncated JSON",
		`{nodes:[]}`: "unquoted keys are not JSON",
	}
	for g, why := range bad {
		if _, isBad := validateGraph(json.RawMessage(g)); !isBad {
			t.Errorf("validateGraph(%q) accepted it — %s", g, why)
		}
	}

	// Size cap: an unbounded JSONB column is a cheap way to fill a tenant's disk.
	huge := `{"nodes":"` + strings.Repeat("x", maxGraphBytes) + `"}`
	if msg, isBad := validateGraph(json.RawMessage(huge)); !isBad {
		t.Error("an oversized graph was accepted")
	} else if !strings.Contains(msg, "too large") {
		t.Errorf("oversized graph gave an unhelpful message: %q", msg)
	}
}

// A workflow's compile state and timestamps must survive the trip to the wire,
// and an empty graph must serialise as an object rather than null — a client
// doing graph.nodes on null would crash on a freshly created workflow.
func TestToWorkflowInfo(t *testing.T) {
	id := uuid.New()
	desc := "weekly digest"
	cerr := "trigger node has no schedule"
	info := toWorkflowInfo(&store.Workflow{
		ID:           id,
		Name:         "Digest",
		Description:  &desc,
		Enabled:      true,
		CompileError: &cerr,
	})

	if info.ID != id.String() || info.Name != "Digest" || !info.Enabled {
		t.Errorf("core fields did not map: %+v", info)
	}
	if info.Description != desc || info.CompileError != cerr {
		t.Errorf("optional fields did not map: %+v", info)
	}
	if string(info.Graph) != `{}` {
		t.Errorf("empty graph serialised as %q, want {} — a client would crash reading .nodes off null", info.Graph)
	}

	// And the JSON actually produced must carry the graph as an object.
	blob, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round struct {
		Graph map[string]any `json:"graph"`
	}
	if err := json.Unmarshal(blob, &round); err != nil {
		t.Fatalf("graph did not round-trip as an object: %v (%s)", err, blob)
	}
}

// Arming is a separate method from update, and this is where that is enforced.
// The difference matters: it is "I saved a draft" versus "I started something
// that runs unattended".
func TestNewWorkflowFromParamsNeverArms(t *testing.T) {
	tenant := uuid.New()
	yes := true
	no := false
	for _, enabled := range []*bool{nil, &yes, &no} {
		p := workflowWriteParams{Enabled: enabled}
		w := newWorkflowFromParams(tenant, "user-1", "Digest", p)
		if w.Enabled {
			t.Errorf("enabled=%v in params produced an armed workflow", enabled)
		}
	}
}

func TestNewWorkflowFromParamsMapsFields(t *testing.T) {
	tenant := uuid.New()
	desc := "  weekly digest  "
	p := workflowWriteParams{
		Description: &desc,
		Graph:       json.RawMessage(`{"nodes":[]}`),
	}
	w := newWorkflowFromParams(tenant, "  user-1  ", "Digest", p)

	if w.TenantID != tenant || w.Name != "Digest" {
		t.Errorf("core fields: %+v", w)
	}
	if w.Description == nil || *w.Description != "weekly digest" {
		t.Errorf("description not trimmed: %v", w.Description)
	}
	if w.CreatedBy == nil || *w.CreatedBy != "user-1" {
		t.Errorf("createdBy not trimmed: %v", w.CreatedBy)
	}
	if string(w.Graph) != `{"nodes":[]}` {
		t.Errorf("graph not carried: %s", w.Graph)
	}

	// A whitespace-only description is no description, not an empty string that
	// would render as a blank subtitle in the UI.
	blank := "   "
	w = newWorkflowFromParams(tenant, "", "Digest", workflowWriteParams{Description: &blank})
	if w.Description != nil {
		t.Errorf("blank description stored as %q", *w.Description)
	}
	if w.CreatedBy != nil {
		t.Errorf("empty user stored as %q", *w.CreatedBy)
	}
}
