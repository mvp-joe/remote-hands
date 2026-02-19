// Package mcptools provides a shared handler layer for remote operations
// that MCP servers (e.g. remotehands-mcp) use.
package mcptools

// BashResult contains the collected output from a bash command execution.
type BashResult struct {
	Stdout   string
	Stderr   string
	ExitCode int32
}

// FileEntry represents a file or directory in the filesystem.
type FileEntry struct {
	Path       string
	Type       string // "file" | "directory" | "symlink"
	Size       int64
	ModifiedAt int64  // Unix timestamp
	Mode       string // Permissions string (e.g., "-rw-r--r--")
}

// GrepMatch represents a single grep match result.
type GrepMatch struct {
	Path          string
	Line          int32
	Content       string
	ContextBefore []string
	ContextAfter  []string
}

// GitFileStatus represents the status of a file in a git repository.
type GitFileStatus struct {
	Path   string
	Status string // "modified" | "added" | "deleted" | "untracked"
}

// BrowserNavigateResult contains the result of navigating to a URL.
type BrowserNavigateResult struct {
	PageID string `json:"page_id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

// PageInfo describes an open browser page.
type PageInfo struct {
	PageID string `json:"page_id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

// ScreenshotResult contains a captured screenshot.
type ScreenshotResult struct {
	Image       []byte `json:"-"`     // raw bytes, not serialized directly
	ImageBase64 string `json:"image"` // base64 encoded for MCP
	ContentType string `json:"content_type"`
}

// AccessibilityNode represents a node in the accessibility tree.
type AccessibilityNode struct {
	Ref         string               `json:"ref,omitempty"`
	Role        string               `json:"role"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Value       string               `json:"value,omitempty"`
	Children    []*AccessibilityNode `json:"children,omitempty"`
	Bounds      *BoundingBox         `json:"bounds,omitempty"`
}

// BoundingBox represents element dimensions and position.
type BoundingBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// SnapshotResult contains the page snapshot data.
type SnapshotResult struct {
	DomHTML           string             `json:"dom_html,omitempty"`
	AccessibilityTree *AccessibilityNode `json:"accessibility_tree,omitempty"`
	SnapshotID        string             `json:"snapshot_id,omitempty"`
}

// BrowserAction describes a single browser action to execute.
type BrowserAction struct {
	Type       string   `json:"type"` // "click", "fill", "type", "press", etc.
	Selector   string   `json:"selector,omitempty"`
	Value      string   `json:"value,omitempty"`
	DelayMs    int32    `json:"delay_ms,omitempty"`
	Values     []string `json:"values,omitempty"`
	Button     string   `json:"button,omitempty"` // "left", "right", "middle"
	ClickCount int32    `json:"click_count,omitempty"`
	Modifiers  []string `json:"modifiers,omitempty"` // "alt", "control", "meta", "shift"
	ScrollX    float64  `json:"scroll_x,omitempty"`
	ScrollY    float64  `json:"scroll_y,omitempty"`
	WaitState  string   `json:"wait_state,omitempty"` // "visible", "hidden", "attached", "detached"
	TimeoutMs  int32    `json:"timeout_ms,omitempty"`
}

// BrowserActionResult contains the result of a single browser action.
type BrowserActionResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Value   string `json:"value,omitempty"` // for EVALUATE
}

// HttpHeader represents an HTTP header key-value pair.
type HttpHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HttpResult contains the response from an HTTP request.
type HttpResult struct {
	StatusCode int32         `json:"status_code"`
	Headers    []*HttpHeader `json:"headers"`
	Body       string        `json:"body"`
	DurationMs int64         `json:"duration_ms"`
}

// GrpcMetadata represents a gRPC metadata key-value pair.
type GrpcMetadata struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GrpcResult contains the response from a gRPC request.
type GrpcResult struct {
	ResponseBody  string          `json:"response_body"`
	Metadata      []*GrpcMetadata `json:"metadata"`
	StatusCode    int32           `json:"status_code"`
	StatusMessage string          `json:"status_message"`
}
