package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestResolveDocumentFileAcceptsWorkspacePath(t *testing.T) {
	workspace := t.TempDir()
	uploadDir := filepath.Join(workspace, ".uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(uploadDir, "codex-9c8914a5.zip")
	if err := os.WriteFile(docPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantPath, err := filepath.EvalSymlinks(docPath)
	if err != nil {
		t.Fatal(err)
	}

	tool := NewReadDocumentTool(nil, nil)
	ctx := WithToolWorkspace(context.Background(), workspace)
	gotPath, gotMime, err := tool.resolveDocumentFile(ctx, "", ".uploads/codex-9c8914a5.zip")
	if err != nil {
		t.Fatalf("resolveDocumentFile returned error: %v", err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	if gotMime != "application/zip" {
		t.Fatalf("mime = %q, want application/zip", gotMime)
	}
}

func TestResolveDocumentFileMatchesUploadedFilenameAlias(t *testing.T) {
	workspace := t.TempDir()
	docPath := filepath.Join(workspace, ".uploads", "codex-9c8914a5.zip")
	refs := []providers.MediaRef{{
		ID:       uuid.NewString(),
		Kind:     "document",
		MimeType: "application/zip",
		Path:     docPath,
	}}

	tool := NewReadDocumentTool(nil, nil)
	ctx := WithMediaDocRefs(context.Background(), refs)
	gotPath, gotMime, err := tool.resolveDocumentFile(ctx, "codex.zip", "")
	if err != nil {
		t.Fatalf("resolveDocumentFile returned error: %v", err)
	}
	if gotPath != docPath {
		t.Fatalf("path = %q, want %q", gotPath, docPath)
	}
	if gotMime != "application/zip" {
		t.Fatalf("mime = %q, want application/zip", gotMime)
	}
}

func TestResolveDocumentFileInvalidMediaIDReturnsError(t *testing.T) {
	refs := []providers.MediaRef{
		{ID: uuid.NewString(), Kind: "document", Path: "/workspace/.uploads/old.pdf", MimeType: "application/pdf"},
		{ID: uuid.NewString(), Kind: "document", Path: "/workspace/.uploads/latest.pdf", MimeType: "application/pdf"},
	}

	tool := NewReadDocumentTool(nil, nil)
	ctx := WithMediaDocRefs(context.Background(), refs)
	gotPath, _, err := tool.resolveDocumentFile(ctx, "not-a-real-media-id", "")
	if err == nil {
		t.Fatalf("resolveDocumentFile returned path %q, want explicit media_id error", gotPath)
	}
	if !strings.Contains(err.Error(), "not-a-real-media-id") {
		t.Fatalf("error = %q, want requested media_id", err.Error())
	}
}

func TestResolveDocumentFileOmittedMediaIDUsesLastRef(t *testing.T) {
	refs := []providers.MediaRef{
		{ID: uuid.NewString(), Kind: "document", Path: "/workspace/.uploads/old.pdf", MimeType: "application/pdf"},
		{ID: uuid.NewString(), Kind: "document", Path: "/workspace/.uploads/latest.pdf", MimeType: "application/pdf"},
	}

	tool := NewReadDocumentTool(nil, nil)
	ctx := WithMediaDocRefs(context.Background(), refs)
	gotPath, _, err := tool.resolveDocumentFile(ctx, "", "")
	if err != nil {
		t.Fatalf("resolveDocumentFile returned error: %v", err)
	}
	if gotPath != refs[1].Path {
		t.Fatalf("path = %q, want most recent %q", gotPath, refs[1].Path)
	}
}

func TestReadDocumentArchiveReturnsExecHint(t *testing.T) {
	workspace := t.TempDir()
	docPath := filepath.Join(workspace, ".uploads", "codex-9c8914a5.zip")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadDocumentTool(nil, nil)
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = store.WithTenantID(store.WithAgentID(ctx, uuid.New()), uuid.New())
	result := tool.Execute(ctx, map[string]any{
		"prompt": "Inspect this archive",
		"path":   ".uploads/codex-9c8914a5.zip",
	})

	if result.IsError {
		t.Fatalf("expected archive hint, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "unzip -l") || !strings.Contains(result.ForLLM, docPath) {
		t.Fatalf("expected unzip hint with path, got: %s", result.ForLLM)
	}
}
