package orchestration

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	plpkg "github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestCaptureFromRunResult_Nil(t *testing.T) {
	r := CaptureFromRunResult(nil, 5*time.Second)
	if r.Status != "failed" {
		t.Errorf("status = %q, want \"failed\"", r.Status)
	}
	if r.Runtime != 5*time.Second {
		t.Errorf("runtime = %v, want 5s", r.Runtime)
	}
}

func TestCaptureFromRunResult_WithUsage(t *testing.T) {
	rr := &agent.RunResult{
		Content:    "hello",
		Iterations: 3,
		Usage:      &providers.Usage{PromptTokens: 100, CompletionTokens: 50},
		Media: []agent.MediaResult{
			{Path: "/tmp/img.png", ContentType: "image/png"},
		},
	}
	c := CaptureFromRunResult(rr, 2*time.Second)
	if c.Content != "hello" {
		t.Errorf("content = %q", c.Content)
	}
	if c.InputTokens != 100 || c.OutputTokens != 50 {
		t.Errorf("tokens = %d/%d", c.InputTokens, c.OutputTokens)
	}
	if c.Iterations != 3 {
		t.Errorf("iterations = %d", c.Iterations)
	}
	if len(c.Media) != 1 || c.Media[0].Path != "/tmp/img.png" {
		t.Errorf("media = %v", c.Media)
	}
	if c.Status != "completed" {
		t.Errorf("status = %q", c.Status)
	}
}

func TestCaptureFromRunResult_NilUsage(t *testing.T) {
	rr := &agent.RunResult{Content: "ok"}
	c := CaptureFromRunResult(rr, time.Second)
	if c.InputTokens != 0 || c.OutputTokens != 0 {
		t.Errorf("expected zero tokens, got %d/%d", c.InputTokens, c.OutputTokens)
	}
}

func TestCaptureFromPipelineResult_Nil(t *testing.T) {
	r := CaptureFromPipelineResult(nil, 3*time.Second)
	if r.Status != "failed" {
		t.Errorf("status = %q, want \"failed\"", r.Status)
	}
}

func TestCaptureFromPipelineResult_WithData(t *testing.T) {
	pr := &plpkg.RunResult{
		Content:    "world",
		Iterations: 5,
		TotalUsage: providers.Usage{PromptTokens: 200, CompletionTokens: 80},
		MediaResults: []plpkg.MediaResult{
			{Path: "/tmp/audio.mp3", ContentType: "audio/mpeg"},
		},
	}
	c := CaptureFromPipelineResult(pr, 4*time.Second)
	if c.Content != "world" {
		t.Errorf("content = %q", c.Content)
	}
	if c.InputTokens != 200 || c.OutputTokens != 80 {
		t.Errorf("tokens = %d/%d", c.InputTokens, c.OutputTokens)
	}
	if len(c.Media) != 1 || c.Media[0].MimeType != "audio/mpeg" {
		t.Errorf("media = %v", c.Media)
	}
}
