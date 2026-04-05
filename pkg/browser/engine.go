package browser

import (
	"context"
	"errors"
	"time"

	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// ErrUnsupported is returned by Page methods that are not supported by the current engine.
var ErrUnsupported = errors.New("operation not supported by this engine")

// Engine abstracts browser lifecycle — swappable runtime (Chrome, Container, Lightpanda).
type Engine interface {
	// Launch starts or connects to a browser instance.
	Launch(opts LaunchOpts) error

	// Close shuts down the browser. Attach-mode engines skip process kill.
	Close() error

	// NewPage creates a new tab navigated to url.
	NewPage(ctx context.Context, url string) (Page, error)

	// Pages returns all open pages.
	Pages() ([]Page, error)

	// Incognito returns an isolated Engine (incognito context) for tenant isolation.
	Incognito() (Engine, error)

	// IsConnected returns true if the browser connection is alive.
	IsConnected() bool

	// Name returns the engine identifier ("chrome", "container", "lightpanda").
	Name() string
}

// Page abstracts a browser tab.
type Page interface {
	// Navigation
	Navigate(url string) error
	WaitStable(d time.Duration) error
	Info() (*PageInfo, error)
	TargetID() string
	Close() error
	Activate() error

	// Content
	GetAXTree() ([]*proto.AccessibilityAXNode, error)
	GetFrameTree() ([]FrameInfo, error)
	GetAXTreeForFrame(frameID string) ([]*proto.AccessibilityAXNode, error)
	Screenshot(fullPage bool, opts *proto.PageCaptureScreenshot) ([]byte, error)
	Eval(js string) (*proto.RuntimeRemoteObject, error)

	// Input
	KeyboardPress(key input.Key) error
	DispatchMouseEvent(typ string, x, y float64, button string, clickCount int) error
	DispatchKeyEvent(typ string, key, code, text string, modifiers int, vkCode int) error
	DispatchScrollEvent(x, y, deltaX, deltaY float64) error

	// DOM resolution (for ref-based actions)
	ResolveBackendNode(backendNodeID int) (Element, error)
	EnableDOM() error

	// Console
	SetupConsoleListener(handler func(ConsoleMessage))

	// --- Chrome extensions (return ErrUnsupported if not Chrome) ---
	GetCookies() ([]*Cookie, error)
	SetCookie(c *Cookie) error
	ClearCookies() error
	GetStorageItems(isLocal bool) (map[string]string, error)
	SetStorageItem(isLocal bool, key, value string) error
	ClearStorage(isLocal bool) error
	GetJSErrors() ([]*JSError, error)

	// Screencast
	// StartScreencast begins streaming JPEG frames. maxWidth/maxHeight control the
	// maximum output resolution (0 = use defaults 1280x720).
	// ch is added to a fan-out set; call StopScreencastCh(ch) to unsubscribe.
	StartScreencast(fps int, quality int, maxWidth int, maxHeight int, ch chan<- ScreencastFrame) error
	// StopScreencast removes all subscribers and stops CDP screencast.
	StopScreencast() error
	// StopScreencastCh removes a single subscriber. CDP is stopped only when no subscribers remain.
	StopScreencastCh(ch chan<- ScreencastFrame)

	// Emulation
	Emulate(opts EmulateOpts) error
	SetExtraHeaders(headers map[string]string) error
	SetOffline(offline bool) error

	// PDF generation
	PDF(landscape bool) ([]byte, error)
}

// Element abstracts a DOM element for ref-based interactions.
type Element interface {
	Click(button proto.InputMouseButton, clickCount int) error
	Hover() error
	Input(text string) error
}

// LaunchOpts configures browser launch.
type LaunchOpts struct {
	Headless       bool     // run in headless mode
	ProfileDir     string   // --user-data-dir for Chrome profile persistence
	RemoteURL      string   // CDP endpoint for sidecar (kill on Close)
	AttachURL      string   // CDP endpoint for existing browser (don't kill on Close)
	BinaryPath     string   // custom browser binary (e.g. Brave, Edge, Chromium)
	ProxyURL       string   // proxy server — scheme://host:port only (NO credentials)
	ProxyUser      string   // proxy auth username (used via CDP Fetch domain)
	ProxyPass      string   // proxy auth password (decrypted, used via CDP Fetch domain)
	ExtensionPaths []string // --load-extension paths for Chrome extensions
}

// ScreencastFrame is a single frame from CDP Page.screencastFrame.
type ScreencastFrame struct {
	Data      []byte // JPEG image bytes
	SessionID int    // ack ID for Page.screencastFrameAck
	Metadata  ScreencastMetadata
}

// ScreencastMetadata contains frame timing and dimensions.
type ScreencastMetadata struct {
	OffsetTop       float64 `json:"offsetTop"`
	PageScaleFactor float64 `json:"pageScaleFactor"`
	DeviceWidth     float64 `json:"deviceWidth"`
	DeviceHeight    float64 `json:"deviceHeight"`
	ScrollOffsetX   float64 `json:"scrollOffsetX"`
	ScrollOffsetY   float64 `json:"scrollOffsetY"`
	Timestamp       float64 `json:"timestamp"`
}

// EmulateOpts configures device/viewport emulation.
type EmulateOpts struct {
	UserAgent  string `json:"userAgent,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Scale      float64 `json:"scale,omitempty"`
	IsMobile   bool   `json:"isMobile,omitempty"`
	HasTouch   bool   `json:"hasTouch,omitempty"`
	Landscape  bool   `json:"landscape,omitempty"`
}

// NetworkOpts configures network behavior.
type NetworkOpts struct {
	Offline          bool              `json:"offline,omitempty"`
	LatencyMs        int               `json:"latencyMs,omitempty"`
	DownloadKbps     int               `json:"downloadKbps,omitempty"`
	UploadKbps       int               `json:"uploadKbps,omitempty"`
	ExtraHeaders     map[string]string `json:"extraHeaders,omitempty"`
	BlockedURLs      []string          `json:"blockedUrls,omitempty"`
}

// PageInfo holds basic page metadata.
type PageInfo struct {
	URL   string
	Title string
}

// Cookie for get/set operations.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
	Expires  float64 `json:"expires,omitempty"` // Unix epoch seconds
	URL      string  `json:"url,omitempty"`     // alternative to domain+path
}

// JSError represents a captured JavaScript exception.
type JSError struct {
	Text   string `json:"text"`
	URL    string `json:"url,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}
