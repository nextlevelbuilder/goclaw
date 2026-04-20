package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseGitHubTargetSpec(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		wantKind string
		wantPath string
		wantRef  string
		wantTag  string
		wantNum  int
	}{
		{
			name:     "blob url",
			spec:     "https://github.com/owner/repo/blob/main/path/to/file.go",
			wantKind: "file",
			wantPath: "path/to/file.go",
			wantRef:  "main",
		},
		{
			name:     "tree url with encoded branch",
			spec:     "https://github.com/owner/repo/tree/feature%2Fbranch/src",
			wantKind: "tree",
			wantPath: "src",
			wantRef:  "feature/branch",
		},
		{
			name:     "issue url",
			spec:     "https://github.com/owner/repo/issues/123",
			wantKind: "issue",
			wantNum:  123,
		},
		{
			name:     "release url",
			spec:     "https://github.com/owner/repo/releases/tag/v1.2.3",
			wantKind: "release",
			wantTag:  "v1.2.3",
		},
		{
			name:     "plain remote",
			spec:     "git@github.com:owner/repo.git",
			wantKind: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, ok := parseGitHubTargetSpec(tt.spec)
			if !ok {
				t.Fatalf("parseGitHubTargetSpec(%q) returned false", tt.spec)
			}
			if target.Owner != "owner" || target.Repo != "repo" {
				t.Fatalf("owner/repo = %q/%q, want owner/repo", target.Owner, target.Repo)
			}
			if target.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", target.Kind, tt.wantKind)
			}
			if target.Path != tt.wantPath {
				t.Fatalf("path = %q, want %q", target.Path, tt.wantPath)
			}
			if target.Ref != tt.wantRef {
				t.Fatalf("ref = %q, want %q", target.Ref, tt.wantRef)
			}
			if target.Tag != tt.wantTag {
				t.Fatalf("tag = %q, want %q", target.Tag, tt.wantTag)
			}
			if target.Number != tt.wantNum {
				t.Fatalf("number = %d, want %d", target.Number, tt.wantNum)
			}
		})
	}
}

func TestGitHubToolExecuteRepoAndFile(t *testing.T) {
	tool := NewGitHubTool(GitHubToolConfig{Token: "test-token"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		switch {
		case r.URL.Path == "/repos/owner/repo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"full_name":       "owner/repo",
				"description":     "repo description",
				"default_branch":   "main",
				"html_url":        "https://github.com/owner/repo",
				"visibility":      "public",
				"private":         false,
				"archived":        false,
				"fork":            false,
				"stargazers_count": 12,
				"forks_count":     3,
				"open_issues_count": 4,
				"language":        "Go",
				"updated_at":      "2026-04-18T12:34:56Z",
			})
		case r.URL.Path == "/repos/owner/repo/contents/README.md":
			if got := r.URL.Query().Get("ref"); got != "main" {
				t.Fatalf("ref = %q, want main", got)
			}
			body := base64.StdEncoding.EncodeToString([]byte("hello from GitHub"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":         "file",
				"name":         "README.md",
				"path":         "README.md",
				"sha":          "abc123",
				"size":         17,
				"html_url":     "https://github.com/owner/repo/blob/main/README.md",
				"download_url": "https://raw.githubusercontent.com/owner/repo/main/README.md",
				"content":      body,
				"encoding":     "base64",
				"truncated":    false,
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	tool.apiBase = server.URL

	repoResult := tool.Execute(context.Background(), map[string]any{
		"repo": "owner/repo",
		"kind": "repo",
	})
	if repoResult.IsError {
		t.Fatalf("repo result error: %s", repoResult.ForLLM)
	}
	if !strings.Contains(repoResult.ForLLM, "Repository: owner/repo") {
		t.Fatalf("repo output missing repository line: %s", repoResult.ForLLM)
	}
	if !strings.Contains(repoResult.ForLLM, "Default branch: main") {
		t.Fatalf("repo output missing default branch: %s", repoResult.ForLLM)
	}

	fileResult := tool.Execute(context.Background(), map[string]any{
		"repo": "https://github.com/owner/repo/blob/main/README.md",
	})
	if fileResult.IsError {
		t.Fatalf("file result error: %s", fileResult.ForLLM)
	}
	if !strings.Contains(fileResult.ForLLM, "<<<EXTERNAL_UNTRUSTED_CONTENT>>>") {
		t.Fatalf("file output missing external wrapper: %s", fileResult.ForLLM)
	}
	if !strings.Contains(fileResult.ForLLM, "Content:\nhello from GitHub") {
		t.Fatalf("file output missing file content: %s", fileResult.ForLLM)
	}
	if !strings.Contains(fileResult.ForLLM, "Path: README.md") {
		t.Fatalf("file output missing path: %s", fileResult.ForLLM)
	}
}
