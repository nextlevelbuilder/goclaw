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
	return page.Keyboard.Press(k)
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

	// Wait for text to appear
	if opts.Text != "" {
		return rod.Try(func() {
			page.Timeout(30 * time.Second).MustElementR("*", opts.Text)
		})
	}

	// Wait for text to disappear
	if opts.TextGone != "" {
		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				return fmt.Errorf("timeout waiting for text %q to disappear", opts.TextGone)
			case <-ticker.C:
				has, _, _ := page.Has("*")
				if !has {
					return nil
				}
				el, err := page.ElementR("*", opts.TextGone)
				if err != nil || el == nil {
					return nil
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
		return nil
	}

	// Default: wait for page to stabilize
	waitStable(page)
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
