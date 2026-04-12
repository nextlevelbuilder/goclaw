package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type OpenHandsDelegateConfig struct {
	BaseURL              string
	BearerToken          string
	DefaultWaitSec       int
	MaxWaitSec           int
	DefaultMaxRuntimeSec int
	DefaultMaxIterations int
	MaxUploadMB          int
	AllowedRepoPrefixes  []string
}

type OpenHandsDelegateTool struct {
	workspace string
	config    OpenHandsDelegateConfig
	client    *http.Client
}

type openHandsCreateRequest struct {
	Objective              string            `json:"objective"`
	Mode                   string            `json:"mode"`
	RepoURL                string            `json:"repo_url,omitempty"`
	Ref                    string            `json:"ref,omitempty"`
	WorkspaceSubdir        string            `json:"workspace_subdir,omitempty"`
	OverlayPatchB64        string            `json:"overlay_patch_b64,omitempty"`
	OverlayUntrackedTarB64 string            `json:"overlay_untracked_tar_b64,omitempty"`
	SnapshotTarB64         string            `json:"snapshot_tar_b64,omitempty"`
	Requester              string            `json:"requester,omitempty"`
	SourceAgent            string            `json:"source_agent,omitempty"`
	MaxRuntimeSec          int               `json:"max_runtime_sec,omitempty"`
	MaxIterations          int               `json:"max_iterations,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

type openHandsJobResponse struct {
	Job openHandsJob `json:"job"`
}

type openHandsJob struct {
	ID              string            `json:"id"`
	Status          string            `json:"status"`
	Mode            string            `json:"mode"`
	RepoURL         string            `json:"repo_url,omitempty"`
	Ref             string            `json:"ref,omitempty"`
	WorkspaceSubdir string            `json:"workspace_subdir,omitempty"`
	Objective       string            `json:"objective"`
	Summary         string            `json:"summary,omitempty"`
	Error           string            `json:"error,omitempty"`
	FilesChanged    []string          `json:"files_changed,omitempty"`
	TestsRun        []string          `json:"tests_run,omitempty"`
	Risks           []string          `json:"risks,omitempty"`
	Artifacts       map[string]string `json:"artifacts,omitempty"`
	CreatedAt       string            `json:"created_at,omitempty"`
	UpdatedAt       string            `json:"updated_at,omitempty"`
	FinishedAt      string            `json:"finished_at,omitempty"`
}

type gitWorkspaceContext struct {
	root            string
	repoURL         string
	ref             string
	patchBase       string
	workspaceSubdir string
	overlayPatchB64 string
	untrackedTarB64 string
}

func NewOpenHandsDelegateTool(workspace string, cfg OpenHandsDelegateConfig) *OpenHandsDelegateTool {
	if cfg.DefaultWaitSec <= 0 {
		cfg.DefaultWaitSec = 1800
	}
	if cfg.MaxWaitSec <= 0 {
		cfg.MaxWaitSec = 3600
	}
	if cfg.DefaultMaxRuntimeSec <= 0 {
		cfg.DefaultMaxRuntimeSec = 1800
	}
	if cfg.DefaultMaxIterations <= 0 {
		cfg.DefaultMaxIterations = 150
	}
	if cfg.MaxUploadMB <= 0 {
		cfg.MaxUploadMB = 64
	}
	return &OpenHandsDelegateTool{
		workspace: workspace,
		config:    cfg,
		client:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (t *OpenHandsDelegateTool) Name() string { return "openhands_delegate" }

func (t *OpenHandsDelegateTool) Description() string {
	return "Delegate a coding task to the remote OpenHands specialist. It can mirror the current git workspace exactly by sending the repo ref plus local uncommitted changes."
}

func (t *OpenHandsDelegateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"objective": map[string]any{
				"type":        "string",
				"description": "Concrete coding objective for the remote OpenHands specialist.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Workspace handoff mode. Default: auto (prefer git_ref with dirty overlay, fallback to snapshot).",
				"enum":        []string{"auto", "git_ref", "snapshot"},
			},
			"repo_url": map[string]any{
				"type":        "string",
				"description": "Override repository URL for git_ref mode. If omitted, uses the current git remote when available.",
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "Git ref to check out on the remote workspace. Defaults to the current HEAD commit when using the current repo.",
			},
			"workspace_subdir": map[string]any{
				"type":        "string",
				"description": "Optional subdirectory inside the workspace to focus on.",
			},
			"include_dirty": map[string]any{
				"type":        "boolean",
				"description": "Include current uncommitted tracked and untracked changes. Default: true.",
			},
			"wait": map[string]any{
				"type":        "boolean",
				"description": "Wait for the remote job to finish before returning. Default: true.",
			},
			"timeout_sec": map[string]any{
				"type":        "number",
				"description": "How long this tool should wait for the remote job result. Default comes from config.",
				"minimum":     30.0,
			},
			"max_runtime_sec": map[string]any{
				"type":        "number",
				"description": "Maximum runtime the remote OpenHands job may spend before timing out.",
				"minimum":     60.0,
			},
			"max_iterations": map[string]any{
				"type":        "number",
				"description": "Maximum remote OpenHands agent iterations.",
				"minimum":     1.0,
			},
		},
		"required": []string{"objective"},
	}
}

func (t *OpenHandsDelegateTool) Execute(ctx context.Context, args map[string]any) *Result {
	if strings.TrimSpace(t.config.BaseURL) == "" || strings.TrimSpace(t.config.BearerToken) == "" {
		return ErrorResult("OpenHands adapter is not configured")
	}

	objective := strings.TrimSpace(ohStringArg(args, "objective"))
	if objective == "" {
		return ErrorResult("objective is required")
	}

	mode := strings.ToLower(strings.TrimSpace(ohStringArg(args, "mode")))
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "git_ref" && mode != "snapshot" {
		return ErrorResult("mode must be one of: auto, git_ref, snapshot")
	}

	includeDirty := ohBoolArg(args, "include_dirty", true)
	wait := ohBoolArg(args, "wait", true)
	waitSec := ohIntArg(args, "timeout_sec", t.config.DefaultWaitSec)
	if waitSec < 30 {
		waitSec = 30
	}
	if waitSec > t.config.MaxWaitSec {
		waitSec = t.config.MaxWaitSec
	}

	maxRuntimeSec := ohIntArg(args, "max_runtime_sec", t.config.DefaultMaxRuntimeSec)
	maxIterations := ohIntArg(args, "max_iterations", t.config.DefaultMaxIterations)
	explicitRepoURL := strings.TrimSpace(ohStringArg(args, "repo_url"))
	explicitRef := strings.TrimSpace(ohStringArg(args, "ref"))
	explicitSubdir := sanitizeSubdir(strings.TrimSpace(ohStringArg(args, "workspace_subdir")))
	if explicitSubdir == "__invalid__" {
		return ErrorResult("workspace_subdir must stay inside the workspace")
	}

	createReq, prepSummary, err := t.buildCreateRequest(ctx, mode, includeDirty, explicitRepoURL, explicitRef, explicitSubdir, objective, maxRuntimeSec, maxIterations)
	if err != nil {
		return ErrorResult(err.Error())
	}

	created, err := t.createJob(ctx, createReq)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create OpenHands job: %v", err))
	}

	if !wait {
		return NewResult(fmt.Sprintf("OpenHands job queued.\n- Job ID: %s\n- Mode: %s\n- Workspace: %s", created.Job.ID, created.Job.Mode, prepSummary))
	}

	job, err := t.waitForJob(ctx, created.Job.ID, waitSec)
	if err != nil {
		return ErrorResult(fmt.Sprintf("OpenHands job %s did not complete cleanly: %v", created.Job.ID, err))
	}
	if job.Status != "succeeded" {
		msg := fmt.Sprintf("OpenHands job %s finished with status=%s", job.ID, job.Status)
		if strings.TrimSpace(job.Error) != "" {
			msg += "\nError: " + job.Error
		}
		if strings.TrimSpace(job.Summary) != "" {
			msg += "\nSummary: " + job.Summary
		}
		return ErrorResult(msg)
	}

	return NewResult(formatOpenHandsResult(job, prepSummary))
}

func (t *OpenHandsDelegateTool) buildCreateRequest(
	ctx context.Context,
	mode string,
	includeDirty bool,
	explicitRepoURL string,
	explicitRef string,
	explicitSubdir string,
	objective string,
	maxRuntimeSec int,
	maxIterations int,
) (openHandsCreateRequest, string, error) {
	cwd := ToolCwdFromCtx(ctx)
	if cwd == "" {
		cwd = ToolWorkspaceFromCtx(ctx)
	}
	if cwd == "" {
		cwd = t.workspace
	}
	cwd = filepath.Clean(cwd)

	preferredMode := mode
	if preferredMode == "auto" {
		preferredMode = "git_ref"
	}

	if preferredMode == "git_ref" {
		if explicitRepoURL != "" && !isGitRepo(ctx, cwd) {
			createReq, summary, err := t.buildDetachedGitRequest(ctx, cwd, explicitRepoURL, explicitRef, explicitSubdir, objective, maxRuntimeSec, maxIterations)
			if err == nil {
				return createReq, summary, nil
			}
			if mode == "git_ref" {
				return openHandsCreateRequest{}, "", err
			}
		}
		gitCtx, err := t.resolveGitWorkspace(ctx, cwd, includeDirty, explicitRepoURL, explicitRef, explicitSubdir)
		if err == nil {
			return openHandsCreateRequest{
				Objective:              objective,
				Mode:                   "git_ref",
				RepoURL:                gitCtx.repoURL,
				Ref:                    gitCtx.ref,
				WorkspaceSubdir:        gitCtx.workspaceSubdir,
				OverlayPatchB64:        gitCtx.overlayPatchB64,
				OverlayUntrackedTarB64: gitCtx.untrackedTarB64,
				Requester:              ToolChannelFromCtx(ctx),
				SourceAgent:            ToolAgentKeyFromCtx(ctx),
				MaxRuntimeSec:          maxRuntimeSec,
				MaxIterations:          maxIterations,
				Metadata: map[string]string{
					"session_key": ToolSessionKeyFromCtx(ctx),
					"workspace":   cwd,
					"mode":        "git_ref",
				},
			}, fmt.Sprintf("git repo %s @ %s", gitCtx.repoURL, gitCtx.ref), nil
		}
		if mode == "git_ref" {
			return openHandsCreateRequest{}, "", err
		}
	}

	snapshotRoot := cwd
	if ws := ToolWorkspaceFromCtx(ctx); ws != "" {
		snapshotRoot = filepath.Clean(ws)
	}
	snapshotSubdir := explicitSubdir
	if snapshotSubdir == "" {
		if rel, err := filepath.Rel(snapshotRoot, cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			snapshotSubdir = sanitizeSubdir(rel)
			if snapshotSubdir == "__invalid__" {
				snapshotSubdir = ""
			}
		}
	}
	snapshotB64, err := t.buildSnapshotTarball(snapshotRoot)
	if err != nil {
		return openHandsCreateRequest{}, "", err
	}
	return openHandsCreateRequest{
		Objective:       objective,
		Mode:            "snapshot",
		SnapshotTarB64:  snapshotB64,
		WorkspaceSubdir: snapshotSubdir,
		Requester:       ToolChannelFromCtx(ctx),
		SourceAgent:     ToolAgentKeyFromCtx(ctx),
		MaxRuntimeSec:   maxRuntimeSec,
		MaxIterations:   maxIterations,
		Metadata: map[string]string{
			"session_key": ToolSessionKeyFromCtx(ctx),
			"workspace":   snapshotRoot,
			"mode":        "snapshot",
		},
	}, fmt.Sprintf("snapshot of %s", snapshotRoot), nil
}

func (t *OpenHandsDelegateTool) buildDetachedGitRequest(
	ctx context.Context,
	cwd string,
	explicitRepoURL string,
	explicitRef string,
	explicitSubdir string,
	objective string,
	maxRuntimeSec int,
	maxIterations int,
) (openHandsCreateRequest, string, error) {
	repoURL := normalizeGitRemoteURL(strings.TrimSpace(explicitRepoURL))
	if repoURL == "" {
		return openHandsCreateRequest{}, "", fmt.Errorf("repo_url is required when current workspace is not a git repository")
	}
	if !repoURLAllowed(repoURL, t.config.AllowedRepoPrefixes) {
		return openHandsCreateRequest{}, "", fmt.Errorf("repo_url %q is not allowed by policy", repoURL)
	}
	req := openHandsCreateRequest{
		Objective:       objective,
		Mode:            "git_ref",
		RepoURL:         repoURL,
		Ref:             explicitRef,
		WorkspaceSubdir: explicitSubdir,
		Requester:       ToolChannelFromCtx(ctx),
		SourceAgent:     ToolAgentKeyFromCtx(ctx),
		MaxRuntimeSec:   maxRuntimeSec,
		MaxIterations:   maxIterations,
		Metadata: map[string]string{
			"session_key": ToolSessionKeyFromCtx(ctx),
			"workspace":   cwd,
			"mode":        "git_ref",
		},
	}
	summary := fmt.Sprintf("git repo %s", repoURL)
	if strings.TrimSpace(explicitRef) != "" {
		summary += " @ " + strings.TrimSpace(explicitRef)
	} else {
		summary += " @ default branch"
	}
	return req, summary, nil
}

func (t *OpenHandsDelegateTool) resolveGitWorkspace(
	ctx context.Context,
	cwd string,
	includeDirty bool,
	explicitRepoURL string,
	explicitRef string,
	explicitSubdir string,
) (gitWorkspaceContext, error) {
	root, err := gitOutput(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return gitWorkspaceContext{}, fmt.Errorf("current workspace is not a git repository")
	}
	root = strings.TrimSpace(root)
	repoURL := explicitRepoURL
	if repoURL == "" {
		repoURL, err = gitOutput(ctx, root, "remote", "get-url", "origin")
		if err != nil || strings.TrimSpace(repoURL) == "" {
			return gitWorkspaceContext{}, fmt.Errorf("git repo has no usable origin remote")
		}
	}
	repoURL = normalizeGitRemoteURL(strings.TrimSpace(repoURL))
	if !repoURLAllowed(repoURL, t.config.AllowedRepoPrefixes) {
		return gitWorkspaceContext{}, fmt.Errorf("repo_url %q is not allowed by policy", repoURL)
	}
	headCommit, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return gitWorkspaceContext{}, fmt.Errorf("failed to resolve HEAD commit: %v", err)
	}
	headCommit = strings.TrimSpace(headCommit)
	ref := headCommit
	patchBase := "HEAD"
	if explicitRef != "" {
		if _, err := gitOutput(ctx, root, "rev-parse", explicitRef); err != nil {
			return gitWorkspaceContext{}, fmt.Errorf("failed to resolve explicit ref %q: %v", explicitRef, err)
		}
		ref = explicitRef
		patchBase = explicitRef
	} else if !remoteContainsCommit(ctx, root, headCommit) {
		if !includeDirty {
			return gitWorkspaceContext{}, fmt.Errorf("current HEAD is not available on remote; retry with include_dirty=true or mode=snapshot")
		}
		baseRef, err := resolveFallbackRemoteRef(ctx, root)
		if err != nil {
			return gitWorkspaceContext{}, fmt.Errorf("current HEAD is not available on remote and no upstream/default remote ref could be resolved; retry with mode=snapshot")
		}
		ref = baseRef
		patchBase = baseRef
	}
	workspaceSubdir := explicitSubdir
	if workspaceSubdir == "" {
		if rel, err := filepath.Rel(root, cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			workspaceSubdir = sanitizeSubdir(rel)
			if workspaceSubdir == "__invalid__" {
				workspaceSubdir = ""
			}
		}
	}
	var patchB64, untrackedB64 string
	if includeDirty {
		patch, err := gitOutput(ctx, root, "diff", "--binary", patchBase)
		if err != nil {
			return gitWorkspaceContext{}, fmt.Errorf("failed to collect git diff: %v", err)
		}
		if strings.TrimSpace(patch) != "" {
			if err := enforceUploadLimit([]byte(patch), t.config.MaxUploadMB); err != nil {
				return gitWorkspaceContext{}, err
			}
			patchB64 = base64.StdEncoding.EncodeToString([]byte(patch))
		}
		untracked, err := gitOutput(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return gitWorkspaceContext{}, fmt.Errorf("failed to collect untracked files: %v", err)
		}
		if strings.TrimSpace(untracked) != "" {
			tarBytes, err := buildUntrackedTarball(root, untracked)
			if err != nil {
				return gitWorkspaceContext{}, fmt.Errorf("failed to package untracked files: %v", err)
			}
			if err := enforceUploadLimit(tarBytes, t.config.MaxUploadMB); err != nil {
				return gitWorkspaceContext{}, err
			}
			untrackedB64 = base64.StdEncoding.EncodeToString(tarBytes)
		}
	}
	return gitWorkspaceContext{
		root:            root,
		repoURL:         repoURL,
		ref:             ref,
		patchBase:       patchBase,
		workspaceSubdir: workspaceSubdir,
		overlayPatchB64: patchB64,
		untrackedTarB64: untrackedB64,
	}, nil
}

func (t *OpenHandsDelegateTool) buildSnapshotTarball(root string) (string, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if shouldSkipSnapshotPath(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			header.Linkname = target
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, file); err != nil {
				file.Close()
				return err
			}
			file.Close()
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gzw.Close(); err != nil {
		return "", err
	}
	if err := enforceUploadLimit(buf.Bytes(), t.config.MaxUploadMB); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (t *OpenHandsDelegateTool) createJob(ctx context.Context, req openHandsCreateRequest) (openHandsJobResponse, error) {
	var out openHandsJobResponse
	if err := t.requestJSON(ctx, http.MethodPost, "/v1/jobs", req, &out); err != nil {
		return openHandsJobResponse{}, err
	}
	return out, nil
}

func (t *OpenHandsDelegateTool) waitForJob(ctx context.Context, jobID string, waitSec int) (openHandsJob, error) {
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for {
		var out openHandsJobResponse
		if err := t.requestJSON(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(jobID), nil, &out); err != nil {
			return openHandsJob{}, err
		}
		switch out.Job.Status {
		case "succeeded", "failed", "cancelled":
			return out.Job, nil
		}
		if time.Now().After(deadline) {
			return openHandsJob{}, fmt.Errorf("timeout waiting for job %s", jobID)
		}
		select {
		case <-ctx.Done():
			return openHandsJob{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (t *OpenHandsDelegateTool) requestJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	base := strings.TrimRight(t.config.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.config.BearerToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out != nil {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func formatOpenHandsResult(job openHandsJob, prepSummary string) string {
	var sb strings.Builder
	sb.WriteString("OpenHands job completed successfully.\n")
	sb.WriteString("- Job ID: " + job.ID + "\n")
	sb.WriteString("- Mode: " + job.Mode + "\n")
	sb.WriteString("- Workspace: " + prepSummary + "\n")
	if strings.TrimSpace(job.Summary) != "" {
		sb.WriteString("\nSummary:\n")
		sb.WriteString(job.Summary)
		sb.WriteString("\n")
	}
	if len(job.FilesChanged) > 0 {
		sb.WriteString("\nFiles changed:\n")
		for _, file := range job.FilesChanged {
			sb.WriteString("- " + file + "\n")
		}
	}
	if len(job.TestsRun) > 0 {
		sb.WriteString("\nTests run:\n")
		for _, item := range job.TestsRun {
			sb.WriteString("- " + item + "\n")
		}
	}
	if len(job.Risks) > 0 {
		sb.WriteString("\nRisks:\n")
		for _, item := range job.Risks {
			sb.WriteString("- " + item + "\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func ohBoolArg(args map[string]any, key string, def bool) bool {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case bool:
		return v
	default:
		return def
	}
}

func ohIntArg(args map[string]any, key string, def int) int {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case float64:
		if int(v) > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}
	return def
}

func ohStringArg(args map[string]any, key string) string {
	raw, _ := args[key].(string)
	return raw
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func isGitRepo(ctx context.Context, dir string) bool {
	_, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	return err == nil
}

func normalizeGitRemoteURL(raw string) string {
	out := strings.TrimSpace(raw)
	if out == "" {
		return out
	}
	if strings.HasPrefix(out, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(out, "git@"), ":", 2)
		if len(parts) == 2 {
			return "https://" + parts[0] + "/" + strings.TrimSuffix(parts[1], "/")
		}
	}
	if strings.HasPrefix(out, "ssh://git@") {
		trimmed := strings.TrimPrefix(out, "ssh://git@")
		if slash := strings.Index(trimmed, "/"); slash > 0 {
			return "https://" + trimmed[:slash] + "/" + trimmed[slash+1:]
		}
	}
	return out
}

func remoteContainsCommit(ctx context.Context, dir, commit string) bool {
	if strings.TrimSpace(commit) == "" {
		return false
	}
	out, err := gitOutput(ctx, dir, "for-each-ref", "--format=%(refname:short)", "--contains", commit, "refs/remotes/origin")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || ref == "origin/HEAD" {
			continue
		}
		return true
	}
	return false
}

func resolveFallbackRemoteRef(ctx context.Context, dir string) (string, error) {
	if out, err := gitOutput(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" && ref != "@{upstream}" {
			return ref, nil
		}
	}
	if out, err := gitOutput(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no fallback remote ref")
}

func repoURLAllowed(repoURL string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	normalized := strings.ToLower(strings.TrimRight(repoURL, "/"))
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, strings.ToLower(strings.TrimRight(prefix, "/"))) {
			return true
		}
	}
	return false
}

func sanitizeSubdir(value string) string {
	if value == "" || value == "." {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." {
		return ""
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "__invalid__"
	}
	return cleaned
}

func buildUntrackedTarball(root string, rawList string) ([]byte, error) {
	parts := strings.Split(rawList, "\x00")
	var files []string
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		files = append(files, filepath.Clean(part))
	}
	sort.Strings(files)
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for _, rel := range files {
		abs := filepath.Join(root, rel)
		info, err := os.Lstat(abs)
		if err != nil {
			return nil, err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, err
		}
		header.Name = filepath.ToSlash(rel)
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(abs)
			if err != nil {
				return nil, err
			}
			header.Linkname = target
		}
		if err := tw.WriteHeader(header); err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(abs)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(tw, file); err != nil {
				file.Close()
				return nil, err
			}
			file.Close()
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func shouldSkipSnapshotPath(rel string, isDir bool) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		switch part {
		case ".git", ".playwright-cli", "node_modules", ".venv", "venv", ".next", ".turbo", "coverage", "dist", "build":
			return true
		}
	}
	base := parts[len(parts)-1]
	if base == ".DS_Store" {
		return true
	}
	return false
}

func enforceUploadLimit(data []byte, maxUploadMB int) error {
	limit := maxUploadMB * 1024 * 1024
	if limit <= 0 {
		return nil
	}
	if len(data) > limit {
		return fmt.Errorf("workspace package exceeds upload limit of %d MB", maxUploadMB)
	}
	return nil
}
