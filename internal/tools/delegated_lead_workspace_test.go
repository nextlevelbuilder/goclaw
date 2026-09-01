package tools

import (
	"context"
	"strings"
	"testing"
)

func delegationArtifactRunCtx(ctx context.Context) context.Context {
	ctx = WithDelegationID(ctx, "11111111-1111-1111-1111-111111111111")
	return WithDelegationArtifactInputs(ctx, "/data/collaboration/delegations/11111111-1111-1111-1111-111111111111/inputs")
}

func containsPrefix(prefixes []string, want string) bool {
	for _, p := range prefixes {
		if p == want {
			return true
		}
	}
	return false
}

// With its team in context a delegated lead can reach the team's files: reads,
// listing and send_file all resolve paths through allowedWithTeamWorkspace.
func TestTeamPathsAreReadableInDelegationArtifactRun(t *testing.T) {
	teamRoot := "/data/teams/team-1"
	teamDir := teamRoot + "/chat-1"
	ctx := delegationArtifactRunCtx(WithToolTeamRoot(WithToolTeamWorkspace(context.Background(), teamDir), teamRoot))

	allowed := allowedWithTeamWorkspace(ctx, nil)
	if !containsPrefix(allowed, teamDir) {
		t.Errorf("team workspace %q missing from allowed read prefixes %v", teamDir, allowed)
	}
	// The root is the half that matters for a deliverable a member wrote under
	// its own chat scope — the lead's own leaf would not cover it.
	if !containsPrefix(allowed, teamRoot) {
		t.Errorf("team root %q missing from allowed read prefixes %v; a member's file written "+
			"under another chat scope stays unreachable to the lead", teamRoot, allowed)
	}
}

// The root widens reads only. Writes stay in the agent's own leaf, so a lead
// cannot write across a peer's chat scope through an absolute path.
func TestTeamRootDoesNotWidenWrites(t *testing.T) {
	teamRoot := "/data/teams/team-1"
	teamDir := teamRoot + "/chat-1"
	ctx := delegationArtifactRunCtx(WithToolTeamRoot(WithToolTeamWorkspace(context.Background(), teamDir), teamRoot))

	if allowed := allowedWriteWithTeamWorkspace(ctx, nil); containsPrefix(allowed, teamRoot) {
		t.Errorf("team root %q became writable: %v", teamRoot, allowed)
	}
}

// Without a team nothing is granted — an agent leading several teams, or none,
// must not gain a path it did not have before.
func TestNoTeamGrantsNothingExtra(t *testing.T) {
	base := []string{"/data/workspace/brain"}
	ctx := delegationArtifactRunCtx(context.Background())

	if got := allowedWithTeamWorkspace(ctx, base); len(got) != len(base) {
		t.Errorf("allowed prefixes grew from %v to %v without a team in context", base, got)
	}
}

// Reading the team's files must not turn into pushing them sideways: the
// delegation exchange stays hermetic and send_file keeps refusing inside an
// artifact run regardless of the new read access.
func TestSendFileStaysBlockedInDelegationArtifactRun(t *testing.T) {
	teamDir := t.TempDir()
	ctx := delegationArtifactRunCtx(WithToolTeamWorkspace(context.Background(), teamDir))

	tool := &SendFileTool{}
	res := tool.Execute(ctx, map[string]any{"path": teamDir + "/deliverable.html"})

	if !res.IsError {
		t.Fatal("send_file succeeded inside a delegation artifact run; files must be published " +
			"through the artifact exchange when the run completes")
	}
	if !strings.Contains(res.ForLLM, "published only after the delegated run completes") {
		t.Errorf("unexpected refusal reason: %s", res.ForLLM)
	}
}
