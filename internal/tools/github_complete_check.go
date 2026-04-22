package tools

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// GitHubCompleteCheckTool closes a GitHub Checks-API "in progress" check
// run that was opened by an upstream service (cartridge-gg/internal)
// before the wake fired. The upstream service stashes `check_run_id` in
// the wake metadata; this tool reads it out of the wake metadata that
// lands in ctx via tools.WithWakeMetadata and PATCHes the check run to
// a terminal conclusion with optional inline annotations.
//
// Wire contract (matches `POST /agent/github/checks/{id}/complete` on
// cartridge-gg/internal, signed with HMAC-SHA-256 over the raw body):
//
//	Header: X-Cartridge-Signature-256: sha256=<hex>
//	Body:   {owner, repo, conclusion, title, summary, annotations[]}
//
// Failure posture: best-effort. If the tool can't reach the callback
// endpoint (bad config, network failure, upstream 5xx after retries),
// it returns an error result but the agent's review comment has already
// been posted independently. The only user-visible impact is that the
// "Code Review" check run stays as a yellow spinner instead of flipping
// to its terminal state.
type GitHubCompleteCheckTool struct {
	baseURL string
	secret  []byte
	// httpClient is injected for testability; nil ⇒ http.DefaultClient.
	httpClient *http.Client
}

// NewGitHubCompleteCheckTool constructs the tool. Callers wire config
// via SetCompletionEndpoint before registering on the tool registry.
func NewGitHubCompleteCheckTool() *GitHubCompleteCheckTool {
	return &GitHubCompleteCheckTool{}
}

// GitHubCompletionEndpointAware tools receive the callback endpoint
// config (base URL + shared HMAC secret).
type GitHubCompletionEndpointAware interface {
	SetCompletionEndpoint(baseURL, secret string)
}

// SetCompletionEndpoint wires the callback endpoint. baseURL should
// include the scheme (e.g. "https://api.cartridge.gg"). An empty baseURL
// or secret leaves the tool registered but inert — Execute returns a
// clear config error so the misconfiguration is obvious in logs.
func (t *GitHubCompleteCheckTool) SetCompletionEndpoint(baseURL, secret string) {
	t.baseURL = strings.TrimRight(baseURL, "/")
	t.secret = []byte(secret)
}

func (t *GitHubCompleteCheckTool) Name() string { return "github_complete_check" }

func (t *GitHubCompleteCheckTool) Description() string {
	return "Close the GitHub 'Code Review' check run opened by the upstream service for a PR review wake. " +
		"Call this as the final step of a /review invoked by an `agent-trigger-pr-review` wake — after posting " +
		"the review comment to the PR. The tool reads `check_run_id`, `repo_slug`, and PR identifiers from the " +
		"wake metadata automatically; it only errors out cleanly when those are absent (non-check-run contexts). " +
		"Choose `conclusion` based on the review's findings: `success` for a clean review, `neutral` for " +
		"informational-only findings, `action_required` when any critical/P1 findings are present, `failure` when " +
		"the PR as a whole should not land. Annotations (optional, up to 50) render as inline comments on the PR diff."
}

func (t *GitHubCompleteCheckTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"conclusion": map[string]any{
				"type":        "string",
				"description": "Terminal state for the check run. One of: success, failure, neutral, action_required, cancelled, timed_out.",
				"enum":        []string{"success", "failure", "neutral", "action_required", "cancelled", "timed_out"},
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Optional short heading shown in the PR Checks tab (e.g. \"Code review complete: 3 findings\").",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Optional markdown body shown under the heading. Keep it tight — the full review goes in the PR comment.",
			},
			"annotations": map[string]any{
				"type":        "array",
				"description": "Optional list of up to 50 inline comments to render on the PR diff. Levels: notice | warning | failure.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":             map[string]any{"type": "string", "description": "Repo-relative file path."},
						"start_line":       map[string]any{"type": "integer", "description": "First line of the annotation range (1-based)."},
						"end_line":         map[string]any{"type": "integer", "description": "Last line of the annotation range (1-based; equal to start_line for single-line comments)."},
						"annotation_level": map[string]any{"type": "string", "enum": []string{"notice", "warning", "failure"}},
						"title":            map[string]any{"type": "string", "description": "Optional short title for the annotation."},
						"message":          map[string]any{"type": "string", "description": "The annotation body."},
					},
					"required": []string{"path", "start_line", "end_line", "annotation_level", "message"},
				},
			},
		},
		"required": []string{"conclusion"},
	}
}

// conclusionAllowlist mirrors cartridge-gg/internal's isValidConclusion so
// we fail at the tool boundary with a clearer error than a 400 from the
// callback endpoint.
var conclusionAllowlist = map[string]bool{
	"success":         true,
	"failure":         true,
	"neutral":         true,
	"action_required": true,
	"cancelled":       true,
	"timed_out":       true,
}

type completeAnnotation struct {
	Path            string `json:"path"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	AnnotationLevel string `json:"annotation_level"`
	Title           string `json:"title,omitempty"`
	Message         string `json:"message"`
}

type completeRequest struct {
	Owner       string               `json:"owner"`
	Repo        string               `json:"repo"`
	Conclusion  string               `json:"conclusion"`
	Title       string               `json:"title,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Annotations []completeAnnotation `json:"annotations,omitempty"`
}

func (t *GitHubCompleteCheckTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.baseURL == "" || len(t.secret) == 0 {
		return ErrorResult("github_complete_check: not configured — cartridge_internal_base_url and cartridge_internal_webhook_secret must be set on the gateway")
	}

	md := WakeMetadataFromCtx(ctx)
	if md == nil {
		return ErrorResult("github_complete_check: no wake metadata in context — this tool is only valid during an external wake run (e.g. agent-trigger-pr-review)")
	}

	checkRunID, ok := readCheckRunID(md)
	if !ok {
		return ErrorResult("github_complete_check: wake metadata missing check_run_id — the upstream check-run creation probably failed on the webhook side")
	}

	repoSlug := argString(args, "repo_slug")
	if repoSlug == "" {
		repoSlug, _ = md["repo_slug"].(string)
	}
	owner, repo, ok := splitRepoSlug(repoSlug)
	if !ok {
		return ErrorResult(fmt.Sprintf("github_complete_check: invalid or missing repo_slug %q — expected owner/repo from wake metadata", repoSlug))
	}

	conclusion := strings.ToLower(strings.TrimSpace(argString(args, "conclusion")))
	if !conclusionAllowlist[conclusion] {
		return ErrorResult(fmt.Sprintf("github_complete_check: invalid conclusion %q — must be one of success|failure|neutral|action_required|cancelled|timed_out", conclusion))
	}

	anns, err := parseAnnotations(args["annotations"])
	if err != nil {
		return ErrorResult(fmt.Sprintf("github_complete_check: %v", err))
	}

	body := completeRequest{
		Owner:       owner,
		Repo:        repo,
		Conclusion:  conclusion,
		Title:       argString(args, "title"),
		Summary:     argString(args, "summary"),
		Annotations: anns,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		// Unreachable for this struct shape but surface cleanly if the
		// set of fields ever grows in a way that fails marshal.
		return ErrorResult(fmt.Sprintf("github_complete_check: marshal body: %v", err))
	}

	endpoint := fmt.Sprintf("%s/agent/github/checks/%d/complete", t.baseURL, checkRunID)

	// Detached context so an agent-ctx cancellation (client hangup, pipeline
	// timeout, user interrupt) right as the review finishes doesn't abort
	// the POST mid-flight and strand the check run `in_progress`. The 30s
	// ceiling bounds the longest this step can hold up the overall run.
	if err := ctx.Err(); err != nil {
		slog.Warn("github_complete_check: parent ctx already done — posting under detached ctx anyway",
			"check_run_id", checkRunID, "parent_err", err)
	}
	postCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = providers.RetryDo(postCtx, providers.DefaultRetryConfig(), func() (struct{}, error) {
		return struct{}{}, t.postOnce(postCtx, endpoint, raw)
	})
	if err != nil {
		slog.Warn("github_complete_check: POST failed after retries",
			"check_run_id", checkRunID, "endpoint", endpoint, "error", err)
		return ErrorResult(fmt.Sprintf("github_complete_check: POST %s failed: %v", endpoint, err))
	}

	slog.Info("github_complete_check: closed check run",
		"check_run_id", checkRunID, "conclusion", conclusion,
		"annotations", len(anns), "owner", owner, "repo", repo)
	return NewResult(fmt.Sprintf("Closed check run %d on %s/%s with conclusion=%s (%d annotation(s)).",
		checkRunID, owner, repo, conclusion, len(anns)))
}

// postOnce issues a single signed POST. Returns a typed HTTPError on
// non-2xx so providers.IsRetryableError picks up 429/5xx for retries.
func (t *GitHubCompleteCheckTool) postOnce(ctx context.Context, endpoint string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cartridge-Signature-256", signBody(body, t.secret))

	client := t.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return &providers.HTTPError{
		Status: resp.StatusCode,
		Body:   strings.TrimSpace(string(snippet)),
	}
}

// signBody returns the `sha256=<hex>` header value that cartridge-gg/internal's
// complete_handler expects. Matches the inbound webhook verify in
// internal/channels/pancake/webhook_handler.go, just computing not verifying.
func signBody(body, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// readCheckRunID extracts the numeric check_run_id from wake metadata.
// json.Unmarshal into `map[string]any` delivers JSON numbers as float64 by
// default, so the int cast has to handle that path explicitly. Also handles
// json.Number (used when a decoder has UseNumber enabled) and native int
// types (a test or programmatic caller might push those in directly).
func readCheckRunID(md map[string]any) (int64, bool) {
	raw, ok := md["check_run_id"]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		if v <= 0 || v != float64(int64(v)) {
			return 0, false
		}
		return int64(v), true
	case int:
		if v <= 0 {
			return 0, false
		}
		return int64(v), true
	case int64:
		if v <= 0 {
			return 0, false
		}
		return v, true
	case json.Number:
		n, err := v.Int64()
		if err != nil || n <= 0 {
			return 0, false
		}
		return n, true
	case string:
		// Defensive: some wake senders stringify IDs. Accept if parses to positive int.
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// splitRepoSlug parses "owner/repo". Rejects empty segments.
func splitRepoSlug(slug string) (owner, repo string, ok bool) {
	slug = strings.TrimSpace(slug)
	i := strings.IndexByte(slug, '/')
	if i <= 0 || i == len(slug)-1 {
		return "", "", false
	}
	owner, repo = slug[:i], slug[i+1:]
	if strings.ContainsAny(owner, "/ ") || strings.ContainsAny(repo, "/ ") {
		return "", "", false
	}
	return owner, repo, true
}

// parseAnnotations coerces the raw []any from json.Unmarshal into typed
// completeAnnotation. Missing required fields (path/start_line/end_line/
// annotation_level/message) return a clear error so the LLM can retry with
// a correct payload instead of sending a request the upstream will 400.
func parseAnnotations(raw any) ([]completeAnnotation, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, errors.New("annotations must be an array")
	}
	out := make([]completeAnnotation, 0, len(list))
	for i, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("annotations[%d]: must be an object", i)
		}
		var ann completeAnnotation
		ann.Path = strings.TrimSpace(argString(obj, "path"))
		ann.AnnotationLevel = strings.ToLower(strings.TrimSpace(argString(obj, "annotation_level")))
		ann.Title = argString(obj, "title")
		ann.Message = argString(obj, "message")

		if ann.Path == "" {
			return nil, fmt.Errorf("annotations[%d]: path is required", i)
		}
		if ann.Message == "" {
			return nil, fmt.Errorf("annotations[%d]: message is required", i)
		}
		switch ann.AnnotationLevel {
		case "notice", "warning", "failure":
		default:
			return nil, fmt.Errorf("annotations[%d]: annotation_level must be notice|warning|failure (got %q)", i, ann.AnnotationLevel)
		}
		start, ok := readAnnotationLine(obj, "start_line")
		if !ok {
			return nil, fmt.Errorf("annotations[%d]: start_line must be a positive integer", i)
		}
		end, ok := readAnnotationLine(obj, "end_line")
		if !ok {
			return nil, fmt.Errorf("annotations[%d]: end_line must be a positive integer", i)
		}
		if end < start {
			return nil, fmt.Errorf("annotations[%d]: end_line (%d) must be >= start_line (%d)", i, end, start)
		}
		ann.StartLine = start
		ann.EndLine = end
		out = append(out, ann)
	}
	return out, nil
}

func readAnnotationLine(obj map[string]any, key string) (int, bool) {
	v, ok := obj[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		if n <= 0 || n != float64(int64(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		if n <= 0 {
			return 0, false
		}
		return n, true
	case int64:
		if n <= 0 {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i <= 0 {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}
