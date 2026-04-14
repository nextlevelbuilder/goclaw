package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// Click clicks an element by ref.
func (m *Manager) Click(ctx context.Context, targetID, ref string, opts ClickOpts) error {
	page, el, err := m.getPageAndResolve(ctx, targetID, ref)
	if err != nil {
		return err
	}

	button := proto.InputMouseButtonLeft
	if opts.Button == "right" {
		button = proto.InputMouseButtonRight
	} else if opts.Button == "middle" {
		button = proto.InputMouseButtonMiddle
	}

	clickCount := 1
	if opts.DoubleClick {
		clickCount = 2
	}

	if err := el.Click(button, clickCount); err != nil {
		return err
	}
	waitStable(page)
	return m.ensurePageURLAllowed(pageTargetID(targetID, page), page)
}

// Type types text into an element by ref.
func (m *Manager) Type(ctx context.Context, targetID, ref, text string, opts TypeOpts) error {
	page, el, err := m.getPageAndResolve(ctx, targetID, ref)
	if err != nil {
		return err
	}

	// Focus the element first
	_ = el.Click(proto.InputMouseButtonLeft, 1)
	time.Sleep(50 * time.Millisecond)

	if opts.Slowly {
		// Type character by character with delay
		for _, ch := range text {
			el.MustInput(string(ch))
			time.Sleep(50 * time.Millisecond)
		}
	} else {
		el.MustInput(text)
	}

	if opts.Submit {
		time.Sleep(50 * time.Millisecond)
		_ = page.Keyboard.Press(input.Enter)
	}

	waitStable(page)
	return m.ensurePageURLAllowed(pageTargetID(targetID, page), page)
}

// Press presses a keyboard key.
func (m *Manager) Press(ctx context.Context, targetID, key string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}

	k := mapKey(key)
	if err := page.Keyboard.Press(k); err != nil {
		return err
	}
	waitStable(page)
	return m.ensurePageURLAllowed(pageTargetID(targetID, page), page)
}

// Hover hovers over an element by ref.
func (m *Manager) Hover(ctx context.Context, targetID, ref string) error {
	page, el, err := m.getPageAndResolve(ctx, targetID, ref)
	if err != nil {
		return err
	}

	if err := el.Hover(); err != nil {
		return err
	}
	waitStable(page)
	return m.ensurePageURLAllowed(pageTargetID(targetID, page), page)
}

// Wait waits for a condition on a page.
func (m *Manager) Wait(ctx context.Context, targetID string, opts WaitOpts) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}

	// Simple time wait
	if opts.TimeMs > 0 {
		select {
		case <-time.After(time.Duration(opts.TimeMs) * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Wait for text to appear
	if opts.Text != "" {
		if err := rod.Try(func() {
			page.Timeout(30*time.Second).MustElementR("*", opts.Text)
		}); err != nil {
			return err
		}
	}

	// Wait for text to disappear
	if opts.TextGone != "" {
		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
	textGoneLoop:
		for {
			select {
			case <-timeout:
				return fmt.Errorf("timeout waiting for text %q to disappear", opts.TextGone)
			case <-ticker.C:
				has, _, _ := page.Has("*")
				if !has {
					break textGoneLoop
				}
				el, err := page.ElementR("*", opts.TextGone)
				if err != nil || el == nil {
					break textGoneLoop
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Wait for URL
	if opts.URL != "" {
		wait := page.WaitNavigation(proto.PageLifecycleEventNameLoad)
		wait()
	}

	// Default: wait for page to stabilize.
	waitStable(page)
	return m.ensurePageURLAllowed(pageTargetID(targetID, page), page)
}

// Evaluate runs JavaScript on a page.
func (m *Manager) Evaluate(ctx context.Context, targetID, js string) (string, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return "", err
	}

	result, err := page.Eval(js)
	if err != nil {
		return "", fmt.Errorf("evaluate: %w", err)
	}

	waitStable(page)
	if err := m.ensurePageURLAllowed(pageTargetID(targetID, page), page); err != nil {
		return "", err
	}

	return result.Value.String(), nil
}

// mapKey converts a key name string to a Rod keyboard key.
func mapKey(key string) input.Key {
	switch key {
	case "Enter":
		return input.Enter
	case "Tab":
		return input.Tab
	case "Escape":
		return input.Escape
	case "Backspace":
		return input.Backspace
	case "Delete":
		return input.Delete
	case "ArrowUp":
		return input.ArrowUp
	case "ArrowDown":
		return input.ArrowDown
	case "ArrowLeft":
		return input.ArrowLeft
	case "ArrowRight":
		return input.ArrowRight
	case "Home":
		return input.Home
	case "End":
		return input.End
	case "PageUp":
		return input.PageUp
	case "PageDown":
		return input.PageDown
	case "Space":
		return input.Space
	default:
		// Try single character
		if len(key) == 1 {
			return input.Key(key[0])
		}
		return input.Enter
	}
}
