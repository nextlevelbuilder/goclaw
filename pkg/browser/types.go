package browser

// TabInfo describes an open browser tab.
type TabInfo struct {
	TargetID   string `json:"targetId"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	AgentKey   string `json:"agentKey,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`
}

// RoleRef maps a snapshot ref (e.g. "e5") to an accessible element.
type RoleRef struct {
	Role          string `json:"role"`
	Name          string `json:"name,omitempty"`
	Nth           int    `json:"nth,omitempty"`
	BackendNodeID int    `json:"backendNodeId,omitempty"`
	FrameID       string `json:"frameId,omitempty"` // empty = main frame
}

// FrameInfo describes an iframe in the page frame tree.
type FrameInfo struct {
	FrameID      string `json:"frameId"`
	ParentID     string `json:"parentId,omitempty"`
	URL          string `json:"url"`
	Name         string `json:"name,omitempty"`
	Origin       string `json:"securityOrigin"`
	Depth        int    `json:"depth"`
	CrossOrigin  bool   `json:"crossOrigin,omitempty"`  // true = OOPIF (separate CDP target)
	OOPIFTarget  string `json:"oopifTarget,omitempty"`  // target ID for cross-origin iframes
}

// SnapshotResult is the output of a page snapshot.
type SnapshotResult struct {
	Snapshot  string             `json:"snapshot"`
	Refs      map[string]RoleRef `json:"refs"`
	URL       string             `json:"url"`
	Title     string             `json:"title"`
	TargetID  string             `json:"targetId"`
	Stats     SnapshotStats      `json:"stats"`
	Truncated bool               `json:"truncated,omitempty"`
}

// SnapshotStats contains metrics about a snapshot.
type SnapshotStats struct {
	Lines       int `json:"lines"`
	Chars       int `json:"chars"`
	Refs        int `json:"refs"`
	Interactive int `json:"interactive"`
}

// SnapshotOptions controls snapshot generation.
type SnapshotOptions struct {
	Interactive   bool   // only include interactive elements
	MaxDepth      int    // 0 = unlimited
	Compact       bool   // remove unnamed structural elements
	MaxChars      int    // truncate output (default 8000)
	Limit         int    // max AX nodes to process (default 500)
	IncludeFrames bool   // include child iframes in snapshot
	FrameID       string // snapshot a specific frame only
}

// DefaultSnapshotOptions returns sensible defaults.
func DefaultSnapshotOptions() SnapshotOptions {
	return SnapshotOptions{
		MaxChars: 8000,
		Limit:    500,
	}
}

// ActResult is the output of a browser action.
type ActResult struct {
	OK       bool   `json:"ok"`
	TargetID string `json:"targetId"`
	URL      string `json:"url,omitempty"`
	Result   string `json:"result,omitempty"`
}

// ClickOpts controls click behavior.
type ClickOpts struct {
	DoubleClick bool
	Button      string // "left", "right", "middle"
	TimeoutMs   int
}

// TypeOpts controls type behavior.
type TypeOpts struct {
	Submit    bool
	Slowly    bool
	TimeoutMs int
}

// WaitOpts controls wait behavior.
type WaitOpts struct {
	TimeMs   int
	Text     string
	TextGone string
	URL      string
	Fn       string
}

// ConsoleMessage is a captured browser console message.
type ConsoleMessage struct {
	Level   string `json:"level"` // "log", "warn", "error", "info"
	Text    string `json:"text"`
	URL     string `json:"url,omitempty"`
	LineNo  int    `json:"lineNo,omitempty"`
	ColNo   int    `json:"colNo,omitempty"`
}

// StatusInfo describes the current browser state.
type StatusInfo struct {
	Running    bool   `json:"running"`
	Tabs       int    `json:"tabs"`
	URL        string `json:"url,omitempty"`        // current tab URL
	Engine     string `json:"engine,omitempty"`      // engine name (chrome, container, lightpanda)
	Headless   *bool  `json:"headless,omitempty"`   // headless mode flag (nil when not running)
	ProfileDir string `json:"profileDir,omitempty"` // active Chrome profile directory
}
