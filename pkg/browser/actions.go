package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// Click clicks an element by ref.
func (m *Manager) Click(ctx context.Context, targetID, ref string, opts ClickOpts) error {
	_, el, err := m.getPageAndResolve(ctx, targetID, ref)
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

	return el.Click(button, clickCount)
}

// Type types text into an element by ref.
func (m *Manager) Type(ctx context.Context, targetID, ref, text string, opts TypeOpts) error {
	page, el, err := m.getPageAndResolve(ctx, targetID, ref)
	if err != nil {
		return err
	}

	// Focus the element first (click may fail on non-interactive elements, that's OK)
	_ = el.Click(proto.InputMouseButtonLeft, 1)
	time.Sleep(50 * time.Millisecond)

	if opts.Slowly {
		// Type character by character with delay
		for _, ch := range text {
			if err := el.Input(string(ch)); err != nil {
				return fmt.Errorf("type input: %w", err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	} else {
		if err := el.Input(text); err != nil {
			return fmt.Errorf("type input: %w", err)
		}
	}

	if opts.Submit {
		time.Sleep(50 * time.Millisecond)
		if err := page.KeyboardPress(input.Enter); err != nil {
			return fmt.Errorf("press Enter: %w", err)
		}
	}

	return nil
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
	return page.KeyboardPress(k)
}

// Hover hovers over an element by ref.
func (m *Manager) Hover(ctx context.Context, targetID, ref string) error {
	_, el, err := m.getPageAndResolve(ctx, targetID, ref)
	if err != nil {
		return err
	}

	return el.Hover()
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
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Wait for text to appear (JS polling — engine-agnostic)
	if opts.Text != "" {
		return pollCondition(ctx, 30*time.Second, 200*time.Millisecond, func() (bool, error) {
			result, err := page.Eval(fmt.Sprintf(`document.body && document.body.innerText.includes(%q)`, opts.Text))
			if err != nil {
				return false, nil // page not ready
			}
			return result.Value.Bool(), nil
		})
	}

	// Wait for text to disappear (JS polling — engine-agnostic)
	if opts.TextGone != "" {
		return pollCondition(ctx, 30*time.Second, 500*time.Millisecond, func() (bool, error) {
			result, err := page.Eval(fmt.Sprintf(`!document.body || !document.body.innerText.includes(%q)`, opts.TextGone))
			if err != nil {
				return false, nil
			}
			return result.Value.Bool(), nil
		})
	}

	// Wait for URL change
	if opts.URL != "" {
		return pollCondition(ctx, 30*time.Second, 200*time.Millisecond, func() (bool, error) {
			result, err := page.Eval(`window.location.href`)
			if err != nil {
				return false, nil
			}
			return result.Value.Str() == opts.URL, nil
		})
	}

	// Default: wait for page to stabilize
	_ = page.WaitStable(300 * time.Millisecond)
	return nil
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

	return result.Value.String(), nil
}

// pollCondition polls fn every interval until it returns true, timeout expires, or ctx is cancelled.
func pollCondition(ctx context.Context, timeout, interval time.Duration, fn func() (bool, error)) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("timeout waiting for condition")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ok, err := fn()
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}
	}
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
