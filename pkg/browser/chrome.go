package browser

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

// ChromeEngine implements Engine using go-rod (Chrome DevTools Protocol).
type ChromeEngine struct {
	browser  *rod.Browser
	attached bool // true = don't kill process on Close()
	logger   *slog.Logger
}

// NewChromeEngine creates a ChromeEngine. The engine is not connected until Launch() is called.
func NewChromeEngine(logger *slog.Logger) *ChromeEngine {
	return &ChromeEngine{logger: logger}
}

func (e *ChromeEngine) Launch(opts LaunchOpts) error {
	switch {
	case opts.AttachURL != "":
		// Attach mode — connect to existing browser, don't kill on Close
		b := rod.New().ControlURL(opts.AttachURL)
		if err := b.Connect(); err != nil {
			return fmt.Errorf("attach to Chrome at %s: %w", opts.AttachURL, err)
		}
		e.browser = b
		e.attached = true
		e.logger.Info("attached to existing Chrome", "cdp", opts.AttachURL)

	case opts.RemoteURL != "":
		// Remote sidecar — resolve via /json/version
		u, err := resolveRemoteCDP(opts.RemoteURL)
		if err != nil {
			return fmt.Errorf("resolve remote Chrome at %s: %w", opts.RemoteURL, err)
		}
		b := rod.New().ControlURL(u)
		if err := b.Connect(); err != nil {
			return fmt.Errorf("connect to remote Chrome: %w", err)
		}
		e.browser = b
		e.attached = false
		e.logger.Info("connected to remote Chrome", "cdp", u, "remote", opts.RemoteURL)

	default:
		// Local browser — launch via rod launcher
		l := launcher.New().
			Headless(opts.Headless).
			Set("disable-gpu").
			Set("no-default-browser-check")

		if opts.BinaryPath != "" {
			l.Bin(opts.BinaryPath)
		}
		if opts.ProfileDir != "" {
			l.UserDataDir(opts.ProfileDir)
		}
		if opts.ProxyURL != "" {
			l.Set("proxy-server", stripUserinfo(opts.ProxyURL))
		}
		if len(opts.ExtensionPaths) > 0 {
			l.Set("load-extension", strings.Join(opts.ExtensionPaths, ","))
		}

		// Apply stealth flags to reduce automation detection
		StealthFlags(l)

		u, err := l.Launch()
		if err != nil {
			return fmt.Errorf("launch browser: %w", err)
		}
		b := rod.New().ControlURL(u)
		if err := b.Connect(); err != nil {
			return fmt.Errorf("connect to browser: %w", err)
		}
		e.browser = b
		e.attached = false
		e.logger.Info("browser launched", "cdp", u, "headless", opts.Headless, "profile", opts.ProfileDir, "binary", opts.BinaryPath)
	}
	return nil
}

func (e *ChromeEngine) Close() error {
	if e.browser == nil {
		return nil
	}
	if e.attached {
		// Don't kill user's browser — just disconnect
		e.browser = nil
		return nil
	}
	err := e.browser.Close()
	e.browser = nil
	return err
}

func (e *ChromeEngine) NewPage(ctx context.Context, url string) (Page, error) {
	if e.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}

	// Create a blank page first so we can inject stealth scripts
	// BEFORE any page JS runs (prevents bot detection).
	rodPage, err := e.browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return nil, fmt.Errorf("new page: %w", err)
	}

	// Inject stealth + fingerprint via Page.addScriptToEvaluateOnNewDocument.
	// RunImmediately=true ensures the script runs on the CURRENT about:blank
	// context AND on all future navigations. Without it, Chrome re-applies
	// navigator.webdriver=true during navigation before our script fires.
	fp := GenerateFingerprint("")
	stealthScript := stealthOnNewDocumentJS + "\n" + FingerprintOnNewDocumentJS(fp)
	_, _ = proto.PageAddScriptToEvaluateOnNewDocument{
		Source:         stealthScript,
		RunImmediately: true,
	}.Call(rodPage)

	// Override UA at CDP/network level so HTTP request headers match the
	// fingerprint. JS-only overrides don't change the actual User-Agent header
	// sent with HTTP requests — Google checks this header server-side.
	_ = proto.NetworkSetUserAgentOverride{
		UserAgent:      fp.UserAgent,
		AcceptLanguage: strings.Join(fp.Languages, ","),
		Platform:       fp.Platform,
	}.Call(rodPage)

	// Override navigator.language/languages at CDP level.
	// JS overrides on Navigator.prototype get reset by Chrome on navigation,
	// but Emulation.setLocaleOverride persists across navigations.
	_ = proto.EmulationSetLocaleOverride{
		Locale: fp.Languages[0],
	}.Call(rodPage)

	// Set viewport to match fingerprint screen dimensions.
	// Mismatch between window.outerWidth/outerHeight and screen.width/height
	// is a detection signal (PHANTOM_WINDOW_HEIGHT check).
	_ = proto.EmulationSetDeviceMetricsOverride{
		Width:             fp.ScreenWidth,
		Height:            fp.ScreenHeight,
		DeviceScaleFactor: 1,
		Mobile:            false,
	}.Call(rodPage)

	// Bypass CSP so stealth scripts can override properties on strict pages.
	_ = proto.PageSetBypassCSP{Enabled: true}.Call(rodPage)

	// Setup CDP Fetch-based proxy authentication.
	// Chrome's --proxy-server flag doesn't support credentials in the URL, so we
	// intercept 407 Proxy-Auth-Required challenges via the Fetch domain and respond
	// with the decrypted username/password.
	if creds := proxyAuthCredsFromCtx(ctx); creds != nil {
		_ = proto.FetchEnable{
			HandleAuthRequests: true,
		}.Call(rodPage)
		go rodPage.EachEvent(func(e *proto.FetchAuthRequired) {
			_ = proto.FetchContinueWithAuth{
				RequestID: e.RequestID,
				AuthChallengeResponse: &proto.FetchAuthChallengeResponse{
					Response: proto.FetchAuthChallengeResponseResponseProvideCredentials,
					Username: creds.Username,
					Password: creds.Password,
				},
			}.Call(rodPage)
		}, func(e *proto.FetchRequestPaused) {
			// Resume non-auth paused requests (Fetch.enable pauses ALL requests).
			_ = proto.FetchContinueRequest{
				RequestID: e.RequestID,
			}.Call(rodPage)
		})()
	}

	// Now navigate — stealth scripts will run before page JS.
	if url != "" && url != "about:blank" {
		if err := rodPage.Navigate(url); err != nil {
			rodPage.Close()
			return nil, fmt.Errorf("navigate to %s: %w", url, err)
		}
	}

	return newChromePage(rodPage), nil
}

func (e *ChromeEngine) Pages() ([]Page, error) {
	if e.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}
	rodPages, err := e.browser.Pages()
	if err != nil {
		return nil, err
	}
	pages := make([]Page, len(rodPages))
	for i, p := range rodPages {
		pages[i] = newChromePage(p)
	}
	return pages, nil
}

func (e *ChromeEngine) Incognito() (Engine, error) {
	if e.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}
	b, err := e.browser.Incognito()
	if err != nil {
		return nil, fmt.Errorf("create incognito context: %w", err)
	}
	return &ChromeEngine{browser: b, logger: e.logger}, nil
}

func (e *ChromeEngine) IsConnected() bool {
	if e.browser == nil {
		return false
	}
	_, err := e.browser.Pages()
	return err == nil
}

func (e *ChromeEngine) Name() string { return "chrome" }

// RodBrowser returns the underlying rod.Browser for reconnect scenarios.
// This is a Chrome-specific escape hatch; callers must type-assert.
func (e *ChromeEngine) RodBrowser() *rod.Browser { return e.browser }

// ---------------------------------------------------------------------------
// ChromePage wraps *rod.Page to implement the Page interface.
// ---------------------------------------------------------------------------

type ChromePage struct {
	page            *rod.Page
	errors          []*JSError
	errMu           sync.Mutex
	screencastMu    sync.Mutex
	screencastSubs  map[chan<- ScreencastFrame]struct{} // fan-out: multiple WS viewers per page
	screencastOn    bool                               // true if CDP screencast + event listener are active
	screencastStop  context.CancelFunc                 // cancels the EachEvent listener goroutine
}

func newChromePage(p *rod.Page) *ChromePage {
	return &ChromePage{page: p}
}

// RodPage returns the underlying *rod.Page for Chrome-specific operations.
func (p *ChromePage) RodPage() *rod.Page { return p.page }

// --- Navigation ---

func (p *ChromePage) Navigate(url string) error {
	return p.page.Navigate(url)
}

func (p *ChromePage) WaitStable(d time.Duration) error {
	return p.page.WaitStable(d)
}

func (p *ChromePage) Info() (*PageInfo, error) {
	info, err := p.page.Info()
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return &PageInfo{URL: info.URL, Title: info.Title}, nil
}

func (p *ChromePage) TargetID() string {
	return string(p.page.TargetID)
}

func (p *ChromePage) Close() error {
	return p.page.Close()
}

func (p *ChromePage) Activate() error {
	_, err := p.page.Activate()
	return err
}

// --- Content ---

func (p *ChromePage) GetAXTree() ([]*proto.AccessibilityAXNode, error) {
	result, err := proto.AccessibilityGetFullAXTree{}.Call(p.page)
	if err != nil {
		return nil, err
	}
	return result.Nodes, nil
}

func (p *ChromePage) GetFrameTree() ([]FrameInfo, error) {
	result, err := proto.PageGetFrameTree{}.Call(p.page)
	if err != nil {
		return nil, err
	}
	if result.FrameTree == nil {
		return nil, nil
	}

	// Build OOPIF target map (URL → targetID) before flattening
	oopifByURL := p.discoverOOPIFsByURL()

	var frames []FrameInfo
	flattenFrameTree(result.FrameTree, "", 0, oopifByURL, &frames)
	return frames, nil
}

// discoverOOPIFsByURL finds all cross-origin iframe targets and indexes them by URL.
// Chrome creates separate targets (type "iframe") for OOPIFs. We match them to
// frames in the tree by URL since OOPIFs don't carry an OpenerID.
func (p *ChromePage) discoverOOPIFsByURL() map[string]string {
	browser := p.page.Browser()
	if browser == nil {
		return nil
	}
	targets, err := proto.TargetGetTargets{}.Call(browser)
	if err != nil {
		return nil
	}

	result := make(map[string]string)
	for _, t := range targets.TargetInfos {
		if string(t.Type) == "iframe" {
			result[t.URL] = string(t.TargetID)
		}
	}
	return result
}

// flattenFrameTree recursively converts the CDP FrameTree into a flat slice.
// oopifByURL maps iframe URLs to their OOPIF target IDs for cross-origin annotation.
func flattenFrameTree(tree *proto.PageFrameTree, parentID string, depth int, oopifByURL map[string]string, out *[]FrameInfo) {
	if tree.Frame == nil {
		return
	}
	f := tree.Frame
	fi := FrameInfo{
		FrameID:  string(f.ID),
		ParentID: parentID,
		URL:      f.URL,
		Name:     f.Name,
		Origin:   f.SecurityOrigin,
		Depth:    depth,
	}
	// Annotate cross-origin frames with their OOPIF target ID
	if tid, ok := oopifByURL[f.URL]; ok {
		fi.CrossOrigin = true
		fi.OOPIFTarget = tid
	}
	*out = append(*out, fi)
	for _, child := range tree.ChildFrames {
		flattenFrameTree(child, string(f.ID), depth+1, oopifByURL, out)
	}
}

func (p *ChromePage) GetAXTreeForFrame(frameID string) ([]*proto.AccessibilityAXNode, error) {
	// First try same-origin access via FrameID parameter
	result, err := proto.AccessibilityGetFullAXTree{
		FrameID: proto.PageFrameID(frameID),
	}.Call(p.page)
	if err == nil && len(result.Nodes) > 0 {
		return result.Nodes, nil
	}

	// Same-origin failed or returned empty — try OOPIF approach:
	// Attach to the iframe's target and get its AX tree directly.
	// The frameID may be the actual OOPIF target ID (passed from FrameInfo.OOPIFTarget),
	// or we can search for a matching target.
	browser := p.page.Browser()
	if browser == nil {
		if err != nil {
			return nil, err
		}
		return result.Nodes, nil
	}

	iframePage, attachErr := browser.PageFromTarget(proto.TargetTargetID(frameID))
	if attachErr != nil {
		// frameID wasn't a target ID — return original result
		if err != nil {
			return nil, fmt.Errorf("frame %s: same-origin empty, OOPIF attach failed: %v", frameID, attachErr)
		}
		return result.Nodes, nil
	}

	// Get AX tree from the OOPIF target (no FrameID needed — the whole target IS the frame)
	oopifResult, oopifErr := proto.AccessibilityGetFullAXTree{}.Call(iframePage)
	if oopifErr != nil {
		return nil, fmt.Errorf("get OOPIF AX tree for %s: %w", frameID, oopifErr)
	}
	return oopifResult.Nodes, nil
}

func (p *ChromePage) Screenshot(fullPage bool, opts *proto.PageCaptureScreenshot) ([]byte, error) {
	return p.page.Screenshot(fullPage, opts)
}

func (p *ChromePage) Eval(js string) (*proto.RuntimeRemoteObject, error) {
	return p.page.Eval(js)
}

// --- Input ---

func (p *ChromePage) KeyboardPress(key input.Key) error {
	return p.page.Keyboard.Press(key)
}

// --- Raw CDP Input dispatch ---

func (p *ChromePage) DispatchMouseEvent(typ string, x, y float64, button string, clickCount int) error {
	return proto.InputDispatchMouseEvent{
		Type:       proto.InputDispatchMouseEventType(typ),
		X:          x,
		Y:          y,
		Button:     proto.InputMouseButton(button),
		ClickCount: clickCount,
	}.Call(p.page)
}

func (p *ChromePage) DispatchScrollEvent(x, y, deltaX, deltaY float64) error {
	return proto.InputDispatchMouseEvent{
		Type:   proto.InputDispatchMouseEventTypeMouseWheel,
		X:      x,
		Y:      y,
		DeltaX: deltaX,
		DeltaY: deltaY,
	}.Call(p.page)
}

func (p *ChromePage) DispatchKeyEvent(typ string, key, code, text string, modifiers int, vkCode int) error {
	return proto.InputDispatchKeyEvent{
		Type:                  proto.InputDispatchKeyEventType(typ),
		Key:                   key,
		Code:                  code,
		Text:                  text,
		Modifiers:             modifiers,
		WindowsVirtualKeyCode: vkCode,
	}.Call(p.page)
}

// --- DOM resolution ---

func (p *ChromePage) ResolveBackendNode(backendNodeID int) (Element, error) {
	bid := proto.DOMBackendNodeID(backendNodeID)
	resolved, err := proto.DOMResolveNode{BackendNodeID: bid}.Call(p.page)
	if err != nil {
		return nil, err
	}
	el, err := p.page.ElementFromObject(resolved.Object)
	if err != nil {
		return nil, err
	}
	return &ChromeElement{el: el}, nil
}

func (p *ChromePage) EnableDOM() error {
	return proto.DOMEnable{}.Call(p.page)
}

// --- Console ---

func (p *ChromePage) SetupConsoleListener(handler func(ConsoleMessage)) {
	go p.page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
		var text strings.Builder
		for _, arg := range e.Args {
			s := arg.Value.String()
			if s != "" && s != "null" {
				text.WriteString(s + " ")
			}
		}

		level := "log"
		switch e.Type {
		case proto.RuntimeConsoleAPICalledTypeWarning:
			level = "warn"
		case proto.RuntimeConsoleAPICalledTypeError:
			level = "error"
		case proto.RuntimeConsoleAPICalledTypeInfo:
			level = "info"
		}

		handler(ConsoleMessage{
			Level: level,
			Text:  text.String(),
		})
	})()

	// Also listen for JS exceptions to capture errors
	go p.page.EachEvent(func(e *proto.RuntimeExceptionThrown) {
		if e.ExceptionDetails == nil {
			return
		}
		jsErr := &JSError{
			Text:   e.ExceptionDetails.Text,
			Line:   e.ExceptionDetails.LineNumber,
			Column: e.ExceptionDetails.ColumnNumber,
		}
		if e.ExceptionDetails.URL != "" {
			jsErr.URL = e.ExceptionDetails.URL
		}
		p.errMu.Lock()
		p.errors = append(p.errors, jsErr)
		if len(p.errors) > 500 {
			p.errors = p.errors[1:]
		}
		p.errMu.Unlock()
	})()
}

// --- Chrome extensions: Cookies ---

func (p *ChromePage) GetCookies() ([]*Cookie, error) {
	result, err := proto.NetworkGetCookies{}.Call(p.page)
	if err != nil {
		return nil, err
	}
	cookies := make([]*Cookie, len(result.Cookies))
	for i, c := range result.Cookies {
		cookies[i] = &Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: string(c.SameSite),
			Expires:  float64(c.Expires),
		}
	}
	return cookies, nil
}

func (p *ChromePage) SetCookie(c *Cookie) error {
	params := proto.NetworkSetCookie{
		Name:     c.Name,
		Value:    c.Value,
		Domain:   c.Domain,
		Path:     c.Path,
		Secure:   c.Secure,
		HTTPOnly: c.HTTPOnly,
	}
	if c.URL != "" {
		params.URL = c.URL
	}
	if c.Expires > 0 {
		params.Expires = proto.TimeSinceEpoch(c.Expires)
	}
	if c.SameSite != "" {
		params.SameSite = proto.NetworkCookieSameSite(c.SameSite)
	}
	_, err := params.Call(p.page)
	return err
}

func (p *ChromePage) ClearCookies() error {
	return proto.NetworkClearBrowserCookies{}.Call(p.page)
}

// --- Chrome extensions: Storage ---

func (p *ChromePage) GetStorageItems(isLocal bool) (map[string]string, error) {
	sid, err := p.storageID(isLocal)
	if err != nil {
		return nil, err
	}
	_ = proto.DOMStorageEnable{}.Call(p.page)
	result, err := proto.DOMStorageGetDOMStorageItems{StorageID: sid}.Call(p.page)
	if err != nil {
		return nil, err
	}
	items := make(map[string]string, len(result.Entries))
	for _, entry := range result.Entries {
		if len(entry) >= 2 {
			items[entry[0]] = entry[1]
		}
	}
	return items, nil
}

func (p *ChromePage) SetStorageItem(isLocal bool, key, value string) error {
	sid, err := p.storageID(isLocal)
	if err != nil {
		return err
	}
	_ = proto.DOMStorageEnable{}.Call(p.page)
	return proto.DOMStorageSetDOMStorageItem{
		StorageID: sid,
		Key:       key,
		Value:     value,
	}.Call(p.page)
}

func (p *ChromePage) ClearStorage(isLocal bool) error {
	sid, err := p.storageID(isLocal)
	if err != nil {
		return err
	}
	_ = proto.DOMStorageEnable{}.Call(p.page)
	return proto.DOMStorageClear{StorageID: sid}.Call(p.page)
}

// storageID builds a DOMStorageStorageID from the page's security origin.
func (p *ChromePage) storageID(isLocal bool) (*proto.DOMStorageStorageID, error) {
	info, err := p.page.Info()
	if err != nil || info == nil {
		return nil, fmt.Errorf("get page info for storage: %w", err)
	}
	parsed, err := url.Parse(info.URL)
	if err != nil {
		return nil, fmt.Errorf("parse page URL: %w", err)
	}
	origin := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	return &proto.DOMStorageStorageID{
		SecurityOrigin:  origin,
		IsLocalStorage:  isLocal,
	}, nil
}

// --- Chrome extensions: JS Errors ---

func (p *ChromePage) GetJSErrors() ([]*JSError, error) {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	result := make([]*JSError, len(p.errors))
	copy(result, p.errors)
	p.errors = nil
	return result, nil
}

// --- Screencast ---

func (p *ChromePage) StartScreencast(fps int, quality int, maxWidth int, maxHeight int, ch chan<- ScreencastFrame) error {
	if fps <= 0 {
		fps = 10
	}
	if quality <= 0 {
		quality = 80
	}
	if maxWidth <= 0 {
		maxWidth = 1280
	}
	if maxHeight <= 0 {
		maxHeight = 720
	}

	p.screencastMu.Lock()
	defer p.screencastMu.Unlock()

	// Add subscriber to the fan-out set.
	if p.screencastSubs == nil {
		p.screencastSubs = make(map[chan<- ScreencastFrame]struct{})
	}
	p.screencastSubs[ch] = struct{}{}

	// If CDP screencast + event listener are already active, just add the subscriber.
	if p.screencastOn {
		return nil
	}

	err := proto.PageStartScreencast{
		Format:    proto.PageStartScreencastFormatJpeg,
		Quality:   &quality,
		MaxWidth:  &maxWidth,
		MaxHeight: &maxHeight,
	}.Call(p.page)
	if err != nil {
		return err
	}

	p.screencastOn = true

	// Create a cancellable page context so StopScreencast can kill the listener goroutine.
	ctx, cancel := context.WithCancel(p.page.GetContext())
	p.screencastStop = cancel
	scPage := p.page.Context(ctx)

	go scPage.EachEvent(func(e *proto.PageScreencastFrame) {
		frame := ScreencastFrame{
			Data:      e.Data,
			SessionID: int(e.SessionID),
			Metadata: ScreencastMetadata{
				OffsetTop:       e.Metadata.OffsetTop,
				PageScaleFactor: e.Metadata.PageScaleFactor,
				DeviceWidth:     e.Metadata.DeviceWidth,
				DeviceHeight:    e.Metadata.DeviceHeight,
				ScrollOffsetX:   e.Metadata.ScrollOffsetX,
				ScrollOffsetY:   e.Metadata.ScrollOffsetY,
				Timestamp:       float64(e.Metadata.Timestamp),
			},
		}
		_ = proto.PageScreencastFrameAck{SessionID: e.SessionID}.Call(p.page)

		p.screencastMu.Lock()
		subs := make([]chan<- ScreencastFrame, 0, len(p.screencastSubs))
		for s := range p.screencastSubs {
			subs = append(subs, s)
		}
		p.screencastMu.Unlock()

		// Fan-out: non-blocking send to all subscribers
		for _, dest := range subs {
			select {
			case dest <- frame:
			default:
			}
		}
	})()

	return nil
}

func (p *ChromePage) StopScreencast() error {
	p.screencastMu.Lock()
	p.screencastSubs = nil
	wasOn := p.screencastOn
	p.screencastOn = false
	cancel := p.screencastStop
	p.screencastStop = nil
	p.screencastMu.Unlock()

	if wasOn {
		_ = proto.PageStopScreencast{}.Call(p.page)
		if cancel != nil {
			cancel()
		}
	}
	return nil
}

// StopScreencastCh removes a single subscriber. CDP screencast is stopped only when
// the last subscriber is removed.
func (p *ChromePage) StopScreencastCh(ch chan<- ScreencastFrame) {
	p.screencastMu.Lock()
	delete(p.screencastSubs, ch)
	remaining := len(p.screencastSubs)
	p.screencastMu.Unlock()

	if remaining == 0 {
		// Last subscriber gone — fully stop CDP screencast
		_ = p.StopScreencast()
	}
}

// --- Emulation ---

func (p *ChromePage) Emulate(opts EmulateOpts) error {
	if opts.UserAgent != "" {
		if err := (proto.NetworkSetUserAgentOverride{UserAgent: opts.UserAgent}).Call(p.page); err != nil {
			return fmt.Errorf("set user agent: %w", err)
		}
	}
	if opts.Width > 0 && opts.Height > 0 {
		scale := opts.Scale
		if scale <= 0 {
			scale = 1
		}
		orientation := &proto.EmulationScreenOrientation{
			Type:  proto.EmulationScreenOrientationTypePortraitPrimary,
			Angle: 0,
		}
		if opts.Landscape {
			orientation.Type = proto.EmulationScreenOrientationTypeLandscapePrimary
			orientation.Angle = 90
		}
		if err := (proto.EmulationSetDeviceMetricsOverride{
			Width:             opts.Width,
			Height:            opts.Height,
			DeviceScaleFactor: scale,
			Mobile:            opts.IsMobile,
			ScreenOrientation: orientation,
		}).Call(p.page); err != nil {
			return fmt.Errorf("set device metrics: %w", err)
		}
	}
	if opts.HasTouch {
		if err := (proto.EmulationSetTouchEmulationEnabled{
			Enabled: true,
		}).Call(p.page); err != nil {
			return fmt.Errorf("set touch emulation: %w", err)
		}
	}
	return nil
}

func (p *ChromePage) SetExtraHeaders(headers map[string]string) error {
	h := make(proto.NetworkHeaders)
	for k, v := range headers {
		h[k] = gson.New(v)
	}
	return proto.NetworkSetExtraHTTPHeaders{Headers: h}.Call(p.page)
}

func (p *ChromePage) SetOffline(offline bool) error {
	return proto.NetworkEmulateNetworkConditions{
		Offline: offline,
	}.Call(p.page)
}

// --- PDF ---

func (p *ChromePage) PDF(landscape bool) ([]byte, error) {
	reader, err := p.page.PDF(&proto.PagePrintToPDF{
		Landscape:       landscape,
		PrintBackground: true,
	})
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}

// ---------------------------------------------------------------------------
// ChromeElement wraps *rod.Element to implement the Element interface.
// ---------------------------------------------------------------------------

type ChromeElement struct {
	el *rod.Element
}

func (e *ChromeElement) Click(button proto.InputMouseButton, clickCount int) error {
	return e.el.Click(button, clickCount)
}

func (e *ChromeElement) Hover() error {
	return e.el.Hover()
}

func (e *ChromeElement) Input(text string) error {
	return e.el.Input(text)
}

// stripUserinfo removes any userinfo (user:pass@) from a proxy URL so it's
// safe for Chrome's --proxy-server flag which doesn't support credentials.
func stripUserinfo(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.User = nil
	return parsed.String()
}
