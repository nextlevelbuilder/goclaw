package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// emptyRegistry is a non-nil provider registry with no providers configured, so
// the fallback chain resolves to an error result instead of panicking on a nil
// receiver. It lets the fall-through tests reach the chain without a live LLM.
func emptyRegistry() *providers.Registry {
	return providers.NewRegistry(func(context.Context) uuid.UUID { return uuid.Nil })
}

// recordingParser is a test double for DocumentParser that records calls.
type recordingParser struct {
	supports       bool
	text           string
	err            error
	supportsCalled bool
	extractCalled  bool
}

func (f *recordingParser) Supports(string) bool {
	f.supportsCalled = true
	return f.supports
}

func (f *recordingParser) Extract(context.Context, string, string) (string, error) {
	f.extractCalled = true
	return f.text, f.err
}

// newPDFTool sets up a ReadDocumentTool over a workspace holding a small PDF and
// returns the tool, the args selecting that file, and the ctx with workspace.
func newPDFTool(t *testing.T, parser DocumentParser) (*ReadDocumentTool, map[string]any, context.Context) {
	t.Helper()
	workspace := t.TempDir()
	docPath := filepath.Join(workspace, "doc.pdf")
	if err := os.WriteFile(docPath, []byte("%PDF-1.4 raw bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewReadDocumentTool(emptyRegistry(), nil)
	tool.SetLocalParser(parser)
	ctx := WithToolWorkspace(context.Background(), workspace)
	return tool, map[string]any{"prompt": "summarize", "path": "doc.pdf"}, ctx
}

func TestReadDocument_LocalHitReturnsTextWithNoSpend(t *testing.T) {
	parser := &recordingParser{supports: true, text: "EXTRACTED DOCUMENT TEXT"}
	tool, args, ctx := newPDFTool(t, parser)

	result := tool.Execute(ctx, args)

	if !parser.extractCalled {
		t.Fatal("expected Extract to be called on a supported mime")
	}
	if result.ForLLM != "EXTRACTED DOCUMENT TEXT" {
		t.Errorf("ForLLM = %q, want extracted text", result.ForLLM)
	}
	if result.Usage != nil {
		t.Errorf("local hit must report no Usage, got %+v", result.Usage)
	}
	if result.Provider != "" {
		t.Errorf("local hit must report empty Provider, got %q", result.Provider)
	}
	if result.IsError {
		t.Error("local hit should not be an error result")
	}
}

func TestReadDocument_LocalMissFallsThroughToChain(t *testing.T) {
	parser := &recordingParser{supports: true, err: ErrParserEmpty}
	tool, args, ctx := newPDFTool(t, parser)

	result := tool.Execute(ctx, args)

	if !parser.extractCalled {
		t.Fatal("expected Extract to be attempted")
	}
	// With a nil registry the chain cannot resolve a provider, so falling
	// through surfaces an error result — proving we did not short-circuit.
	if !result.IsError {
		t.Errorf("expected fall-through to the (unconfigured) chain, got ForLLM=%q", result.ForLLM)
	}
}

func TestReadDocument_DisabledNeverConsultsParser(t *testing.T) {
	// Supports=false models a disabled / unavailable parser.
	parser := &recordingParser{supports: false}
	tool, args, ctx := newPDFTool(t, parser)

	_ = tool.Execute(ctx, args)

	if parser.extractCalled {
		t.Error("Extract must not be called when Supports returns false")
	}
}

func TestReadDocument_NilParserUnchanged(t *testing.T) {
	// No parser injected => the local-first block is skipped entirely.
	workspace := t.TempDir()
	docPath := filepath.Join(workspace, "doc.pdf")
	if err := os.WriteFile(docPath, []byte("%PDF-1.4 raw bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewReadDocumentTool(emptyRegistry(), nil)
	ctx := WithToolWorkspace(context.Background(), workspace)
	result := tool.Execute(ctx, map[string]any{"prompt": "summarize", "path": "doc.pdf"})
	// Reaches the chain (nil registry) => error result, no panic.
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
