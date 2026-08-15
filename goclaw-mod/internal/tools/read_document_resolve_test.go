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

func TestResolveDocumentFileRequiresExactMediaID(t *testing.T) {
	workspace := t.TempDir()
	docPath := filepath.Join(workspace, ".uploads", "codex-9c8914a5.zip")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := []providers.MediaRef{{
		ID:       uuid.NewString(),
		Kind:     "document",
		MimeType: "application/zip",
		Path:     docPath,
	}}

	tool := NewReadDocumentTool(nil, nil)
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaDocRefs(ctx, refs)
	if gotPath, _, err := tool.resolveDocumentFile(ctx, "codex.zip", ""); err == nil {
		t.Fatalf("filename alias resolved to %q, want exact media_id error", gotPath)
	}

	gotPath, gotMime, err := tool.resolveDocumentFile(ctx, refs[0].ID, "")
	if err != nil {
		t.Fatalf("exact media_id returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
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
	workspace := t.TempDir()
	oldPath := filepath.Join(workspace, ".uploads", "old.pdf")
	latestPath := filepath.Join(workspace, ".uploads", "latest.pdf")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oldPath, latestPath} {
		if err := os.WriteFile(path, []byte("pdf"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs := []providers.MediaRef{
		{ID: uuid.NewString(), Kind: "document", Path: oldPath, MimeType: "application/pdf"},
		{ID: uuid.NewString(), Kind: "document", Path: latestPath, MimeType: "application/pdf"},
	}

	tool := NewReadDocumentTool(nil, nil)
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaDocRefs(ctx, refs)
	gotPath, _, err := tool.resolveDocumentFile(ctx, "", "")
	if err != nil {
		t.Fatalf("resolveDocumentFile returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(refs[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want most recent %q", gotPath, wantPath)
	}
}

func TestResolveDocumentFileRejectsRefPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.pdf")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "document",
		Path:     outsidePath,
		MimeType: "application/pdf",
	}

	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithToolTeamWorkspace(ctx, outsideDir)
	ctx = WithMediaDocRefs(ctx, []providers.MediaRef{ref})
	gotPath, _, err := NewReadDocumentTool(nil, nil).resolveDocumentFile(ctx, ref.ID, "")
	if err == nil {
		t.Fatalf("outside ref path resolved to %q, want containment error", gotPath)
	}
}

func TestResolveDocumentFileAcceptsDelegationInputRef(t *testing.T) {
	ctx, inputs, _ := delegationArtifactToolContext(t)
	docPath := filepath.Join(inputs, "brief.pdf")
	if err := os.WriteFile(docPath, []byte("pdf"), 0o440); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "document",
		Path:     "inputs/brief.pdf",
		MimeType: "application/pdf",
	}
	ctx = WithMediaDocRefs(ctx, []providers.MediaRef{ref})

	gotPath, _, err := NewReadDocumentTool(nil, nil).resolveDocumentFile(ctx, ref.ID, "")
	if err != nil {
		t.Fatalf("delegation input ref returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want staged input %q", gotPath, wantPath)
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
	if !strings.Contains(result.ForLLM, "unzip -l") ||
		!strings.Contains(result.ForLLM, ".uploads/codex-9c8914a5.zip") {
		t.Fatalf("expected unzip hint with logical path, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, workspace) || strings.Contains(result.ForLLM, docPath) {
		t.Fatalf("archive hint leaked physical workspace path: %s", result.ForLLM)
	}
}

func TestReadDocumentRejectsMediaIDAndPathTogether(t *testing.T) {
	tool := NewReadDocumentTool(nil, nil)
	result := tool.Execute(context.Background(), map[string]any{
		"prompt":   "Inspect",
		"media_id": uuid.NewString(),
		"path":     ".uploads/document.pdf",
	})

	if !result.IsError || !strings.Contains(result.ForLLM, "either media_id or path") {
		t.Fatalf("result = %#v, want mutually exclusive argument error", result)
	}
}
