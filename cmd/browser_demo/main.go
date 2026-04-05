// Quick browser demo — no DB, no gateway needed.
// Usage: go run ./cmd/browser_demo
//
// Optional env vars:
//   BROWSER_BINARY  - custom binary (e.g. Brave)
//   BROWSER_URL     - URL to visit (default: https://example.com)
//   BROWSER_HEADLESS - "false" to see the browser window
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

func main() {
	url := envOr("BROWSER_URL", "https://example.com")
	headless := envOr("BROWSER_HEADLESS", "true") != "false"

	opts := []browser.Option{
		browser.WithHeadless(headless),
		browser.WithWorkspace(os.TempDir()),
		browser.WithIdleTimeout(0),
	}
	if bin := os.Getenv("BROWSER_BINARY"); bin != "" {
		opts = append(opts, browser.WithBinaryPath(bin))
		fmt.Printf("Binary: %s\n", bin)
	}

	m := browser.New(opts...)
	ctx := context.Background()

	// --- Start ---
	fmt.Println("=== Starting browser...")
	if err := m.Start(ctx); err != nil {
		log.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	status := m.Status()
	fmt.Printf("Engine: %s | Running: %v\n\n", status.Engine, status.Running)

	// --- Open tab ---
	fmt.Printf("=== Opening %s...\n", url)
	tab, err := m.OpenTab(ctx, url)
	if err != nil {
		log.Fatalf("OpenTab: %v", err)
	}
	fmt.Printf("TargetID: %s\nURL: %s\nTitle: %s\n\n", tab.TargetID, tab.URL, tab.Title)

	time.Sleep(1 * time.Second)

	// --- Snapshot ---
	fmt.Println("=== Snapshot (accessibility tree):")
	snap, err := m.Snapshot(ctx, tab.TargetID, browser.DefaultSnapshotOptions())
	if err != nil {
		log.Fatalf("Snapshot: %v", err)
	}
	fmt.Printf("Title: %s\nURL: %s\nRefs: %d | Interactive: %d\n", snap.Title, snap.URL, snap.Stats.Refs, snap.Stats.Interactive)
	// Print first 500 chars of snapshot
	s := snap.Snapshot
	if len(s) > 500 {
		s = s[:500] + "..."
	}
	fmt.Println(s)
	fmt.Println()

	// --- Evaluate JS ---
	fmt.Println("=== Evaluate JS:")
	ua, err := m.Evaluate(ctx, tab.TargetID, `() => navigator.userAgent`)
	if err != nil {
		log.Printf("Evaluate userAgent: %v", err)
	} else {
		fmt.Printf("User-Agent: %s\n", ua)
	}

	title, err := m.Evaluate(ctx, tab.TargetID, `() => document.title`)
	if err != nil {
		log.Printf("Evaluate title: %v", err)
	} else {
		fmt.Printf("document.title: %s\n", title)
	}
	fmt.Println()

	// --- Cookies ---
	fmt.Println("=== Cookies:")
	if err := m.SetCookie(ctx, tab.TargetID, &browser.Cookie{
		Name: "demo_cookie", Value: "hello_goclaw", Domain: ".example.com", Path: "/",
	}); err != nil {
		log.Printf("SetCookie: %v", err)
	}
	cookies, err := m.GetCookies(ctx, tab.TargetID)
	if err != nil {
		log.Printf("GetCookies: %v", err)
	} else {
		for _, c := range cookies {
			fmt.Printf("  %s = %s (domain=%s)\n", c.Name, c.Value, c.Domain)
		}
	}
	fmt.Println()

	// --- Storage ---
	fmt.Println("=== LocalStorage:")
	if err := m.SetStorage(ctx, tab.TargetID, true, "demo_key", "demo_value"); err != nil {
		log.Printf("SetStorage: %v", err)
	}
	items, err := m.GetStorage(ctx, tab.TargetID, true)
	if err != nil {
		log.Printf("GetStorage: %v", err)
	} else {
		for k, v := range items {
			fmt.Printf("  %s = %s\n", k, v)
		}
	}
	fmt.Println()

	// --- Screenshot ---
	fmt.Println("=== Screenshot:")
	data, err := m.Screenshot(ctx, tab.TargetID, false)
	if err != nil {
		log.Printf("Screenshot: %v", err)
	} else {
		path := fmt.Sprintf("/tmp/goclaw_demo_%d.png", time.Now().Unix())
		os.WriteFile(path, data, 0644)
		fmt.Printf("Saved: %s (%d bytes)\n", path, len(data))
	}
	fmt.Println()

	// --- Navigate ---
	fmt.Println("=== Navigate to https://httpbin.org/get...")
	if err := m.Navigate(ctx, tab.TargetID, "https://httpbin.org/get"); err != nil {
		log.Printf("Navigate: %v", err)
	}
	time.Sleep(1 * time.Second)

	snap2, err := m.Snapshot(ctx, tab.TargetID, browser.DefaultSnapshotOptions())
	if err != nil {
		log.Printf("Snapshot2: %v", err)
	} else {
		s2 := snap2.Snapshot
		if len(s2) > 300 {
			s2 = s2[:300] + "..."
		}
		fmt.Printf("New page title: %s\n%s\n", snap2.Title, s2)
	}
	fmt.Println()

	// --- JS Errors ---
	fmt.Println("=== JS Errors (inject one):")
	m.Evaluate(ctx, tab.TargetID, `() => setTimeout(() => { throw new Error("demo error") }, 0)`)
	time.Sleep(300 * time.Millisecond)
	jsErrors, err := m.GetJSErrors(ctx, tab.TargetID)
	if err != nil {
		log.Printf("GetJSErrors: %v", err)
	} else {
		fmt.Printf("Captured: %d errors\n", len(jsErrors))
		for _, e := range jsErrors {
			fmt.Printf("  %s (line %d)\n", e.Text, e.Line)
		}
	}
	fmt.Println()

	// --- Tabs ---
	fmt.Println("=== Tabs:")
	tab2, _ := m.OpenTab(ctx, "https://example.org")
	tabs, _ := m.ListTabs(ctx)
	fmt.Printf("Open tabs: %d\n", len(tabs))
	for _, t := range tabs {
		fmt.Printf("  [%s] %s - %s\n", t.TargetID[:8], t.URL, t.Title)
	}
	if tab2 != nil {
		m.CloseTab(ctx, tab2.TargetID)
	}
	fmt.Println()

	// --- Profiles ---
	fmt.Println("=== Profiles:")
	sm := browser.NewStorageManager(os.TempDir(), nil)
	profiles, _ := sm.ListProfiles("default")
	fmt.Printf("Profiles: %d\n", len(profiles))
	for _, p := range profiles {
		fmt.Printf("  %s (%s, modified %s)\n", p.Name, p.Size, p.LastModified.Format("2006-01-02 15:04"))
	}

	fmt.Println("\n=== Done! Stopping browser...")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}
