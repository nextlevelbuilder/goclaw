package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GithubPublishDirTool publishes a directory of files from the workspace to a
// GitHub repository as a NEW branch + pull request, using the caller's connected
// GitHub (Composio) tools. The Composio GitHub tools are invoked SERVER-SIDE
// (via the parent agent's registry), so the — potentially large — file contents
// never pass through the model's context. This is the "publish" half of the
// hybrid where a connected coding agent (e.g. Claude Code) refactors files into
// the shared workspace and the agent then opens a PR through the user's own
// GitHub connection (no PAT).
type GithubPublishDirTool struct {
	workspace string
}

// NewGithubPublishDirTool wires the tool with the workspace root that the
// sandbox (and thus a delegated agent) writes into.
func NewGithubPublishDirTool(workspace string) *GithubPublishDirTool {
	return &GithubPublishDirTool{workspace: workspace}
}

func (t *GithubPublishDirTool) Name() string { return "github_publish_dir" }

func (t *GithubPublishDirTool) Description() string {
	return "Publish a directory of files from the workspace to a GitHub repository as a NEW branch and pull request, using the user's connected GitHub. Reads the files server-side (their contents never enter this conversation), commits them all in one commit, and opens the PR — returns the PR URL. Use this to ship work a connected coding agent (e.g. Claude Code) wrote into the workspace, without a PAT."
}

func (t *GithubPublishDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"owner":          map[string]any{"type": "string", "description": "GitHub repository owner (user or org)."},
			"repo":           map[string]any{"type": "string", "description": "Repository name, without the .git extension."},
			"source_dir":     map[string]any{"type": "string", "description": "Workspace-relative directory whose files should be published (e.g. the folder the refactor was written to)."},
			"branch":         map[string]any{"type": "string", "description": "New branch to create for the changes (must not already exist)."},
			"base_branch":    map[string]any{"type": "string", "description": "Base branch to branch from and target the PR at. Omit to use the repository's default branch."},
			"commit_message": map[string]any{"type": "string", "description": "Commit message."},
			"pr_title":       map[string]any{"type": "string", "description": "Pull request title."},
			"pr_body":        map[string]any{"type": "string", "description": "Pull request description (markdown)."},
		},
		"required": []string{"owner", "repo", "source_dir", "branch", "commit_message", "pr_title"},
	}
}

const (
	composioToolPrefix = "mcp_composio_mcp__"
	maxPublishFiles    = 800
	maxPublishTotal    = 8 << 20 // 8 MiB total across all files
	maxPublishFileSize = 1 << 20 // 1 MiB per file
)

var prURLRe = regexp.MustCompile(`https://github\.com/[^\s"']+/pull/\d+`)

// shaRe matches a git commit SHA — the first one in a GITHUB_GET_A_BRANCH
// response is the branch's tip commit.
var shaRe = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

func (t *GithubPublishDirTool) Execute(ctx context.Context, args map[string]any) *Result {
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	sourceDir, _ := args["source_dir"].(string)
	branch, _ := args["branch"].(string)
	baseBranch, _ := args["base_branch"].(string)
	commitMsg, _ := args["commit_message"].(string)
	prTitle, _ := args["pr_title"].(string)
	prBody, _ := args["pr_body"].(string)

	for k, v := range map[string]string{"owner": owner, "repo": repo, "source_dir": sourceDir, "branch": branch, "commit_message": commitMsg, "pr_title": prTitle} {
		if strings.TrimSpace(v) == "" {
			return ErrorResult(fmt.Sprintf("%s is required", k))
		}
	}

	reg := ParentRegistryFromCtx(ctx)
	if reg == nil {
		return ErrorResult("github_publish_dir: no tool registry in context — cannot reach the GitHub tools")
	}

	root, err := t.resolveDir(sourceDir)
	if err != nil {
		return ErrorResult(err.Error())
	}
	upserts, skipped, err := collectUpserts(root)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if len(upserts) == 0 {
		return ErrorResult(fmt.Sprintf("no publishable text files found under %q", sourceDir))
	}

	if strings.TrimSpace(baseBranch) == "" {
		baseBranch = t.resolveDefaultBranch(ctx, reg, owner, repo)
	}
	if strings.TrimSpace(baseBranch) == "" {
		baseBranch = "main"
	}

	// The whole flow uses only the curated/granted GitHub tools (get-branch,
	// create-reference, create-or-update-file, create-pull-request) — the same
	// ones the agent can call directly — via reg.Execute, which resolves and
	// activates them (inline or deferred) with the caller's Composio identity.

	// 1) Resolve the base branch's tip commit SHA.
	brRes := reg.Execute(ctx, composioToolPrefix+"GITHUB_GET_A_BRANCH", map[string]any{
		"owner": owner, "repo": repo, "branch": baseBranch,
	})
	if brRes == nil || brRes.IsError {
		return ErrorResult(fmt.Sprintf("couldn't read the base branch %q from GitHub (is GitHub connected for this agent?): %s", baseBranch, resultText(brRes)))
	}
	baseSHA := shaRe.FindString(resultText(brRes))
	if baseSHA == "" {
		return ErrorResult(fmt.Sprintf("couldn't resolve the tip commit SHA of base branch %q", baseBranch))
	}

	// 2) Create the new branch off that SHA.
	refRes := reg.Execute(ctx, composioToolPrefix+"GITHUB_CREATE_A_REFERENCE", map[string]any{
		"owner": owner, "repo": repo, "ref": "refs/heads/" + branch, "sha": baseSHA,
	})
	if refRes == nil || refRes.IsError {
		return ErrorResult(fmt.Sprintf("couldn't create branch %q (it may already exist — pick a new name): %s", branch, resultText(refRes)))
	}

	// 3) Write each file onto the new branch (content is raw text; Composio
	//    encodes it). One commit per file — the curated set has no multi-file
	//    commit, and this is exactly what works by hand.
	for i, u := range upserts {
		path, _ := u["path"].(string)
		content, _ := u["content"].(string)
		fileMsg := commitMsg
		if len(upserts) > 1 {
			fileMsg = fmt.Sprintf("%s (%d/%d: %s)", commitMsg, i+1, len(upserts), path)
		}
		wRes := reg.Execute(ctx, composioToolPrefix+"GITHUB_CREATE_OR_UPDATE_FILE_CONTENTS", map[string]any{
			"owner": owner, "repo": repo, "branch": branch,
			"path": path, "message": fileMsg, "content": content,
		})
		if wRes == nil || wRes.IsError {
			return ErrorResult(fmt.Sprintf("created branch %q and wrote %d/%d files, then failed on %q: %s", branch, i, len(upserts), path, resultText(wRes)))
		}
	}

	// 4) Open the PR.
	prRes := reg.Execute(ctx, composioToolPrefix+"GITHUB_CREATE_A_PULL_REQUEST", map[string]any{
		"owner": owner,
		"repo":  repo,
		"head":  branch,
		"base":  baseBranch,
		"title": prTitle,
		"body":  prBody,
	})
	if prRes == nil || prRes.IsError {
		return ErrorResult(fmt.Sprintf("published %d files to branch %q, but opening the PR failed: %s", len(upserts), branch, resultText(prRes)))
	}

	msg := fmt.Sprintf("Published %d files to %s/%s on branch %q (base %q) and opened a pull request.", len(upserts), owner, repo, branch, baseBranch)
	if skipped > 0 {
		msg += fmt.Sprintf(" %d binary/oversized files were skipped.", skipped)
	}
	if url := prURLRe.FindString(resultText(prRes)); url != "" {
		msg += "\nPR: " + url
	} else {
		msg += "\n" + truncateStr(resultText(prRes), 400)
	}
	return NewResult(msg)
}

// resolveDir joins source_dir onto the workspace and guarantees the result stays
// within the workspace root (no path traversal).
func (t *GithubPublishDirTool) resolveDir(sourceDir string) (string, error) {
	base := filepath.Clean(t.workspace)
	full := filepath.Clean(filepath.Join(base, sourceDir))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("source_dir %q is outside the workspace", sourceDir)
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("source_dir %q not found in the workspace", sourceDir)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source_dir %q is not a directory", sourceDir)
	}
	return full, nil
}

// collectUpserts walks root and returns [{path, content}] entries for every
// text file, skipping .git, binaries (NUL byte), and oversized files. Paths are
// repo-relative with forward slashes. Returns (entries, skippedCount, error).
func collectUpserts(root string) ([]map[string]any, int, error) {
	var upserts []map[string]any
	var total int64
	skipped := 0
	err := filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(upserts) >= maxPublishFiles {
			return fmt.Errorf("too many files to publish (>%d) — narrow source_dir", maxPublishFiles)
		}
		if info.Size() > maxPublishFileSize {
			skipped++
			return nil
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		// Skip binary files (a NUL byte is a reliable text/binary discriminator).
		if len(data) > 0 && strings.IndexByte(string(data), 0) >= 0 {
			skipped++
			return nil
		}
		total += int64(len(data))
		if total > maxPublishTotal {
			return fmt.Errorf("total content exceeds %d bytes — narrow source_dir", maxPublishTotal)
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		upserts = append(upserts, map[string]any{
			"path":    filepath.ToSlash(rel),
			"content": string(data),
		})
		return nil
	})
	if err != nil {
		return nil, skipped, err
	}
	return upserts, skipped, nil
}

// resolveDefaultBranch asks GitHub for the repo's default branch. Returns "" on
// any failure (the caller falls back to "main").
func (t *GithubPublishDirTool) resolveDefaultBranch(ctx context.Context, reg *Registry, owner, repo string) string {
	res := reg.Execute(ctx, composioToolPrefix+"GITHUB_GET_A_REPOSITORY", map[string]any{"owner": owner, "repo": repo})
	if res == nil || res.IsError {
		return ""
	}
	// The bridge returns the API JSON as text; pull default_branch out of it.
	var parsed struct {
		DefaultBranch string `json:"default_branch"`
		Data          struct {
			DefaultBranch string `json:"default_branch"`
		} `json:"data"`
	}
	txt := resultText(res)
	if i := strings.IndexByte(txt, '{'); i >= 0 {
		_ = json.Unmarshal([]byte(txt[i:]), &parsed)
	}
	if parsed.DefaultBranch != "" {
		return parsed.DefaultBranch
	}
	return parsed.Data.DefaultBranch
}

func resultText(r *Result) string {
	if r == nil {
		return ""
	}
	return r.ForLLM
}
