package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

func TestApplyToolResultTruncationPrefersToolWorkspace(t *testing.T) {
	toolWorkspace := t.TempDir()
	pipelineWorkspace := t.TempDir()

	ctx := tools.WithToolWorkspace(context.Background(), toolWorkspace)
	state := &pipeline.RunState{
		Workspace: &workspace.WorkspaceContext{ActivePath: pipelineWorkspace},
	}
	result := &tools.Result{ForLLM: strings.Repeat("x", 35000)}

	applyToolResultTruncation(ctx, state, result)

	wantPrefix := filepath.Join(toolWorkspace, "overflow")
	if !strings.Contains(result.ForLLM, wantPrefix) {
		t.Fatalf("expected truncated result to reference tool workspace overflow under %q, got %q", wantPrefix, result.ForLLM)
	}
}
