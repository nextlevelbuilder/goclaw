package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/browsertakeover"
	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const maxTakeoverReason = 300

// Visible reports whether the browser has a window the user can reach: a
// locally launched, non-headless Chrome. Headless and remote (sidecar) browsers
// have no screen, so the user cannot act on them.
func (m *Manager) Visible() bool { return !m.headless && m.remoteURL == "" }

// enterTakeoverSession marks a tab as having been in the user's hands. It
// reuses the secret-tab guard: the tab belongs to one run, every input value is
// masked in snapshots, and evaluate, console and screenshot are closed until
// the agent navigates away. Whatever the user typed (a password, an OTP) can
// therefore not be read back to the model.
func (m *Manager) enterTakeoverSession(targetID, owner, pageURL string) {
	origin, _, err := topOrigin(pageURL)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.secretTabs == nil {
		m.secretTabs = make(map[string]*secretSession)
	}
	s := m.secretTabs[targetID]
	if s == nil || s.origin != origin {
		s = &secretSession{owner: owner, origin: origin, masked: map[int]bool{}}
		m.secretTabs[targetID] = s
	}
	s.fillURL = pageURL
	s.maskAll = true
}

// takeoverTarget resolves the tab, its address and its host without holding the
// manager lock during the request.
func (m *Manager) takeoverTarget(ctx context.Context, targetID string) (tid, pageURL, host string, err error) {
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantIDFromCtx(ctx))
	m.mu.Unlock()
	if err != nil {
		return "", "", "", err
	}
	info, err := page.Info()
	if err != nil || info == nil {
		return "", "", "", errors.New("cannot read the page address")
	}
	_, host, err = topOrigin(info.URL)
	if err != nil {
		return "", "", "", err
	}
	tid = targetID
	if tid == "" {
		tid = string(page.TargetID)
	}
	return tid, info.URL, host, nil
}

func (t *BrowserTool) handleTakeover(ctx context.Context, args map[string]any) *tools.Result {
	if tools.ToolChannelFromCtx(ctx) != "ws" {
		return tools.ErrorResult("Take over only works in the Web UI. Ask the user to continue this task from the Web UI.")
	}
	if !t.manager.Visible() {
		return tools.ErrorResult("Take over needs a visible browser (tools.browser.headless=false, no remote_url). " +
			"This browser is headless or remote and has no live view yet, so the user cannot act on it. " +
			"Tell the user what step they must do themselves, or ask them to sync cookies from their own browser.")
	}
	user := store.UserIDFromContext(ctx)
	if user == "" {
		return tools.ErrorResult("Take over needs a signed-in Web UI user.")
	}
	if err := t.manager.Start(ctx); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to start browser: %v", err))
	}

	targetID, _ := args["targetId"].(string)
	tid, pageURL, host, err := t.manager.takeoverTarget(ctx, targetID)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("take over failed: %v", err))
	}

	runSession := ""
	redact := func(s string) string { return s }
	if rt := secretvault.RuntimeFrom(ctx); rt != nil {
		runSession = rt.SessionKey
		redact = rt.Redactor.Redact
	}
	if err := t.manager.guardSecretTab(ctx, tid, "takeover", runSession); err != nil {
		return tools.ErrorResult(err.Error())
	}
	owner := secretOwnerKey(scopeFromCtx(ctx), runSession)
	// Mask the tab from now on so a parallel snapshot cannot read it while the user works.
	t.manager.enterTakeoverSession(tid, owner, pageURL)

	reason, _ := args["reason"].(string)
	reason = redact(strings.TrimSpace(reason))
	if r := []rune(reason); len(r) > maxTakeoverReason {
		reason = string(r[:maxTakeoverReason])
	}

	res, err := t.takeover.Request(ctx, browsertakeover.Spec{
		Tenant:     store.TenantIDFromContext(ctx),
		User:       user,
		TargetID:   tid,
		Host:       host,
		Reason:     reason,
		AgentKey:   tools.ToolAgentKeyFromCtx(ctx),
		SessionKey: tools.ToolSessionKeyFromCtx(ctx),
	})
	switch {
	case errors.Is(err, browsertakeover.ErrNoUI):
		return tools.ErrorResult("The user has no Web UI session open, so they cannot take over. Ask them to open the Web UI and try again.")
	case errors.Is(err, browsertakeover.ErrBusy):
		return tools.ErrorResult("This tab is already waiting for the user.")
	case err != nil:
		return tools.ErrorResult("take over failed")
	}

	// The user may have moved the tab (SSO redirect): guard the page it ended on.
	if _, finalURL, _, err := t.manager.takeoverTarget(ctx, tid); err == nil {
		t.manager.enterTakeoverSession(tid, owner, finalURL)
	}

	switch res.Status {
	case browsertakeover.StatusDone:
		return tools.NewResult("The user finished their part. Take a snapshot to continue. Input values are hidden, " +
			"and evaluate, console and screenshot are disabled on this tab until you navigate to another site. Do not ask the user to read secrets back.")
	case browsertakeover.StatusTimeout:
		return tools.ErrorResult("The user did not respond in time. Ask them to try again, or stop.")
	default:
		return tools.ErrorResult("The user declined to take over. Do not retry; ask them how to proceed.")
	}
}
