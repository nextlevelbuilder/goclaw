package browser

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
)

// FillSecretOpts describes one fill_secret act.
type FillSecretOpts struct {
	Secret string // vault entry name (not a placeholder)
	Field  string // "username" or "password" (default "password")
	Submit bool   // press Enter after filling
}

// FillSecret types a vault secret into an input of the top frame. The value is
// resolved here, from the vault runtime in ctx, and never leaves this function:
// the destination host comes from the page itself, not from tool arguments.
func (m *Manager) FillSecret(ctx context.Context, targetID, ref string, opts FillSecretOpts) error {
	rt := secretvault.RuntimeFrom(ctx)
	if rt == nil {
		return errors.New("secret vault is not available in this run")
	}
	field := opts.Field
	if field == "" {
		field = "password"
	}
	if field != "username" && field != "password" {
		return errors.New(`field must be "username" or "password"`)
	}
	if opts.Secret == "" {
		return errors.New("secret name is required")
	}

	page, el, err := m.getPageAndResolve(ctx, targetID, ref)
	if err != nil {
		return err
	}
	info, err := page.Info()
	if err != nil || info == nil {
		return errors.New("cannot read the page address")
	}
	host, err := checkFillOrigin(info.URL)
	if err != nil {
		return err
	}
	if err := requireTopFrameInput(el); err != nil {
		return err
	}

	tid := targetID
	if tid == "" {
		tid = string(page.TargetID)
	}
	owner := secretOwnerKey(scopeFromCtx(ctx), rt.SessionKey)
	// A tab that already holds another run's secret is off limits.
	if err := m.guardSecretTab(ctx, tid, "fill_secret", rt.SessionKey); err != nil {
		return err
	}

	sec, err := rt.Use(ctx, opts.Secret, "", host)
	if err != nil {
		return err
	}
	value := sec.Payload.Secret
	if field == "username" {
		value = sec.Payload.Username
	}
	if value == "" {
		return fmt.Errorf("secret %q has no %s", opts.Secret, field)
	}

	node, _ := proto.DOMDescribeNode{ObjectID: el.Object.ObjectID}.Call(page)
	maskID := 0
	if node != nil && node.Node != nil {
		maskID = int(node.Node.BackendNodeID)
	}
	// Register the session before typing so a failure midway still hides the field.
	m.enterSecretSession(tid, owner, info.URL, opts.Submit, maskID)

	if err := fillElement(page, el, value, opts.Submit); err != nil {
		// Never echo the underlying error: it could carry the typed value.
		return errors.New("fill failed")
	}
	return nil
}

// requireTopFrameInput rejects anything but a text-entry control that lives in
// the top frame, so a cross-origin iframe cannot receive the secret.
func requireTopFrameInput(el *rod.Element) error {
	res, err := el.Eval(`() => {
		const tag = this.tagName;
		const ok = (tag === 'INPUT' || tag === 'TEXTAREA') && this.ownerDocument.defaultView === window.top;
		return ok;
	}`)
	if err != nil || !res.Value.Bool() {
		return errors.New("target must be an input in the top-level page, not inside a frame")
	}
	return nil
}

func fillElement(page *rod.Page, el *rod.Element, value string, submit bool) error {
	if err := el.Focus(); err != nil {
		return err
	}
	if err := el.SelectAllText(); err != nil {
		return err
	}
	if err := el.Input(value); err != nil {
		return err
	}
	if submit {
		return page.Keyboard.Press(input.Enter)
	}
	return nil
}
