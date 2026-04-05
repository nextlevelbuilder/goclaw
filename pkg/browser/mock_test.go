package browser

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

var mockIncognitoCounter atomic.Int64

// mockEngine implements Engine for testing without a real browser.
type mockEngine struct {
	mu          sync.Mutex
	connected   bool
	name        string
	pages       []Page
	launched    LaunchOpts
	closeCalled bool
}

func newMockEngine() *mockEngine {
	return &mockEngine{name: "mock", connected: true}
}

func (e *mockEngine) Launch(opts LaunchOpts) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.launched = opts
	e.connected = true
	return nil
}

func (e *mockEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.connected = false
	e.closeCalled = true
	return nil
}

func (e *mockEngine) NewPage(ctx context.Context, url string) (Page, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.connected {
		return nil, fmt.Errorf("not connected")
	}
	p := newMockPage(fmt.Sprintf("%s-tab-%d", e.name, len(e.pages)+1), url)
	e.pages = append(e.pages, p)
	return p, nil
}

func (e *mockEngine) Pages() ([]Page, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.connected {
		return nil, fmt.Errorf("not connected")
	}
	result := make([]Page, len(e.pages))
	copy(result, e.pages)
	return result, nil
}

func (e *mockEngine) Incognito() (Engine, error) {
	n := mockIncognitoCounter.Add(1)
	child := newMockEngine()
	child.name = fmt.Sprintf("%s-incognito-%d", e.name, n)
	return child, nil
}

func (e *mockEngine) IsConnected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connected
}

func (e *mockEngine) Name() string { return e.name }

// addPage adds a pre-built page for testing.
func (e *mockEngine) addPage(p Page) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pages = append(e.pages, p)
}

// ---------------------------------------------------------------------------
// mockPage implements Page for testing.
// ---------------------------------------------------------------------------

type mockPage struct {
	mu           sync.Mutex
	targetID     string
	url          string
	title        string
	closed       bool
	activated    bool
	domEnabled   bool
	stableWaited bool
	cookies      []*Cookie
	storage      map[string]map[string]string // "local"/"session" → key → value
	jsErrors     []*JSError
	evalResults  map[string]*proto.RuntimeRemoteObject
	consoleCb    func(ConsoleMessage)
	elements     map[int]*mockElement // backendNodeID → element
}

func newMockPage(targetID, url string) *mockPage {
	return &mockPage{
		targetID: targetID,
		url:      url,
		title:    "Mock Page",
		cookies:  []*Cookie{},
		storage: map[string]map[string]string{
			"local":   {},
			"session": {},
		},
		jsErrors:    []*JSError{},
		evalResults: map[string]*proto.RuntimeRemoteObject{},
		elements:    map[int]*mockElement{},
	}
}

func (p *mockPage) Navigate(url string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.url = url
	return nil
}

func (p *mockPage) WaitStable(d time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stableWaited = true
	return nil
}

func (p *mockPage) Info() (*PageInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &PageInfo{URL: p.url, Title: p.title}, nil
}

func (p *mockPage) TargetID() string { return p.targetID }

func (p *mockPage) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *mockPage) Activate() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activated = true
	return nil
}

func (p *mockPage) GetAXTree() ([]*proto.AccessibilityAXNode, error) {
	// Return minimal AX tree for snapshot tests
	return []*proto.AccessibilityAXNode{}, nil
}

func (p *mockPage) GetFrameTree() ([]FrameInfo, error) {
	return []FrameInfo{
		{FrameID: "main", URL: p.url, Name: "", Origin: p.url, Depth: 0},
	}, nil
}

func (p *mockPage) GetAXTreeForFrame(frameID string) ([]*proto.AccessibilityAXNode, error) {
	return []*proto.AccessibilityAXNode{}, nil
}

func (p *mockPage) Screenshot(fullPage bool, opts *proto.PageCaptureScreenshot) ([]byte, error) {
	return []byte("fake-png-data"), nil
}

func (p *mockPage) Eval(js string) (*proto.RuntimeRemoteObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if result, ok := p.evalResults[js]; ok {
		return result, nil
	}
	// Default: return empty value
	return &proto.RuntimeRemoteObject{}, nil
}

func (p *mockPage) KeyboardPress(key input.Key) error { return nil }

func (p *mockPage) DispatchMouseEvent(typ string, x, y float64, button string, clickCount int) error {
	return nil
}

func (p *mockPage) DispatchKeyEvent(typ string, key, code, text string, modifiers int, vkCode int) error {
	return nil
}

func (p *mockPage) DispatchScrollEvent(x, y, deltaX, deltaY float64) error {
	return nil
}

func (p *mockPage) ResolveBackendNode(backendNodeID int) (Element, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if el, ok := p.elements[backendNodeID]; ok {
		return el, nil
	}
	return nil, fmt.Errorf("node %d not found", backendNodeID)
}

func (p *mockPage) EnableDOM() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.domEnabled = true
	return nil
}

func (p *mockPage) SetupConsoleListener(handler func(ConsoleMessage)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consoleCb = handler
}

// --- Chrome extensions ---

func (p *mockPage) GetCookies() ([]*Cookie, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]*Cookie, len(p.cookies))
	copy(result, p.cookies)
	return result, nil
}

func (p *mockPage) SetCookie(c *Cookie) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cookies = append(p.cookies, c)
	return nil
}

func (p *mockPage) ClearCookies() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cookies = nil
	return nil
}

func (p *mockPage) GetStorageItems(isLocal bool) (map[string]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := "session"
	if isLocal {
		key = "local"
	}
	result := make(map[string]string)
	maps.Copy(result, p.storage[key])
	return result, nil
}

func (p *mockPage) SetStorageItem(isLocal bool, k, v string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := "session"
	if isLocal {
		key = "local"
	}
	p.storage[key][k] = v
	return nil
}

func (p *mockPage) ClearStorage(isLocal bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := "session"
	if isLocal {
		key = "local"
	}
	p.storage[key] = map[string]string{}
	return nil
}

func (p *mockPage) GetJSErrors() ([]*JSError, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]*JSError, len(p.jsErrors))
	copy(result, p.jsErrors)
	p.jsErrors = nil
	return result, nil
}

// --- Screencast ---

func (p *mockPage) StartScreencast(fps int, quality int, maxWidth int, maxHeight int, ch chan<- ScreencastFrame) error {
	return nil
}

func (p *mockPage) StopScreencast() error {
	return nil
}

func (p *mockPage) StopScreencastCh(ch chan<- ScreencastFrame) {}

// --- Emulation ---

func (p *mockPage) Emulate(opts EmulateOpts) error {
	return nil
}

func (p *mockPage) SetExtraHeaders(headers map[string]string) error {
	return nil
}

func (p *mockPage) SetOffline(offline bool) error {
	return nil
}

// --- PDF ---

func (p *mockPage) PDF(landscape bool) ([]byte, error) {
	return []byte("fake-pdf-data"), nil
}

// emitConsole simulates a console message.
func (p *mockPage) emitConsole(msg ConsoleMessage) {
	p.mu.Lock()
	cb := p.consoleCb
	p.mu.Unlock()
	if cb != nil {
		cb(msg)
	}
}

// addElement registers a mock element at a backendNodeID.
func (p *mockPage) addElement(backendNodeID int, el *mockElement) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.elements[backendNodeID] = el
}

// ---------------------------------------------------------------------------
// mockElement implements Element for testing.
// ---------------------------------------------------------------------------

type mockElement struct {
	clicked    bool
	hovered    bool
	inputText  string
	lastButton proto.InputMouseButton
	lastCount  int
}

func (e *mockElement) Click(button proto.InputMouseButton, clickCount int) error {
	e.clicked = true
	e.lastButton = button
	e.lastCount = clickCount
	return nil
}

func (e *mockElement) Hover() error {
	e.hovered = true
	return nil
}

func (e *mockElement) Input(text string) error {
	e.inputText += text
	return nil
}

// ---------------------------------------------------------------------------
// helpers: inject mock engine into Manager for testing
// ---------------------------------------------------------------------------

func newTestManager(eng *mockEngine) *Manager {
	m := New(WithIdleTimeout(0)) // disable reaper
	m.engine = eng
	return m
}
