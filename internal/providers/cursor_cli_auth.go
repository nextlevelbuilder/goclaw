package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// CursorAuthStatus is the gateway's view of whether the Cursor `agent` CLI is signed in.
type CursorAuthStatus struct {
	LoggedIn         bool
	Email            string
	SubscriptionType string
}

type cursorAuthJSON struct {
	LoggedIn         bool   `json:"loggedIn"`
	Authenticated    bool   `json:"authenticated"`
	Email            string `json:"email,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
}

var (
	ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	// about: "User Email          user@host"
	cursorAboutEmailRe = regexp.MustCompile(`(?m)^User Email\s+(\S+)\s*$`)
	// status: "✓ Logged in as user@host" (after ANSI strip)
	cursorStatusEmailRe = regexp.MustCompile(`Logged in as\s+(\S+)`)
)

func stripANSISequences(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// CheckCursorAuthStatus reports whether the Cursor `agent` CLI is authenticated on the host.
//
// We invoke `agent status` (no extra flags). Output may be JSON (starts with '{') or plain
// text (e.g. "✓ Logged in as …"); we strip ANSI escapes and parse either form.
//
// If status output cannot be interpreted, we fall back to `agent about`.
//
// Login is `agent login`; credentials stay on disk for the CLI.
func CheckCursorAuthStatus(ctx context.Context, cliPath string) (*CursorAuthStatus, error) {
	if cliPath == "" {
		cliPath = "agent"
	}

	resolved, err := exec.LookPath(cliPath)
	if err != nil {
		return nil, fmt.Errorf("agent CLI binary not found at %q: %w", cliPath, err)
	}

	cmd := exec.CommandContext(ctx, resolved, "status")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(stripANSISequences(string(out)))

	if st := parseStatusStdout(text); st != nil {
		return st, nil
	}

	out2, errAbout := exec.CommandContext(ctx, resolved, "about").CombinedOutput()
	textAbout := strings.TrimSpace(stripANSISequences(string(out2)))
	if st := parseAboutText(textAbout); st != nil {
		return st, nil
	}

	if err != nil {
		return nil, fmt.Errorf("agent status failed: %w", err)
	}
	if errAbout != nil {
		return nil, fmt.Errorf("agent about failed after unparsable status output: %w", errAbout)
	}
	return nil, fmt.Errorf("could not parse agent status or about output")
}

// parseStatusStdout handles JSON or plain-text lines from `agent status`.
func parseStatusStdout(text string) *CursorAuthStatus {
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "{") {
		var j cursorAuthJSON
		if err := json.Unmarshal([]byte(text), &j); err != nil {
			return nil
		}
		loggedIn := j.LoggedIn || j.Authenticated
		return &CursorAuthStatus{
			LoggedIn:         loggedIn,
			Email:            j.Email,
			SubscriptionType: j.SubscriptionType,
		}
	}
	return parseStatusText(text)
}

func parseAboutText(text string) *CursorAuthStatus {
	m := cursorAboutEmailRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}
	email := strings.TrimSpace(m[1])
	if email == "" || email == "-" || strings.EqualFold(email, "none") {
		return &CursorAuthStatus{LoggedIn: false}
	}
	return &CursorAuthStatus{LoggedIn: true, Email: email}
}

func parseStatusText(text string) *CursorAuthStatus {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "not logged in") || strings.Contains(lower, "sign in") {
		return &CursorAuthStatus{LoggedIn: false}
	}
	m := cursorStatusEmailRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}
	email := strings.TrimSpace(m[1])
	if email == "" {
		return &CursorAuthStatus{LoggedIn: false}
	}
	return &CursorAuthStatus{LoggedIn: true, Email: email}
}
