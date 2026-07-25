package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CheckIntegrationTool lets the agent accurately report which third-party
// integrations (GitHub, Gmail, Slack, …) the user has connected, by querying
// composio-mcp's /accounts endpoint. It exists so the agent stops *guessing*
// connection status (e.g. concluding "GitHub isn't connected" just because a
// shell command failed).
type CheckIntegrationTool struct {
	composioURL string
	http        *http.Client
}

func NewCheckIntegrationTool(composioURL string) *CheckIntegrationTool {
	return &CheckIntegrationTool{
		composioURL: strings.TrimRight(composioURL, "/"),
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *CheckIntegrationTool) Name() string { return "check_integration" }

func (t *CheckIntegrationTool) Description() string {
	return "Check which third-party integrations the user has connected (GitHub, Gmail, Google Calendar/Drive/Docs/Sheets, Slack, Notion, Outlook, Salesforce, LinkedIn, X/Twitter). Use this whenever the user asks whether an integration is connected, or BEFORE you try to use one — never guess connection status. Pass `provider` to check one, or omit it to list everything connected."
}

func (t *CheckIntegrationTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"provider": map[string]any{
				"type":        "string",
				"description": "Which integration to check, e.g. \"github\", \"gmail\", \"google calendar\", \"slack\". Omit to list all connected integrations.",
			},
		},
	}
}

// toolkitAliases maps common names → composio toolkit slugs.
var toolkitAliases = map[string]string{
	"github": "github", "gmail": "gmail", "slack": "slack", "notion": "notion",
	"salesforce": "salesforce", "outlook": "outlook", "linkedin": "linkedin",
	"twitter": "twitter", "x": "twitter", "x/twitter": "twitter",
	"google calendar": "googlecalendar", "googlecalendar": "googlecalendar", "calendar": "googlecalendar", "gcal": "googlecalendar",
	"google drive": "googledrive", "googledrive": "googledrive", "drive": "googledrive", "gdrive": "googledrive",
	"google sheets": "googlesheets", "googlesheets": "googlesheets", "sheets": "googlesheets",
	"google docs": "googledocs", "googledocs": "googledocs", "docs": "googledocs",
}

// allToolkits is the set composio-mcp exposes (COMPOSIO_TOOLKITS), with display names.
var allToolkits = []struct{ slug, name string }{
	{"github", "GitHub"}, {"gmail", "Gmail"}, {"googlecalendar", "Google Calendar"},
	{"googledrive", "Google Drive"}, {"googlesheets", "Google Sheets"}, {"googledocs", "Google Docs"},
	{"slack", "Slack"}, {"notion", "Notion"}, {"outlook", "Outlook"},
	{"salesforce", "Salesforce"}, {"linkedin", "LinkedIn"}, {"twitter", "X (Twitter)"},
}

func integrationDisplayName(slug string) string {
	for _, tk := range allToolkits {
		if tk.slug == slug {
			return tk.name
		}
	}
	return slug
}

func (t *CheckIntegrationTool) Execute(ctx context.Context, args map[string]any) *Result {
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		return ErrorResult("cannot check integrations without a user context")
	}
	provider := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", args["provider"])))
	if provider == "" || provider == "<nil>" {
		return t.listAll(ctx, userID)
	}

	slug, ok := toolkitAliases[provider]
	if !ok {
		return NewResult(fmt.Sprintf("%q isn't an integration I can check. Available: GitHub, Gmail, Google Calendar/Drive/Docs/Sheets, Slack, Notion, Outlook, Salesforce, LinkedIn, X.", provider))
	}
	n := t.countAccounts(ctx, userID, slug)
	if n > 0 {
		return NewResult(fmt.Sprintf("%s is connected (%d account%s).", integrationDisplayName(slug), n, plural(n)))
	}
	return NewResult(fmt.Sprintf("%s is NOT connected. The user can connect it on the Integrations page in AOS.", integrationDisplayName(slug)))
}

func (t *CheckIntegrationTool) listAll(ctx context.Context, userID string) *Result {
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		connected []string
	)
	for _, tk := range allToolkits {
		wg.Add(1)
		go func(slug, name string) {
			defer wg.Done()
			if t.countAccounts(ctx, userID, slug) > 0 {
				mu.Lock()
				connected = append(connected, name)
				mu.Unlock()
			}
		}(tk.slug, tk.name)
	}
	wg.Wait()
	if len(connected) == 0 {
		return NewResult("No integrations are currently connected. The user can connect them on the Integrations page in AOS.")
	}
	sort.Strings(connected)
	return NewResult("Connected integrations: " + strings.Join(connected, ", ") + ".")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// countAccounts returns how many ACTIVE accounts the user has for a toolkit,
// via composio-mcp /accounts (same endpoint the inbox uses).
func (t *CheckIntegrationTool) countAccounts(ctx context.Context, userID, toolkit string) int {
	body, _ := json.Marshal(map[string]any{"toolkit": toolkit})
	req, err := http.NewRequestWithContext(ctx, "POST", t.composioURL+"/accounts", bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Proxy-User", userID)
	resp, err := t.http.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0
	}
	var out struct {
		Accounts []struct {
			ID string `json:"id"`
		} `json:"accounts"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return 0
	}
	return len(out.Accounts)
}
