package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	githubAPIBaseURL      = "https://api.github.com"
	githubUserAgent       = "GoClaw-GitHubTool/1.0"
	githubRequestTimeout  = 30 * time.Second
	githubDefaultMaxChars = 40000
	githubDefaultLimit    = 20
	githubMaxLimit        = 100
)

// GitHubToolConfig configures the GitHub read-only tool.
type GitHubToolConfig struct {
	Token string
}

// GitHubTool reads GitHub repositories, files, issues, pull requests, and releases.
type GitHubTool struct {
	mu      sync.RWMutex
	token   string
	apiBase string
	client  *http.Client
	maxChars int
}

// NewGitHubTool creates a GitHub tool backed by the GitHub REST API.
func NewGitHubTool(cfg GitHubToolConfig) *GitHubTool {
	return &GitHubTool{
		token:    cfg.Token,
		apiBase:  githubAPIBaseURL,
		client:   &http.Client{Timeout: githubRequestTimeout},
		maxChars: githubDefaultMaxChars,
	}
}

// UpdateToken swaps the bearer token used for subsequent requests.
func (t *GitHubTool) UpdateToken(token string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.token = token
}

func (t *GitHubTool) Name() string { return "github_read" }

func (t *GitHubTool) Description() string {
	return "Read GitHub repositories, files, issues, pull requests, and releases via the GitHub REST API."
}

func (t *GitHubTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"repo": map[string]any{
				"type":        "string",
				"description": "Repository in owner/repo form, or a GitHub URL such as https://github.com/owner/repo/blob/main/path.go. If omitted, the tool tries to infer the current workspace origin remote.",
			},
			"kind": map[string]any{
				"type": "string",
				"enum": []string{"repo", "file", "tree", "issue", "pull", "release", "releases"},
				"description": "Resource type to read. Optional — inferred from the GitHub URL path or other arguments when omitted.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path within the repository for file/tree reads.",
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "Branch, tag, or commit ref for file/tree reads.",
			},
			"number": map[string]any{
				"type":        "number",
				"description": "Issue or pull request number.",
				"minimum":     1.0,
			},
			"tag": map[string]any{
				"type":        "string",
				"description": "Release tag for release reads.",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Maximum number of entries to return for tree/release lists.",
				"minimum":     1.0,
				"maximum":     float64(githubMaxLimit),
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "When reading a tree, walk nested directories recursively.",
			},
			"maxChars": map[string]any{
				"type":        "number",
				"description": "Maximum number of characters to return after formatting.",
				"minimum":     1.0,
			},
		},
	}
}

func (t *GitHubTool) Execute(ctx context.Context, args map[string]any) *Result {
	req, source, err := t.resolveRequest(ctx, args)
	if err != nil {
		return ErrorResult("github_read: " + err.Error())
	}

	content, err := t.readResource(ctx, req)
	if err != nil {
		return ErrorResult("github_read: " + err.Error())
	}

	content = truncateRunes(content, req.MaxChars)
	return NewResult(wrapExternalContent(content, source, true))
}

type githubReadRequest struct {
	Owner     string
	Repo      string
	Kind      string
	Path      string
	Ref       string
	Tag       string
	Number    int
	Limit     int
	Recursive bool
	MaxChars  int
}

type githubTargetSpec struct {
	Owner  string
	Repo   string
	Kind   string
	Path   string
	Ref    string
	Tag    string
	Number int
}

func (t *GitHubTool) resolveRequest(ctx context.Context, args map[string]any) (githubReadRequest, string, error) {
	_, hasRecursiveArg := args["recursive"]
	req := githubReadRequest{
		Kind:      normalizeGitHubKind(stringArg(args, "kind")),
		Path:      strings.TrimSpace(stringArg(args, "path")),
		Ref:       strings.TrimSpace(stringArg(args, "ref")),
		Tag:       strings.TrimSpace(stringArg(args, "tag")),
		Number:    intArgAny(args, "number", 0),
		Limit:     intArgAny(args, "limit", 0),
		Recursive: boolArgAny(args, "recursive", false),
		MaxChars:  intArgAny(args, "maxChars", 0),
	}
	if req.MaxChars <= 0 {
		req.MaxChars = t.defaultMaxChars()
	}
	if req.Limit <= 0 {
		req.Limit = githubDefaultLimit
	}
	if req.Limit > githubMaxLimit {
		req.Limit = githubMaxLimit
	}

	repoSource := strings.TrimSpace(stringArg(args, "repo"))
	if repoSource == "" {
		inferredRepo, err := inferGitHubRepoSpec(ctx)
		if err != nil {
			return githubReadRequest{}, "", err
		}
		repoSource = inferredRepo
	}

	target, ok := parseGitHubTargetSpec(repoSource)
	if !ok {
		owner, repo, err := parseGitHubRepoSpec(repoSource)
		if err != nil {
			return githubReadRequest{}, "", err
		}
		target.Owner = owner
		target.Repo = repo
	}

	if target.Kind != "" && req.Kind == "" {
		req.Kind = target.Kind
	}
	if target.Path != "" && req.Path == "" {
		req.Path = target.Path
	}
	if target.Ref != "" && req.Ref == "" {
		req.Ref = target.Ref
	}
	if target.Tag != "" && req.Tag == "" {
		req.Tag = target.Tag
	}
	if target.Number > 0 && req.Number <= 0 {
		req.Number = target.Number
	}
	req.Owner = target.Owner
	req.Repo = target.Repo

	if req.Kind == "" {
		switch {
		case req.Tag != "":
			req.Kind = "release"
		case req.Number > 0:
			req.Kind = "issue"
		case req.Path != "":
			req.Kind = "file"
		default:
			req.Kind = "repo"
		}
	}

	if req.Kind == "tree" && !hasRecursiveArg {
		req.Recursive = true
	}

	if req.Owner == "" || req.Repo == "" {
		return githubReadRequest{}, "", fmt.Errorf("repo is required")
	}

	if req.Kind == "releases" && req.Limit <= 0 {
		req.Limit = githubDefaultLimit
	}

	return req, req.sourceLabel(), nil
}

func (r githubReadRequest) sourceLabel() string {
	base := fmt.Sprintf("GitHub %s/%s", r.Owner, r.Repo)
	switch r.Kind {
	case "file":
		if r.Path != "" {
			return base + ":" + r.Path
		}
		return base + " repository contents"
	case "tree":
		if r.Path != "" {
			return base + ":" + r.Path + " tree"
		}
		return base + " tree"
	case "issue":
		if r.Number > 0 {
			return fmt.Sprintf("%s issue #%d", base, r.Number)
		}
		return base + " issue"
	case "pull":
		if r.Number > 0 {
			return fmt.Sprintf("%s pull request #%d", base, r.Number)
		}
		return base + " pull request"
	case "release":
		if r.Tag != "" {
			return fmt.Sprintf("%s release %s", base, r.Tag)
		}
		return base + " latest release"
	case "releases":
		return base + " releases"
	default:
		return base + " repository"
	}
}

func normalizeGitHubKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "auto":
		return ""
	case "repo", "repository":
		return "repo"
	case "file", "files", "contents", "content":
		return "file"
	case "tree", "dir", "directory":
		return "tree"
	case "issue", "issues":
		return "issue"
	case "pull", "pr", "pull_request", "pull-request", "pulls":
		return "pull"
	case "release", "tag":
		return "release"
	case "releases":
		return "releases"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func parseGitHubTargetSpec(spec string) (githubTargetSpec, bool) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return githubTargetSpec{}, false
	}
	if idx := strings.IndexAny(s, "?#"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSuffix(s, ".git")

	lower := strings.ToLower(s)
	if idx := strings.Index(lower, "github.com/"); idx >= 0 {
		s = s[idx+len("github.com/") :]
	} else if idx := strings.Index(lower, "github.com:"); idx >= 0 {
		s = s[idx+len("github.com:") :]
	}
	s = strings.TrimPrefix(s, "/")
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 {
		return githubTargetSpec{}, false
	}

	target := githubTargetSpec{
		Owner: urlUnescapeSegment(parts[0]),
		Repo:  urlUnescapeSegment(parts[1]),
	}
	if len(parts) < 3 {
		return target, true
	}

	switch parts[2] {
	case "blob":
		if len(parts) >= 5 {
			target.Kind = "file"
			target.Ref = urlUnescapeSegment(parts[3])
			target.Path = joinUnescapedPath(parts[4:])
		}
	case "tree":
		if len(parts) >= 4 {
			target.Kind = "tree"
			target.Ref = urlUnescapeSegment(parts[3])
			if len(parts) > 4 {
				target.Path = joinUnescapedPath(parts[4:])
			}
		}
	case "issues":
		if len(parts) >= 4 {
			target.Kind = "issue"
			target.Number = mustAtoi(urlUnescapeSegment(parts[3]))
		}
	case "pull", "pulls":
		if len(parts) >= 4 {
			target.Kind = "pull"
			target.Number = mustAtoi(urlUnescapeSegment(parts[3]))
		}
	case "releases":
		if len(parts) == 3 {
			target.Kind = "releases"
			return target, true
		}
		if len(parts) >= 5 && parts[3] == "tag" {
			target.Kind = "release"
			target.Tag = joinUnescapedPath(parts[4:])
		}
	}

	return target, true
}

func parseGitHubRepoSpec(spec string) (owner, repo string, err error) {
	target, ok := parseGitHubTargetSpec(spec)
	if !ok {
		return "", "", fmt.Errorf("invalid GitHub repository %q", spec)
	}
	return target.Owner, target.Repo, nil
}

func inferGitHubRepoSpec(ctx context.Context) (string, error) {
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		return "", fmt.Errorf("repo is required when no workspace is available")
	}

	for _, args := range [][]string{
		{"-C", workspace, "remote", "get-url", "origin"},
		{"-C", workspace, "config", "--get", "remote.origin.url"},
	} {
		cmd := exec.Command("git", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		remote := strings.TrimSpace(string(out))
		if remote != "" {
			return remote, nil
		}
	}

	return "", fmt.Errorf("unable to infer GitHub repository from workspace %q", workspace)
}

func urlUnescapeSegment(value string) string {
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}

func joinUnescapedPath(segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		parts = append(parts, urlUnescapeSegment(segment))
	}
	return strings.Join(parts, "/")
}

func mustAtoi(value string) int {
	if value == "" {
		return 0
	}
	n, _ := strconv.Atoi(value)
	return n
}

func stringArg(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func intArgAny(args map[string]any, key string, fallback int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return fallback
}

func boolArgAny(args map[string]any, key string, fallback bool) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}

func (t *GitHubTool) defaultMaxChars() int {
	if t == nil || t.maxChars <= 0 {
		return githubDefaultMaxChars
	}
	return t.maxChars
}

func (t *GitHubTool) clientOrDefault() *http.Client {
	if t != nil && t.client != nil {
		return t.client
	}
	return http.DefaultClient
}

func (t *GitHubTool) apiBaseURL() string {
	if t == nil || strings.TrimSpace(t.apiBase) == "" {
		return githubAPIBaseURL
	}
	return strings.TrimRight(t.apiBase, "/")
}

func (t *GitHubTool) tokenValue() string {
	if t == nil {
		return ""
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.token
}

func (t *GitHubTool) endpoint(path string, query url.Values) string {
	base := t.apiBaseURL()
	full := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	if query != nil {
		if encoded := query.Encode(); encoded != "" {
			full += "?" + encoded
		}
	}
	return full
}

func (t *GitHubTool) doRequest(ctx context.Context, method, path string, query url.Values) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, t.endpoint(path, query), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", githubUserAgent)
	if token := t.tokenValue(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := t.clientOrDefault().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, t.apiError(resp, body)
	}
	return body, resp.Header, nil
}

func (t *GitHubTool) apiError(resp *http.Response, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = resp.Status
	}
	msg = truncateStr(msg, 500)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub authentication failed: %s", msg)
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
				if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
					return fmt.Errorf("GitHub API rate limit exceeded until %s: %s", time.Unix(ts, 0).UTC().Format(time.RFC3339), msg)
				}
			}
			return fmt.Errorf("GitHub API rate limit exceeded: %s", msg)
		}
		return fmt.Errorf("GitHub API forbidden: %s", msg)
	case http.StatusNotFound:
		return fmt.Errorf("GitHub resource not found: %s", msg)
	default:
		path := ""
		if resp.Request != nil && resp.Request.URL != nil {
			path = resp.Request.URL.Path
		}
		if path != "" {
			return fmt.Errorf("GitHub API %s %s returned %d: %s", resp.Request.Method, path, resp.StatusCode, msg)
		}
		return fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, msg)
	}
}

type githubRepo struct {
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Visibility    string `json:"visibility"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	Stars         int    `json:"stargazers_count"`
	Forks         int    `json:"forks_count"`
	OpenIssues    int    `json:"open_issues_count"`
	Language      string `json:"language"`
	Homepage      string `json:"homepage"`
	UpdatedAt     string `json:"updated_at"`
	License       *struct {
		SPDXID string `json:"spdx_id"`
		Name   string `json:"name"`
	} `json:"license"`
}

type githubContentEntry struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	SHA         string `json:"sha"`
	Size        int    `json:"size"`
	HTMLURL     string `json:"html_url"`
	DownloadURL string `json:"download_url"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
	Truncated   bool   `json:"truncated"`
	URL         string `json:"url"`
	Submodule   string `json:"submodule_git_url,omitempty"`
	Target      string `json:"target,omitempty"`
}

type githubBlob struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int    `json:"size"`
	SHA      string `json:"sha"`
}

type githubIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Locked      bool   `json:"locked"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	Comments    int    `json:"comments"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	ClosedAt    string `json:"closed_at"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

type githubPullRequest struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	State         string `json:"state"`
	Draft         bool   `json:"draft"`
	Locked        bool   `json:"locked"`
	Merged        bool   `json:"merged"`
	HTMLURL       string `json:"html_url"`
	Body          string `json:"body"`
	Comments      int    `json:"comments"`
	ReviewComments int   `json:"review_comments"`
	Commits       int    `json:"commits"`
	Additions     int    `json:"additions"`
	Deletions     int    `json:"deletions"`
	ChangedFiles  int    `json:"changed_files"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	ClosedAt      string `json:"closed_at"`
	MergedAt      string `json:"merged_at"`
	User          struct {
		Login string `json:"login"`
	} `json:"user"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	CreatedAt   string `json:"created_at"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

func (t *GitHubTool) readResource(ctx context.Context, req githubReadRequest) (string, error) {
	switch req.Kind {
	case "repo":
		repo, err := t.getRepo(ctx, req.Owner, req.Repo)
		if err != nil {
			return "", err
		}
		return formatGitHubRepo(req, repo), nil
	case "file":
		return t.readFileOrDirectory(ctx, req, false)
	case "tree":
		return t.readFileOrDirectory(ctx, req, true)
	case "issue":
		issue, err := t.getIssue(ctx, req.Owner, req.Repo, req.Number)
		if err != nil {
			return "", err
		}
		return formatGitHubIssue(req, issue), nil
	case "pull":
		pull, err := t.getPull(ctx, req.Owner, req.Repo, req.Number)
		if err != nil {
			return "", err
		}
		return formatGitHubPull(req, pull), nil
	case "release":
		release, err := t.getRelease(ctx, req.Owner, req.Repo, req.Tag)
		if err != nil {
			return "", err
		}
		return formatGitHubRelease(req, release), nil
	case "releases":
		releases, err := t.listReleases(ctx, req.Owner, req.Repo, req.Limit)
		if err != nil {
			return "", err
		}
		return formatGitHubReleases(req, releases), nil
	default:
		return "", fmt.Errorf("unsupported kind %q", req.Kind)
	}
}

func (t *GitHubTool) readFileOrDirectory(ctx context.Context, req githubReadRequest, recursive bool) (string, error) {
	result, err := t.getContents(ctx, req.Owner, req.Repo, req.Path, req.Ref)
	if err != nil {
		return "", err
	}
	if result.kind == "file" {
		return formatGitHubFile(req, result.file), nil
	}
	if recursive {
		entries, err := t.collectTree(ctx, req.Owner, req.Repo, req.Path, req.Ref, req.Limit)
		if err != nil {
			return "", err
		}
		return formatGitHubTree(req, entries, true), nil
	}
	return formatGitHubDirectory(req, result.entries), nil
}

func (t *GitHubTool) getRepo(ctx context.Context, owner, repo string) (*githubRepo, error) {
	var out githubRepo
	if err := t.getJSON(ctx, fmt.Sprintf("/repos/%s/%s", escapePathSegment(owner), escapePathSegment(repo)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (t *GitHubTool) getIssue(ctx context.Context, owner, repo string, number int) (*githubIssue, error) {
	if number <= 0 {
		return nil, fmt.Errorf("issue number is required")
	}
	var out githubIssue
	if err := t.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d", escapePathSegment(owner), escapePathSegment(repo), number), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (t *GitHubTool) getPull(ctx context.Context, owner, repo string, number int) (*githubPullRequest, error) {
	if number <= 0 {
		return nil, fmt.Errorf("pull request number is required")
	}
	var out githubPullRequest
	if err := t.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", escapePathSegment(owner), escapePathSegment(repo), number), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (t *GitHubTool) getRelease(ctx context.Context, owner, repo, tag string) (*githubRelease, error) {
	var out githubRelease
	endpoint := fmt.Sprintf("/repos/%s/%s/releases/latest", escapePathSegment(owner), escapePathSegment(repo))
	if tag != "" {
		endpoint = fmt.Sprintf("/repos/%s/%s/releases/tags/%s", escapePathSegment(owner), escapePathSegment(repo), escapePathSegment(tag))
	}
	if err := t.getJSON(ctx, endpoint, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (t *GitHubTool) listReleases(ctx context.Context, owner, repo string, limit int) ([]githubRelease, error) {
	if limit <= 0 {
		limit = githubDefaultLimit
	}
	if limit > githubMaxLimit {
		limit = githubMaxLimit
	}
	query := url.Values{}
	query.Set("per_page", strconv.Itoa(limit))
	var out []githubRelease
	if err := t.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/releases", escapePathSegment(owner), escapePathSegment(repo)), query, &out); err != nil {
		return nil, err
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type githubContentsResult struct {
	kind    string
	file    *githubContentEntry
	entries []githubContentEntry
}

func (t *GitHubTool) getContents(ctx context.Context, owner, repo, repoPath, ref string) (githubContentsResult, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/contents", escapePathSegment(owner), escapePathSegment(repo))
	if repoPath != "" {
		endpoint += "/" + escapePath(repoPath)
	}
	query := url.Values{}
	if ref != "" {
		query.Set("ref", ref)
	}
	body, _, err := t.doRequest(ctx, http.MethodGet, endpoint, query)
	if err != nil {
		return githubContentsResult{}, err
	}
	if len(body) == 0 {
		return githubContentsResult{}, fmt.Errorf("GitHub contents response was empty")
	}
	if body[0] == '[' {
		var entries []githubContentEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			return githubContentsResult{}, err
		}
		sortGitHubEntries(entries)
		return githubContentsResult{kind: "dir", entries: entries}, nil
	}

	var entry githubContentEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		return githubContentsResult{}, err
	}
	if entry.Type == "dir" {
		return githubContentsResult{kind: "dir", entries: []githubContentEntry{entry}}, nil
	}

	text, binary, err := decodeGitHubContent(entry.Content, entry.Encoding)
	if err != nil {
		return githubContentsResult{}, err
	}
	if (text == "" || entry.Truncated) && entry.SHA != "" {
		if blobText, blobBinary, blobErr := t.getBlobText(ctx, owner, repo, entry.SHA); blobErr == nil {
			text = blobText
			binary = blobBinary
		} else if text == "" {
			return githubContentsResult{}, blobErr
		}
	}
	entry.Content = text
	if binary {
		entry.Content = ""
	}
	return githubContentsResult{kind: "file", file: &entry}, nil
}

func (t *GitHubTool) getBlobText(ctx context.Context, owner, repo, sha string) (string, bool, error) {
	var blob githubBlob
	if err := t.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/git/blobs/%s", escapePathSegment(owner), escapePathSegment(repo), escapePathSegment(sha)), nil, &blob); err != nil {
		return "", false, err
	}
	text, binary, err := decodeGitHubContent(blob.Content, blob.Encoding)
	if err != nil {
		return "", false, err
	}
	return text, binary, nil
}

func (t *GitHubTool) collectTree(ctx context.Context, owner, repo, repoPath, ref string, limit int) ([]githubContentEntry, error) {
	result, err := t.getContents(ctx, owner, repo, repoPath, ref)
	if err != nil {
		return nil, err
	}
	if result.kind == "file" {
		return []githubContentEntry{*result.file}, nil
	}

	var entries []githubContentEntry
	stack := append([]githubContentEntry(nil), result.entries...)
	for len(stack) > 0 {
		entry := stack[0]
		stack = stack[1:]
		entries = append(entries, entry)
		if limit > 0 && len(entries) >= limit {
			break
		}
		if entry.Type != "dir" {
			continue
		}
		nested, err := t.getContents(ctx, owner, repo, entry.Path, ref)
		if err != nil {
			return nil, err
		}
		if nested.kind != "dir" {
			continue
		}
		stack = append(nested.entries, stack...)
		sortGitHubEntries(stack)
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (t *GitHubTool) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	body, _, err := t.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func decodeGitHubContent(content, encoding string) (string, bool, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return "", false, nil
	}
	if encoding != "" && !strings.EqualFold(encoding, "base64") {
		return text, false, nil
	}
	cleaned := strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(text)
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return text, false, nil
	}
	if !utf8.Valid(decoded) {
		return "", true, nil
	}
	return string(decoded), false, nil
}

func escapePathSegment(value string) string {
	return url.PathEscape(value)
}

func escapePath(repoPath string) string {
	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func sortGitHubEntries(entries []githubContentEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path == entries[j].Path {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].Path < entries[j].Path
	})
}

func formatGitHubRepo(req githubReadRequest, repo *githubRepo) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", coalesce(repo.FullName, req.Owner+"/"+req.Repo))
	writeKV(&sb, "Kind", "repo")
	writeKV(&sb, "Default branch", coalesce(repo.DefaultBranch, "(unknown)"))
	writeKV(&sb, "Visibility", visibilityLabel(repo))
	writeKV(&sb, "Archived", boolLabel(repo.Archived, "yes", "no"))
	writeKV(&sb, "Fork", boolLabel(repo.Fork, "yes", "no"))
	writeKV(&sb, "Stars", strconv.Itoa(repo.Stars))
	writeKV(&sb, "Forks", strconv.Itoa(repo.Forks))
	writeKV(&sb, "Open issues", strconv.Itoa(repo.OpenIssues))
	writeKV(&sb, "Language", coalesce(repo.Language, "(unknown)"))
	writeKV(&sb, "Updated at", coalesce(repo.UpdatedAt, "(unknown)"))
	if repo.Homepage != "" {
		writeKV(&sb, "Homepage", repo.Homepage)
	}
	if repo.License != nil {
		license := repo.License.Name
		if repo.License.SPDXID != "" {
			license += " (" + repo.License.SPDXID + ")"
		}
		writeKV(&sb, "License", license)
	}
	writeKV(&sb, "URL", repo.HTMLURL)
	if repo.Description != "" {
		writeKV(&sb, "Description", repo.Description)
	}
	return sb.String()
}

func formatGitHubFile(req githubReadRequest, entry *githubContentEntry) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", req.Owner+"/"+req.Repo)
	writeKV(&sb, "Kind", "file")
	writeKV(&sb, "Path", coalesce(entry.Path, req.Path))
	if req.Ref != "" {
		writeKV(&sb, "Ref", req.Ref)
	}
	writeKV(&sb, "Type", coalesce(entry.Type, "file"))
	if entry.Size > 0 {
		writeKV(&sb, "Size", fmt.Sprintf("%d bytes", entry.Size))
	}
	if entry.SHA != "" {
		writeKV(&sb, "SHA", entry.SHA)
	}
	if entry.HTMLURL != "" {
		writeKV(&sb, "URL", entry.HTMLURL)
	}
	if entry.DownloadURL != "" {
		writeKV(&sb, "Download URL", entry.DownloadURL)
	}
	if entry.Content != "" {
		sb.WriteString("Content:\n")
		sb.WriteString(entry.Content)
		sb.WriteString("\n")
	} else {
		writeKV(&sb, "Content", "(binary or empty file)")
	}
	return sb.String()
}

func formatGitHubDirectory(req githubReadRequest, entries []githubContentEntry) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", req.Owner+"/"+req.Repo)
	writeKV(&sb, "Kind", "directory")
	writeKV(&sb, "Path", dirLabel(req.Path))
	if req.Ref != "" {
		writeKV(&sb, "Ref", req.Ref)
	}
	writeKV(&sb, "Entries", strconv.Itoa(len(entries)))
	for _, entry := range entries {
		sb.WriteString("- ")
		sb.WriteString(entry.Path)
		sb.WriteString(" [")
		sb.WriteString(entry.Type)
		sb.WriteString("]")
		if entry.Size > 0 {
			sb.WriteString(" ")
			sb.WriteString(fmt.Sprintf("%d bytes", entry.Size))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatGitHubTree(req githubReadRequest, entries []githubContentEntry, recursive bool) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", req.Owner+"/"+req.Repo)
	writeKV(&sb, "Kind", "tree")
	writeKV(&sb, "Path", dirLabel(req.Path))
	if req.Ref != "" {
		writeKV(&sb, "Ref", req.Ref)
	}
	writeKV(&sb, "Recursive", boolLabel(recursive, "yes", "no"))
	writeKV(&sb, "Entries", strconv.Itoa(len(entries)))
	for _, entry := range entries {
		indent := strings.Count(entry.Path, "/")
		if indent < 0 {
			indent = 0
		}
		sb.WriteString(strings.Repeat("  ", indent))
		sb.WriteString("- ")
		sb.WriteString(entry.Path)
		sb.WriteString(" [")
		sb.WriteString(entry.Type)
		sb.WriteString("]")
		if entry.Size > 0 {
			sb.WriteString(" ")
			sb.WriteString(fmt.Sprintf("%d bytes", entry.Size))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatGitHubIssue(req githubReadRequest, issue *githubIssue) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", req.Owner+"/"+req.Repo)
	writeKV(&sb, "Kind", "issue")
	writeKV(&sb, "Number", strconv.Itoa(issue.Number))
	writeKV(&sb, "Title", issue.Title)
	writeKV(&sb, "State", issue.State)
	writeKV(&sb, "Locked", boolLabel(issue.Locked, "yes", "no"))
	writeKV(&sb, "Author", issue.User.Login)
	writeKV(&sb, "Comments", strconv.Itoa(issue.Comments))
	writeKV(&sb, "Created at", issue.CreatedAt)
	writeKV(&sb, "Updated at", issue.UpdatedAt)
	if issue.ClosedAt != "" {
		writeKV(&sb, "Closed at", issue.ClosedAt)
	}
	if issue.Milestone != nil && issue.Milestone.Title != "" {
		writeKV(&sb, "Milestone", issue.Milestone.Title)
	}
	if len(issue.Labels) > 0 {
		labels := make([]string, 0, len(issue.Labels))
		for _, label := range issue.Labels {
			labels = append(labels, label.Name)
		}
		writeKV(&sb, "Labels", strings.Join(labels, ", "))
	}
	if len(issue.Assignees) > 0 {
		assignees := make([]string, 0, len(issue.Assignees))
		for _, assignee := range issue.Assignees {
			assignees = append(assignees, assignee.Login)
		}
		writeKV(&sb, "Assignees", strings.Join(assignees, ", "))
	}
	writeKV(&sb, "URL", issue.HTMLURL)
	if issue.Body != "" {
		sb.WriteString("Body:\n")
		sb.WriteString(issue.Body)
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatGitHubPull(req githubReadRequest, pull *githubPullRequest) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", req.Owner+"/"+req.Repo)
	writeKV(&sb, "Kind", "pull")
	writeKV(&sb, "Number", strconv.Itoa(pull.Number))
	writeKV(&sb, "Title", pull.Title)
	writeKV(&sb, "State", pull.State)
	writeKV(&sb, "Draft", boolLabel(pull.Draft, "yes", "no"))
	writeKV(&sb, "Locked", boolLabel(pull.Locked, "yes", "no"))
	writeKV(&sb, "Merged", boolLabel(pull.Merged, "yes", "no"))
	writeKV(&sb, "Author", pull.User.Login)
	writeKV(&sb, "Base", pull.Base.Ref)
	writeKV(&sb, "Head", pull.Head.Ref)
	writeKV(&sb, "Commits", strconv.Itoa(pull.Commits))
	writeKV(&sb, "Changed files", strconv.Itoa(pull.ChangedFiles))
	writeKV(&sb, "Additions", strconv.Itoa(pull.Additions))
	writeKV(&sb, "Deletions", strconv.Itoa(pull.Deletions))
	writeKV(&sb, "Comments", strconv.Itoa(pull.Comments))
	writeKV(&sb, "Review comments", strconv.Itoa(pull.ReviewComments))
	writeKV(&sb, "Created at", pull.CreatedAt)
	writeKV(&sb, "Updated at", pull.UpdatedAt)
	if pull.ClosedAt != "" {
		writeKV(&sb, "Closed at", pull.ClosedAt)
	}
	if pull.MergedAt != "" {
		writeKV(&sb, "Merged at", pull.MergedAt)
	}
	writeKV(&sb, "URL", pull.HTMLURL)
	if pull.Body != "" {
		sb.WriteString("Body:\n")
		sb.WriteString(pull.Body)
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatGitHubRelease(req githubReadRequest, release *githubRelease) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", req.Owner+"/"+req.Repo)
	writeKV(&sb, "Kind", "release")
	writeKV(&sb, "Tag", release.TagName)
	writeKV(&sb, "Name", release.Name)
	writeKV(&sb, "Draft", boolLabel(release.Draft, "yes", "no"))
	writeKV(&sb, "Prerelease", boolLabel(release.Prerelease, "yes", "no"))
	writeKV(&sb, "Author", release.Author.Login)
	writeKV(&sb, "Published at", release.PublishedAt)
	writeKV(&sb, "Created at", release.CreatedAt)
	writeKV(&sb, "URL", release.HTMLURL)
	if release.Body != "" {
		sb.WriteString("Body:\n")
		sb.WriteString(release.Body)
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatGitHubReleases(req githubReadRequest, releases []githubRelease) string {
	var sb strings.Builder
	writeKV(&sb, "Repository", req.Owner+"/"+req.Repo)
	writeKV(&sb, "Kind", "releases")
	writeKV(&sb, "Limit", strconv.Itoa(req.Limit))
	writeKV(&sb, "Entries", strconv.Itoa(len(releases)))
	for _, release := range releases {
		sb.WriteString("- ")
		sb.WriteString(coalesce(release.TagName, "(untagged)"))
		if release.Name != "" {
			sb.WriteString(" — ")
			sb.WriteString(release.Name)
		}
		sb.WriteString(" [")
		if release.Draft {
			sb.WriteString("draft")
		} else if release.Prerelease {
			sb.WriteString("prerelease")
		} else {
			sb.WriteString("release")
		}
		sb.WriteString("]")
		if release.PublishedAt != "" {
			sb.WriteString(" ")
			sb.WriteString(release.PublishedAt)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func writeKV(sb *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	sb.WriteString(key)
	sb.WriteString(": ")
	sb.WriteString(value)
	sb.WriteString("\n")
}

func coalesce(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func visibilityLabel(repo *githubRepo) string {
	if repo.Private {
		return "private"
	}
	if repo.Visibility != "" {
		return repo.Visibility
	}
	return "public"
}

func dirLabel(path string) string {
	if path == "" {
		return "."
	}
	return path
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 40 {
		return string(runes[:max])
	}
	note := fmt.Sprintf("\n\n[Output truncated to %d characters]", max)
	noteRunes := []rune(note)
	if max <= len(noteRunes) {
		return string(runes[:max])
	}
	cut := max - len(noteRunes)
	return string(runes[:cut]) + note
}
