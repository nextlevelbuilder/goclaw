package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Shared fingerprint checks ---

var fingerprintChecks = []struct {
	name   string
	js     string
	expect string
}{
	{"webdriver_absent", "() => String('webdriver' in navigator)", "false"},
	{"chrome", "() => typeof window.chrome", "object"},
	{"chrome.runtime", "() => typeof window.chrome.runtime", "object"},
	{"chrome.csi", "() => typeof window.chrome.csi", "function"},
	{"chrome.loadTimes", "() => typeof window.chrome.loadTimes", "function"},
	{"plugins.length", "() => String(navigator.plugins.length > 0)", "true"},
	{"languages", "() => String(navigator.languages.length > 0)", "true"},
	{"userAgent no headless", "() => /HeadlessChrome/.test(navigator.userAgent) ? 'HEADLESS_LEAK' : 'ok'", "ok"},
	{"connection", "() => typeof navigator.connection", "object"},
	{"permissions", "() => typeof navigator.permissions.query", "function"},
	{"outerWidth matches screen", "() => String(window.outerWidth === screen.width)", "true"},
	{"outerHeight matches screen", "() => String(window.outerHeight === screen.height)", "true"},
	{"screen.availWidth", "() => String(screen.availWidth > 0)", "true"},
	{"screen.availHeight", "() => String(screen.availHeight > 0)", "true"},
}

func runFingerprintChecks(t *testing.T, page Page) {
	t.Helper()
	for _, c := range fingerprintChecks {
		res, err := page.Eval(c.js)
		if err != nil {
			t.Logf("  [WARN] %s: %v", c.name, err)
			continue
		}
		val := fmt.Sprintf("%v", res.Value)
		if val != c.expect {
			t.Errorf("  [FAIL] %s = %q, want %q", c.name, val, c.expect)
		} else {
			t.Logf("  [OK] %s = %s", c.name, val)
		}
	}
}

// =============================================================================
// Basic image tests (chromedp/headless-shell)
// =============================================================================

// TestBasicFingerprint runs fingerprint checks on the default headless-shell image.
//
//	go test -v -run TestBasicFingerprint -timeout 120s ./pkg/browser/
func TestBasicFingerprint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	eng := NewContainerEngine(DefaultContainerImage, slog.Default())
	if err := eng.Launch(LaunchOpts{}); err != nil {
		t.Fatalf("launch container: %v", err)
	}
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	page, err := eng.NewPage(ctx, "https://bot.sannysoft.com/")
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	defer page.Close()

	waitStable(page, 2*time.Second, 15*time.Second)
	saveScreenshot(t, page, "basic-sannysoft")
	dumpSannysoftResults(t, page)
	runFingerprintChecks(t, page)
}

// =============================================================================
// Stealth image tests (goclaw/chromium)
// =============================================================================

// TestStealthFingerprint runs fingerprint checks on the stealth Chromium image.
//
//	go test -v -run TestStealthFingerprint -timeout 120s ./pkg/browser/
func TestStealthFingerprint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	eng := NewContainerEngine(StealthContainerImage, slog.Default())
	if err := ensureImage(StealthContainerImage, slog.Default()); err != nil {
		t.Fatalf("ensure stealth image: %v", err)
	}
	if err := eng.Launch(LaunchOpts{}); err != nil {
		t.Fatalf("launch container: %v", err)
	}
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	page, err := eng.NewPage(ctx, "https://bot.sannysoft.com/")
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	defer page.Close()

	waitStable(page, 2*time.Second, 15*time.Second)
	saveScreenshot(t, page, "stealth-sannysoft")
	dumpSannysoftResults(t, page)
	runFingerprintChecks(t, page)

	// Extra stealth-only checks
	pluginsRes, _ := page.Eval("() => String(navigator.plugins instanceof PluginArray)")
	if v := fmt.Sprintf("%v", pluginsRes.Value); v != "true" {
		t.Errorf("  [FAIL] plugins instanceof PluginArray = %q, want %q", v, "true")
	} else {
		t.Logf("  [OK] plugins instanceof PluginArray = %s", v)
	}

	// Language: fingerprint randomizes locale, so verify consistency rather than exact value.
	// navigator.language must equal navigator.languages[0].
	langRes, _ := page.Eval("() => JSON.stringify({lang: navigator.language, langs: navigator.languages})")
	t.Logf("  [INFO] language fingerprint: %s", langRes.Value)
	langConsistent, _ := page.Eval("() => String(navigator.language === navigator.languages[0])")
	if v := fmt.Sprintf("%v", langConsistent.Value); v != "true" {
		t.Errorf("  [FAIL] language consistency: navigator.language !== navigator.languages[0]")
	} else {
		t.Logf("  [OK] language consistency: navigator.language === navigator.languages[0]")
	}
}

// TestStealthGoogleSearch verifies that the stealth browser can perform a Google search.
//
//	go test -v -run TestStealthGoogleSearch -timeout 120s ./pkg/browser/
//
// NOTE: Sensitive to IP rate-limiting. If flagged by Google, wait or use a different IP.
func TestStealthGoogleSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	eng := NewContainerEngine(StealthContainerImage, slog.Default())
	if err := ensureImage(StealthContainerImage, slog.Default()); err != nil {
		t.Fatalf("ensure stealth image: %v", err)
	}
	if err := eng.Launch(LaunchOpts{}); err != nil {
		t.Fatalf("launch container: %v", err)
	}
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	page, err := eng.NewPage(ctx, "https://www.google.com/")
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	defer page.Close()

	waitStable(page, 2*time.Second, 5*time.Second)

	homeInfo, _ := page.Info()
	t.Logf("Homepage: %s — %s", homeInfo.URL, homeInfo.Title)

	homeCheck, _ := page.Eval("() => document.body.innerText.includes('unusual traffic') ? 'blocked' : 'ok'")
	if fmt.Sprintf("%v", homeCheck.Value) == "blocked" {
		t.Skip("IP rate-limited by Google — homepage blocked, skip search test")
	}

	time.Sleep(3 * time.Second)

	if err := page.Navigate("https://www.google.com/search?q=test"); err != nil {
		t.Fatalf("navigate to search: %v", err)
	}

	waitStable(page, 500*time.Millisecond, 10*time.Second)

	info, err := page.Info()
	if err != nil {
		t.Fatalf("page info: %v", err)
	}

	t.Logf("Search URL: %s", info.URL)
	t.Logf("Search Title: %s", info.Title)

	captcha, _ := page.Eval("() => document.querySelector('iframe[src*=\"recaptcha\"]') ? 'captcha' : 'none'")
	results, _ := page.Eval("() => document.querySelectorAll('#search .g, #rso .g, div[data-hveid]').length")

	captchaVal := fmt.Sprintf("%v", captcha.Value)
	resultsVal := fmt.Sprintf("%v", results.Value)

	t.Logf("CAPTCHA: %s", captchaVal)
	t.Logf("Search results: %s", resultsVal)

	if strings.Contains(captchaVal, "captcha") {
		if strings.Contains(info.URL, "/sorry/") {
			t.Error("Google CAPTCHA triggered — likely IP rate-limited. Wait and retry with a fresh IP.")
		} else {
			t.Error("Google bot detection triggered — CAPTCHA detected")
		}
	}
}

// TestStealthHeadlessDetection tests against intoli.com headless detection page.
//
//	go test -v -run TestStealthHeadlessDetection -timeout 60s ./pkg/browser/
func TestStealthHeadlessDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	eng := NewContainerEngine(StealthContainerImage, slog.Default())
	if err := ensureImage(StealthContainerImage, slog.Default()); err != nil {
		t.Fatalf("ensure stealth image: %v", err)
	}
	if err := eng.Launch(LaunchOpts{}); err != nil {
		t.Fatalf("launch container: %v", err)
	}
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	page, err := eng.NewPage(ctx, "https://arh.antoinevastel.com/bots/areyouheadless")
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	defer page.Close()

	waitStable(page, 2*time.Second, 10*time.Second)

	info, _ := page.Info()
	t.Logf("URL: %s", info.URL)

	result, err := page.Eval("() => document.querySelector('#res')?.innerText || document.body.innerText.substring(0, 200)")
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	val := fmt.Sprintf("%v", result.Value)
	t.Logf("Detection result: %s", val)

	if strings.Contains(strings.ToLower(val), "you are chrome headless") ||
		strings.Contains(strings.ToLower(val), "headless: true") {
		t.Error("Headless Chrome detected!")
	}
}

// TestStealthNowsecure tests against nowsecure.nl anti-bot detection (Cloudflare).
//
//	go test -v -run TestStealthNowsecure -timeout 60s ./pkg/browser/
func TestStealthNowsecure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	eng := NewContainerEngine(StealthContainerImage, slog.Default())
	if err := ensureImage(StealthContainerImage, slog.Default()); err != nil {
		t.Fatalf("ensure stealth image: %v", err)
	}
	if err := eng.Launch(LaunchOpts{}); err != nil {
		t.Fatalf("launch container: %v", err)
	}
	defer eng.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	page, err := eng.NewPage(ctx, "https://nowsecure.nl/")
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	defer page.Close()

	waitStable(page, 2*time.Second, 15*time.Second)

	info, _ := page.Info()
	t.Logf("URL: %s", info.URL)
	t.Logf("Title: %s", info.Title)

	blocked, _ := page.Eval("() => document.title.includes('Just a moment') || document.title.includes('Attention Required') ? 'blocked' : 'passed'")
	val := fmt.Sprintf("%v", blocked.Value)
	t.Logf("Cloudflare: %s", val)

	if val == "blocked" {
		t.Error("Cloudflare challenge not passed")
	}
}

func saveScreenshot(t *testing.T, page Page, name string) {
	t.Helper()
	data, err := page.Screenshot(false, nil) // viewport only, not full page
	if err != nil {
		t.Logf("screenshot %s failed: %v", name, err)
		return
	}
	dir := filepath.Join(os.TempDir(), "goclaw-stealth-test")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name+".png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Logf("write screenshot failed: %v", err)
		return
	}
	t.Logf("screenshot saved: %s (%d bytes)", path, len(data))
}

// dumpSannysoftResults scrapes the bot.sannysoft.com result table and logs pass/fail counts.
func dumpSannysoftResults(t *testing.T, page Page) {
	t.Helper()
	res, err := page.Eval(`() => {
		const rows = document.querySelectorAll('table tr');
		let pass = 0, fail = 0, warn = 0, results = [];
		rows.forEach(r => {
			const cells = r.querySelectorAll('td');
			if (cells.length < 2) return;
			const name = cells[0].innerText.trim();
			const val = cells[1].innerText.trim();
			const bg = cells[1].style.backgroundColor || '';
			let status = 'unknown';
			if (bg.includes('144') || bg.includes('green') || cells[1].className.includes('passed')) { status = 'PASS'; pass++; }
			else if (bg.includes('255, 0') || bg.includes('red') || cells[1].className.includes('failed')) { status = 'FAIL'; fail++; }
			else if (bg.includes('255, 255') || bg.includes('yellow')) { status = 'WARN'; warn++; }
			else { status = 'PASS'; pass++; }
			if (status !== 'PASS') results.push(name + ': ' + val + ' [' + status + ']');
		});
		return JSON.stringify({pass, fail, warn, issues: results});
	}`)
	if err != nil {
		t.Logf("scrape sannysoft failed: %v", err)
		return
	}
	t.Logf("sannysoft results: %s", res.Value)
}

func waitStable(page Page, stableDur, maxWait time.Duration) {
	done := make(chan error, 1)
	go func() { done <- page.WaitStable(stableDur) }()
	select {
	case <-done:
	case <-time.After(maxWait):
	}
}
