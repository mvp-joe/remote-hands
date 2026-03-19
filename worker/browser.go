package worker

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// BrowserManager manages a headless Chromium browser instance via Rod.
// All public methods are safe for concurrent use.
type BrowserManager struct {
	browser    *rod.Browser
	launcher   *launcher.Launcher
	pages      map[string]*rod.Page              // page_id -> page
	refMap     map[string]proto.DOMBackendNodeID // ref string -> backend node ID
	snapshotID uint64                            // incrementing snapshot counter
	lastPageID string                            // most recently created/navigated page
	mu         sync.Mutex
}

// NewBrowserManager creates a new BrowserManager with initialized maps.
func NewBrowserManager() *BrowserManager {
	return &BrowserManager{
		pages:  make(map[string]*rod.Page),
		refMap: make(map[string]proto.DOMBackendNodeID),
	}
}

// Start launches a headless Chromium instance. It is idempotent -- calling
// Start on an already-running browser returns nil.
func (bm *BrowserManager) Start(ctx context.Context) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser != nil {
		return nil
	}

	l := launcher.New().
		Bin("chromium").
		Headless(true).
		NoSandbox(true).
		Set("disable-dev-shm-usage")

	u, err := l.Context(ctx).Launch()
	if err != nil {
		return fmt.Errorf("launch chromium: %w", err)
	}

	// Use background context for the long-lived browser connection.
	// Request-scoped ctx was only needed for the Launch() call above.
	// Binding a request ctx here would cancel the browser when the
	// BrowserStart RPC completes.
	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		l.Kill()
		return fmt.Errorf("connect to chromium: %w", err)
	}

	bm.browser = browser
	bm.launcher = l
	return nil
}

// Stop shuts down the browser and cleans up all resources. It is idempotent --
// calling Stop on an already-stopped browser returns nil.
func (bm *BrowserManager) Stop(ctx context.Context) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return nil
	}

	// Close all tracked pages.
	for id, page := range bm.pages {
		_ = page.Close()
		delete(bm.pages, id)
	}

	// Close browser connection.
	if err := bm.browser.Close(); err != nil {
		// Best-effort: still kill the launcher process.
		bm.launcher.Kill()
		bm.browser = nil
		bm.launcher = nil
		bm.lastPageID = ""
		clear(bm.refMap)
		return fmt.Errorf("close browser: %w", err)
	}

	bm.launcher.Kill()
	bm.browser = nil
	bm.launcher = nil
	bm.lastPageID = ""
	clear(bm.refMap)

	return nil
}

// IsRunning reports whether the browser is currently running.
func (bm *BrowserManager) IsRunning() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.browser != nil
}

// PageResult holds the info returned by Navigate and ListPages.
type PageResult struct {
	PageID string
	URL    string
	Title  string
}

// Navigate opens a URL in a new or existing page. If pageID is nil or empty,
// a new page is created. Otherwise the existing page is navigated. The page
// is tracked and set as the last-active page. The refMap is cleared because
// navigation invalidates BackendNodeIDs.
func (bm *BrowserManager) Navigate(ctx context.Context, url string, pageID *string) (PageResult, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return PageResult{}, connect.NewError(connect.CodeFailedPrecondition, errors.New("browser not started"))
	}

	var page *rod.Page
	var err error

	if pageID != nil && *pageID != "" {
		// Navigate an existing page.
		var ok bool
		page, ok = bm.pages[*pageID]
		if !ok {
			return PageResult{}, connect.NewError(connect.CodeNotFound, fmt.Errorf("page %q not found", *pageID))
		}
		if err = page.Navigate(url); err != nil {
			return PageResult{}, fmt.Errorf("navigate page: %w", err)
		}
	} else {
		// Create a new page. browser.Page creates, navigates, but does not WaitLoad.
		page, err = bm.browser.Page(proto.TargetCreateTarget{URL: url})
		if err != nil {
			return PageResult{}, fmt.Errorf("create page: %w", err)
		}
	}

	if err = page.WaitLoad(); err != nil {
		return PageResult{}, fmt.Errorf("wait for page load: %w", err)
	}

	// Track the page by its CDP target ID.
	id := string(page.TargetID)
	bm.pages[id] = page
	bm.lastPageID = id

	// Navigation invalidates BackendNodeIDs.
	clear(bm.refMap)

	info, err := page.Info()
	if err != nil {
		return PageResult{}, fmt.Errorf("get page info: %w", err)
	}

	return PageResult{
		PageID: id,
		URL:    info.URL,
		Title:  info.Title,
	}, nil
}

// ListPages returns info for all tracked pages. The browser must be running.
func (bm *BrowserManager) ListPages(ctx context.Context) ([]PageResult, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("browser not started"))
	}

	results := make([]PageResult, 0, len(bm.pages))
	for id, page := range bm.pages {
		info, err := page.Info()
		if err != nil {
			return nil, fmt.Errorf("get page info for %s: %w", id, err)
		}
		results = append(results, PageResult{
			PageID: id,
			URL:    info.URL,
			Title:  info.Title,
		})
	}
	return results, nil
}

// ClosePage closes a tracked page by its ID and removes it from tracking.
// If the closed page was the last-active page, lastPageID is updated to
// an arbitrary remaining page or cleared if none remain.
func (bm *BrowserManager) ClosePage(ctx context.Context, pageID string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("browser not started"))
	}

	page, ok := bm.pages[pageID]
	if !ok {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("page %q not found", pageID))
	}

	if err := page.Close(); err != nil {
		return fmt.Errorf("close page: %w", err)
	}
	delete(bm.pages, pageID)

	if bm.lastPageID == pageID {
		bm.lastPageID = ""
		for id := range bm.pages {
			bm.lastPageID = id
			break
		}
	}

	return nil
}

// getPage resolves a page by ID. If pageID is nil or empty, the last-active
// page is returned. Returns a connect error with CodeNotFound if the page
// does not exist, or if no pages are open when using the default.
func (bm *BrowserManager) getPage(pageID *string) (*rod.Page, string, error) {
	id := bm.lastPageID
	if pageID != nil && *pageID != "" {
		id = *pageID
	}

	if id == "" {
		return nil, "", connect.NewError(connect.CodeNotFound, errors.New("no pages open"))
	}

	page, ok := bm.pages[id]
	if !ok {
		return nil, "", connect.NewError(connect.CodeNotFound, fmt.Errorf("page %q not found", id))
	}
	return page, id, nil
}

// knownSelectorPrefixes is the set of recognised prefix= strategies.
var knownSelectorPrefixes = map[string]bool{
	"xpath": true,
	"text":  true,
	"role":  true,
	"ref":   true,
}

// parseSelector splits a Playwright-aligned selector string into a strategy
// and value. Recognised prefixes: xpath=, text=, role=, ref=. Any unknown
// prefix (e.g. "foo=bar") or no prefix at all falls back to CSS.
func parseSelector(selector string) (strategy, value string) {
	if idx := strings.Index(selector, "="); idx > 0 {
		prefix := selector[:idx]
		if knownSelectorPrefixes[prefix] {
			return prefix, selector[idx+1:]
		}
	}
	return "css", selector
}

// roleSpecNameRe extracts the name value from a role spec like
// button[name="OK"] or button[name='OK'].
var roleSpecNameRe = regexp.MustCompile(`name=["']([^"']+)["']`)

// parseRoleSpec parses a role selector value like "button[name=\"OK\"]" into
// the role name and the accessible name. A simple value like "button" returns
// an empty name.
func parseRoleSpec(spec string) (role, name string) {
	idx := strings.Index(spec, "[")
	if idx == -1 {
		return spec, ""
	}
	role = spec[:idx]
	if m := roleSpecNameRe.FindStringSubmatch(spec[idx:]); len(m) > 1 {
		return role, m[1]
	}
	return role, ""
}

// findElement locates a DOM element on the page using the Playwright-aligned
// selector convention. Supported strategies: css (default), xpath=, text=,
// role=, ref=.
func (bm *BrowserManager) findElement(page *rod.Page, selector string) (*rod.Element, error) {
	strategy, value := parseSelector(selector)
	switch strategy {
	case "css":
		el, err := page.Element(value)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("element %q not found: %w", selector, err))
		}
		return el, nil
	case "xpath":
		el, err := page.ElementX(value)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("element xpath=%q not found: %w", value, err))
		}
		return el, nil
	case "text":
		// ElementR matches element text content against a JS regex.
		// Use "*" as CSS selector to match any element.
		el, err := page.ElementR("*", value)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("element text=%q not found: %w", value, err))
		}
		return el, nil
	case "role":
		return bm.findByRole(page, value)
	case "ref":
		return bm.resolveRef(page, value)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown selector strategy %q", strategy))
	}
}

// resolveRef looks up a BackendNodeID from the refMap (populated by the most
// recent accessibility snapshot) and resolves it to a live DOM element.
func (bm *BrowserManager) resolveRef(page *rod.Page, ref string) (*rod.Element, error) {
	nodeID, ok := bm.refMap[ref]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref %q not found in current snapshot", ref))
	}

	result, err := proto.DOMResolveNode{BackendNodeID: nodeID}.Call(page)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ref %q no longer valid: %w", ref, err))
	}

	el, err := page.ElementFromObject(result.Object)
	if err != nil {
		return nil, fmt.Errorf("resolve ref element: %w", err)
	}
	return el, nil
}

// findByRole queries the accessibility tree for an element matching the given
// role (and optionally name). The roleSpec follows the format "role" or
// "role[name=\"...\"]".
func (bm *BrowserManager) findByRole(page *rod.Page, roleSpec string) (*rod.Element, error) {
	role, name := parseRoleSpec(roleSpec)

	result, err := proto.AccessibilityQueryAXTree{
		Role:           role,
		AccessibleName: name,
	}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("query accessibility tree: %w", err)
	}
	if len(result.Nodes) == 0 {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no element with role %q found", roleSpec))
	}

	nodeID := result.Nodes[0].BackendDOMNodeID
	if nodeID == 0 {
		return nil, fmt.Errorf("role element has no backend DOM node ID")
	}

	resolved, err := proto.DOMResolveNode{BackendNodeID: nodeID}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("resolve role element: %w", err)
	}
	return page.ElementFromObject(resolved.Object)
}

// screenshotFormatMapping maps the proto ScreenshotFormat enum to Rod's CDP
// format constant and the corresponding HTTP content type.
func screenshotFormatMapping(format remotehandsv1.ScreenshotFormat) (proto.PageCaptureScreenshotFormat, string) {
	switch format {
	case remotehandsv1.ScreenshotFormat_SCREENSHOT_FORMAT_JPEG:
		return proto.PageCaptureScreenshotFormatJpeg, "image/jpeg"
	case remotehandsv1.ScreenshotFormat_SCREENSHOT_FORMAT_WEBP:
		return proto.PageCaptureScreenshotFormatWebp, "image/webp"
	default:
		return proto.PageCaptureScreenshotFormatPng, "image/png"
	}
}

// Screenshot captures a screenshot of a page or element. It returns the image
// bytes and the content type string. Parameters:
//   - pageID: target page (nil = last-active)
//   - format: image format (UNSPECIFIED defaults to PNG)
//   - fullPage: capture entire scrollable area vs viewport only
//   - selector: if set, capture only the matching element
//   - quality: compression quality for JPEG/WebP (ignored for PNG)
func (bm *BrowserManager) Screenshot(
	ctx context.Context,
	pageID *string,
	format remotehandsv1.ScreenshotFormat,
	fullPage bool,
	selector *string,
	quality *int32,
) ([]byte, string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition, errors.New("browser not started"))
	}

	page, _, err := bm.getPage(pageID)
	if err != nil {
		return nil, "", err
	}

	cdpFormat, contentType := screenshotFormatMapping(format)

	cmd := proto.PageCaptureScreenshot{
		Format: cdpFormat,
	}

	// Quality only applies to JPEG and WebP.
	if quality != nil && (cdpFormat == proto.PageCaptureScreenshotFormatJpeg || cdpFormat == proto.PageCaptureScreenshotFormatWebp) {
		q := int(*quality)
		cmd.Quality = &q
	}

	if selector != nil && *selector != "" {
		// Element screenshot: clip to the element's bounding box.
		el, err := bm.findElement(page, *selector)
		if err != nil {
			return nil, "", err
		}
		shape, err := el.Shape()
		if err != nil {
			return nil, "", fmt.Errorf("get element shape: %w", err)
		}
		box := shape.Box()
		if box == nil {
			return nil, "", connect.NewError(connect.CodeFailedPrecondition, errors.New("element has no visible bounding box"))
		}
		cmd.Clip = &proto.PageViewport{
			X:      box.X,
			Y:      box.Y,
			Width:  box.Width,
			Height: box.Height,
			Scale:  1,
		}
	} else if fullPage {
		// Full-page screenshot: clip to the full content size.
		metrics, err := proto.PageGetLayoutMetrics{}.Call(page)
		if err != nil {
			return nil, "", fmt.Errorf("get layout metrics: %w", err)
		}
		// Prefer CSS content size; fall back to deprecated ContentSize.
		cs := metrics.CSSContentSize
		if cs == nil {
			cs = metrics.ContentSize
		}
		if cs != nil {
			cmd.Clip = &proto.PageViewport{
				X:      0,
				Y:      0,
				Width:  cs.Width,
				Height: cs.Height,
				Scale:  1,
			}
			cmd.CaptureBeyondViewport = true
		}
	}
	// else: viewport screenshot — no clip needed.

	result, err := cmd.Call(page)
	if err != nil {
		return nil, "", fmt.Errorf("capture screenshot: %w", err)
	}

	return result.Data, contentType, nil
}

// SnapshotResult holds the results of a page snapshot request.
type SnapshotResult struct {
	DomHTML           *string
	AccessibilityTree *remotehandsv1.AccessibilityNode
	SnapshotID        *string
}

// Snapshot captures DOM HTML and/or the accessibility tree for a page.
// If types is empty, both DOM and accessibility are returned.
func (bm *BrowserManager) Snapshot(
	ctx context.Context,
	pageID *string,
	types []remotehandsv1.SnapshotType,
	includeBounds bool,
) (*SnapshotResult, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("browser not started"))
	}

	page, _, err := bm.getPage(pageID)
	if err != nil {
		return nil, err
	}

	// Determine which snapshot types were requested.
	wantDOM := false
	wantA11y := false
	for _, t := range types {
		switch t {
		case remotehandsv1.SnapshotType_SNAPSHOT_TYPE_DOM:
			wantDOM = true
		case remotehandsv1.SnapshotType_SNAPSHOT_TYPE_ACCESSIBILITY:
			wantA11y = true
		}
	}
	// If nothing specific requested, return both.
	if !wantDOM && !wantA11y {
		wantDOM = true
		wantA11y = true
	}

	result := &SnapshotResult{}

	if wantDOM {
		html, err := page.HTML()
		if err != nil {
			return nil, fmt.Errorf("get page HTML: %w", err)
		}
		result.DomHTML = &html
	}

	if wantA11y {
		tree, err := bm.buildAccessibilityTree(page, includeBounds)
		if err != nil {
			return nil, fmt.Errorf("build accessibility tree: %w", err)
		}
		result.AccessibilityTree = tree

		bm.snapshotID++
		sid := strconv.FormatUint(bm.snapshotID, 10)
		result.SnapshotID = &sid
	}

	return result, nil
}

// buildAccessibilityTree fetches the full accessibility tree via CDP,
// converts the flat node list into a tree of proto AccessibilityNode messages,
// and populates bm.refMap with ref -> BackendDOMNodeID mappings.
func (bm *BrowserManager) buildAccessibilityTree(
	page *rod.Page,
	includeBounds bool,
) (*remotehandsv1.AccessibilityNode, error) {
	axResult, err := proto.AccessibilityGetFullAXTree{}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("get accessibility tree: %w", err)
	}

	// Clear the refMap — refs are scoped to the latest snapshot.
	clear(bm.refMap)

	// Build a lookup from AXNodeID -> converted proto node, and track
	// parent-child relationships.
	type nodeEntry struct {
		axNode *proto.AccessibilityAXNode
		pbNode *remotehandsv1.AccessibilityNode
	}

	nodesByID := make(map[proto.AccessibilityAXNodeID]*nodeEntry, len(axResult.Nodes))

	// First pass: convert each non-ignored AX node to a proto message.
	for _, axNode := range axResult.Nodes {
		if axNode.Ignored {
			continue
		}

		pbNode := &remotehandsv1.AccessibilityNode{}

		if axNode.Role != nil && !axNode.Role.Value.Nil() {
			pbNode.Role = axNode.Role.Value.Str()
		}
		if axNode.Name != nil && !axNode.Name.Value.Nil() {
			pbNode.Name = axNode.Name.Value.Str()
		}
		if axNode.Description != nil && !axNode.Description.Value.Nil() {
			desc := axNode.Description.Value.Str()
			if desc != "" {
				pbNode.Description = &desc
			}
		}
		if axNode.Value != nil && !axNode.Value.Value.Nil() {
			val := axNode.Value.Value.Str()
			if val != "" {
				pbNode.Value = &val
			}
		}

		// Assign ref for nodes with a backend DOM node ID.
		if axNode.BackendDOMNodeID != 0 {
			ref := fmt.Sprintf("e%d", axNode.BackendDOMNodeID)
			pbNode.Ref = ref
			bm.refMap[ref] = axNode.BackendDOMNodeID
		}

		nodesByID[axNode.NodeID] = &nodeEntry{
			axNode: axNode,
			pbNode: pbNode,
		}
	}

	// Second pass: wire up parent-child relationships.
	var root *remotehandsv1.AccessibilityNode
	for _, entry := range nodesByID {
		if entry.axNode.ParentID == "" {
			root = entry.pbNode
			continue
		}
		parent, ok := nodesByID[entry.axNode.ParentID]
		if ok {
			parent.pbNode.Children = append(parent.pbNode.Children, entry.pbNode)
		}
	}

	// Third pass (optional): resolve bounding boxes.
	if includeBounds {
		for _, entry := range nodesByID {
			if entry.axNode.BackendDOMNodeID == 0 {
				continue
			}
			boxResult, err := proto.DOMGetBoxModel{
				BackendNodeID: entry.axNode.BackendDOMNodeID,
			}.Call(page)
			if err != nil {
				// Some nodes (e.g. text nodes, off-screen) may not have a box model.
				continue
			}
			if boxResult.Model != nil {
				entry.pbNode.Bounds = &remotehandsv1.BoundingBox{
					X:      float64(boxResult.Model.Content[0]),
					Y:      float64(boxResult.Model.Content[1]),
					Width:  float64(boxResult.Model.Width),
					Height: float64(boxResult.Model.Height),
				}
			}
		}
	}

	if root == nil {
		// No non-ignored root found; return an empty tree.
		return &remotehandsv1.AccessibilityNode{Role: "none"}, nil
	}

	return root, nil
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

// Act executes a sequence of browser actions on a page. It uses stop-on-first-
// error semantics: if any action fails, remaining actions are marked as
// skipped.
func (bm *BrowserManager) Act(
	ctx context.Context,
	pageID *string,
	actions []*remotehandsv1.BrowserAction,
) ([]*remotehandsv1.BrowserActionResult, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("browser not started"))
	}

	page, _, err := bm.getPage(pageID)
	if err != nil {
		return nil, err
	}

	results := make([]*remotehandsv1.BrowserActionResult, len(actions))
	for i, action := range actions {
		results[i] = bm.executeAction(ctx, page, action)
		if !results[i].Success {
			skippedMsg := "skipped due to previous error"
			for j := i + 1; j < len(actions); j++ {
				results[j] = &remotehandsv1.BrowserActionResult{
					Success: false,
					Error:   &skippedMsg,
				}
			}
			break
		}
	}
	return results, nil
}

// actionSuccess returns a successful BrowserActionResult with an optional
// value.
func actionSuccess(value *string) *remotehandsv1.BrowserActionResult {
	return &remotehandsv1.BrowserActionResult{Success: true, Value: value}
}

// actionError returns a failed BrowserActionResult.
func actionError(err error) *remotehandsv1.BrowserActionResult {
	msg := err.Error()
	return &remotehandsv1.BrowserActionResult{Success: false, Error: &msg}
}

// executeAction dispatches a single browser action to its implementation.
func (bm *BrowserManager) executeAction(
	ctx context.Context,
	page *rod.Page,
	action *remotehandsv1.BrowserAction,
) *remotehandsv1.BrowserActionResult {
	switch action.Type {
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_CLICK:
		return bm.actionClick(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_FILL:
		return bm.actionFill(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_TYPE:
		return bm.actionType(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_PRESS:
		return bm.actionPress(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_SELECT_OPTION:
		return bm.actionSelectOption(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_CHECK:
		return bm.actionCheck(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_UNCHECK:
		return bm.actionUncheck(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_HOVER:
		return bm.actionHover(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_SCROLL:
		return bm.actionScroll(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_FOCUS:
		return bm.actionFocus(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_EVALUATE:
		return bm.actionEvaluate(page, action)
	case remotehandsv1.BrowserActionType_BROWSER_ACTION_WAIT_FOR_SELECTOR:
		return bm.actionWaitForSelector(ctx, page, action)
	default:
		return actionError(fmt.Errorf("unknown action type: %v", action.Type))
	}
}

// requireSelector returns the selector from an action, or an error if absent.
func requireSelector(action *remotehandsv1.BrowserAction) (string, error) {
	if action.Selector == nil || *action.Selector == "" {
		return "", fmt.Errorf("selector is required for %v", action.Type)
	}
	return *action.Selector, nil
}

// actionClick clicks an element. Supports button, click_count, and modifiers.
func (bm *BrowserManager) actionClick(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	el, err := bm.findElement(page, selector)
	if err != nil {
		return actionError(err)
	}

	button := proto.InputMouseButtonLeft
	if action.Button != nil {
		switch *action.Button {
		case remotehandsv1.MouseButton_MOUSE_BUTTON_RIGHT:
			button = proto.InputMouseButtonRight
		case remotehandsv1.MouseButton_MOUSE_BUTTON_MIDDLE:
			button = proto.InputMouseButtonMiddle
		}
	}

	clickCount := 1
	if action.ClickCount != nil && *action.ClickCount > 0 {
		clickCount = int(*action.ClickCount)
	}

	if err := el.Click(button, clickCount); err != nil {
		return actionError(fmt.Errorf("click: %w", err))
	}
	return actionSuccess(nil)
}

// actionFill clears an input element and types the new value.
func (bm *BrowserManager) actionFill(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	el, err := bm.findElement(page, selector)
	if err != nil {
		return actionError(err)
	}

	// Clear existing content then input the new value.
	if err := el.SelectAllText(); err != nil {
		return actionError(fmt.Errorf("fill select-all: %w", err))
	}

	value := ""
	if action.Value != nil {
		value = *action.Value
	}
	if err := el.Input(value); err != nil {
		return actionError(fmt.Errorf("fill input: %w", err))
	}
	return actionSuccess(nil)
}

// actionType types text into an element, optionally with a delay between
// keystrokes.
func (bm *BrowserManager) actionType(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	el, err := bm.findElement(page, selector)
	if err != nil {
		return actionError(err)
	}

	text := ""
	if action.Value != nil {
		text = *action.Value
	}

	var delay time.Duration
	if action.DelayMs != nil && *action.DelayMs > 0 {
		delay = time.Duration(*action.DelayMs) * time.Millisecond
	}

	if delay == 0 {
		// Fast path: type all at once.
		if err := el.Input(text); err != nil {
			return actionError(fmt.Errorf("type: %w", err))
		}
	} else {
		// Character-by-character with delay.
		for _, ch := range text {
			k := mapRuneToKey(ch)
			if k == 0 {
				// Printable character not in keymap — use Input for single char.
				if err := el.Input(string(ch)); err != nil {
					return actionError(fmt.Errorf("type char %q: %w", ch, err))
				}
			} else {
				if err := el.Type(k); err != nil {
					return actionError(fmt.Errorf("type key %q: %w", ch, err))
				}
			}
			time.Sleep(delay)
		}
	}
	return actionSuccess(nil)
}

// actionPress presses a key combination (e.g. "Enter", "Control+a").
func (bm *BrowserManager) actionPress(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	keyName := ""
	if action.Value != nil {
		keyName = *action.Value
	}
	if keyName == "" {
		return actionError(fmt.Errorf("key name is required for PRESS"))
	}

	// Build the key sequence: modifiers + key.
	ka := page.KeyActions()

	// Press modifier keys from the action's modifier list.
	for _, mod := range action.Modifiers {
		if k := mapModifierToKey(mod); k != 0 {
			ka.Press(k)
		}
	}

	// Parse the key name — may be a combo like "Control+a".
	parts := strings.Split(keyName, "+")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		k := mapKeyName(part)
		if k == 0 {
			return actionError(fmt.Errorf("unknown key %q", part))
		}
		ka.Type(k)
	}

	// Release modifiers in reverse.
	for i := len(action.Modifiers) - 1; i >= 0; i-- {
		if k := mapModifierToKey(action.Modifiers[i]); k != 0 {
			ka.Release(k)
		}
	}

	if err := ka.Do(); err != nil {
		return actionError(fmt.Errorf("press: %w", err))
	}
	return actionSuccess(nil)
}

// actionSelectOption selects options in a <select> element by their text
// content.
func (bm *BrowserManager) actionSelectOption(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	el, err := bm.findElement(page, selector)
	if err != nil {
		return actionError(err)
	}

	if err := el.Select(action.Values, true, rod.SelectorTypeText); err != nil {
		return actionError(fmt.Errorf("select option: %w", err))
	}
	return actionSuccess(nil)
}

// actionCheck ensures a checkbox or radio is checked. If already checked, it
// is a no-op.
func (bm *BrowserManager) actionCheck(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	return bm.setChecked(page, action, true)
}

// actionUncheck ensures a checkbox is unchecked. If already unchecked, it is
// a no-op.
func (bm *BrowserManager) actionUncheck(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	return bm.setChecked(page, action, false)
}

// setChecked clicks the element if its checked state doesn't match the desired
// value.
func (bm *BrowserManager) setChecked(page *rod.Page, action *remotehandsv1.BrowserAction, want bool) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	el, err := bm.findElement(page, selector)
	if err != nil {
		return actionError(err)
	}

	checked, err := el.Property("checked")
	if err != nil {
		return actionError(fmt.Errorf("get checked property: %w", err))
	}

	isChecked := checked.Bool()
	if isChecked != want {
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return actionError(fmt.Errorf("toggle checked: %w", err))
		}
	}
	return actionSuccess(nil)
}

// actionHover moves the mouse over an element.
func (bm *BrowserManager) actionHover(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	el, err := bm.findElement(page, selector)
	if err != nil {
		return actionError(err)
	}

	if err := el.Hover(); err != nil {
		return actionError(fmt.Errorf("hover: %w", err))
	}
	return actionSuccess(nil)
}

// actionScroll scrolls an element into view or scrolls the page by offsets.
func (bm *BrowserManager) actionScroll(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	if action.Selector != nil && *action.Selector != "" {
		el, err := bm.findElement(page, *action.Selector)
		if err != nil {
			return actionError(err)
		}
		if err := el.ScrollIntoView(); err != nil {
			return actionError(fmt.Errorf("scroll into view: %w", err))
		}
		return actionSuccess(nil)
	}

	// Page-level scroll by offsets.
	scrollX := 0.0
	scrollY := 0.0
	if action.ScrollX != nil {
		scrollX = *action.ScrollX
	}
	if action.ScrollY != nil {
		scrollY = *action.ScrollY
	}
	if err := page.Mouse.Scroll(scrollX, scrollY, 0); err != nil {
		return actionError(fmt.Errorf("scroll: %w", err))
	}
	return actionSuccess(nil)
}

// actionFocus sets focus on an element.
func (bm *BrowserManager) actionFocus(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	el, err := bm.findElement(page, selector)
	if err != nil {
		return actionError(err)
	}

	if err := el.Focus(); err != nil {
		return actionError(fmt.Errorf("focus: %w", err))
	}
	return actionSuccess(nil)
}

// actionEvaluate executes JavaScript on the page and returns the result as a
// JSON string. If value is a function (arrow or function keyword), it is
// passed directly to Rod's Eval. Otherwise it is auto-wrapped as
// () => { return (<expression>); } so that raw expressions like
// "document.title" work without requiring the caller to wrap them.
func (bm *BrowserManager) actionEvaluate(page *rod.Page, action *remotehandsv1.BrowserAction) *remotehandsv1.BrowserActionResult {
	js := ""
	if action.Value != nil {
		js = *action.Value
	}
	if js == "" {
		return actionError(fmt.Errorf("JavaScript expression is required for EVALUATE"))
	}

	if !isJSFunction(js) {
		js = "() => { return (" + js + "); }"
	}

	result, err := page.Eval(js)
	if err != nil {
		return actionError(fmt.Errorf("evaluate: %w", err))
	}

	val := result.Value.JSON("", "")
	return actionSuccess(&val)
}

// actionWaitForSelector waits for an element to reach a specified state.
func (bm *BrowserManager) actionWaitForSelector(
	ctx context.Context,
	page *rod.Page,
	action *remotehandsv1.BrowserAction,
) *remotehandsv1.BrowserActionResult {
	selector, err := requireSelector(action)
	if err != nil {
		return actionError(err)
	}

	timeout := 30 * time.Second
	if action.TimeoutMs != nil && *action.TimeoutMs > 0 {
		timeout = time.Duration(*action.TimeoutMs) * time.Millisecond
	}

	state := remotehandsv1.WaitForState_WAIT_FOR_STATE_VISIBLE
	if action.WaitState != nil {
		state = *action.WaitState
	}

	strategy, value := parseSelector(selector)

	switch state {
	case remotehandsv1.WaitForState_WAIT_FOR_STATE_VISIBLE,
		remotehandsv1.WaitForState_WAIT_FOR_STATE_ATTACHED,
		remotehandsv1.WaitForState_WAIT_FOR_STATE_UNSPECIFIED:
		// Wait for the element to appear. Rod's Element/ElementX/ElementR
		// already retry until timeout.
		timedPage := page.Timeout(timeout)
		switch strategy {
		case "css":
			_, err = timedPage.Element(value)
		case "xpath":
			_, err = timedPage.ElementX(value)
		case "text":
			_, err = timedPage.ElementR("*", value)
		case "role":
			// No built-in timeout for role queries — poll manually.
			err = pollUntil(ctx, timeout, 100*time.Millisecond, func() error {
				_, e := bm.findByRole(page, value)
				return e
			})
		case "ref":
			_, err = bm.resolveRef(page, value)
		default:
			err = fmt.Errorf("unknown selector strategy %q", strategy)
		}

	case remotehandsv1.WaitForState_WAIT_FOR_STATE_HIDDEN:
		// Poll until the element is not visible.
		err = pollUntil(ctx, timeout, 100*time.Millisecond, func() error {
			el, findErr := bm.findElement(page, selector)
			if findErr != nil {
				// Element not found — counts as hidden.
				return nil
			}
			visible, visErr := el.Visible()
			if visErr != nil || !visible {
				return nil
			}
			return fmt.Errorf("element %q still visible", selector)
		})

	case remotehandsv1.WaitForState_WAIT_FOR_STATE_DETACHED:
		// Poll until the element is gone from the DOM.
		err = pollUntil(ctx, timeout, 100*time.Millisecond, func() error {
			_, findErr := bm.findElement(page, selector)
			if findErr != nil {
				return nil // Detached.
			}
			return fmt.Errorf("element %q still attached", selector)
		})
	}

	if err != nil {
		return actionError(fmt.Errorf("wait for selector: %w", err))
	}
	return actionSuccess(nil)
}

// pollUntil calls fn repeatedly until it returns nil, the timeout expires, or
// the context is cancelled.
func pollUntil(ctx context.Context, timeout, interval time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := fn(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %v", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// isJSFunction returns true if js looks like a function expression or arrow
// function. Used to decide whether to auto-wrap raw expressions for Eval.
func isJSFunction(js string) bool {
	trimmed := strings.TrimSpace(js)
	return strings.HasPrefix(trimmed, "function") ||
		strings.HasPrefix(trimmed, "(") ||
		strings.HasPrefix(trimmed, "async") ||
		strings.Contains(strings.SplitN(trimmed, "\n", 2)[0], "=>")
}

// ---------------------------------------------------------------------------
// Key mapping helpers
// ---------------------------------------------------------------------------

// mapModifierToKey converts a proto Modifier enum to a Rod input.Key.
func mapModifierToKey(mod remotehandsv1.Modifier) input.Key {
	switch mod {
	case remotehandsv1.Modifier_MODIFIER_ALT:
		return input.AltLeft
	case remotehandsv1.Modifier_MODIFIER_CONTROL:
		return input.ControlLeft
	case remotehandsv1.Modifier_MODIFIER_META:
		return input.MetaLeft
	case remotehandsv1.Modifier_MODIFIER_SHIFT:
		return input.ShiftLeft
	default:
		return 0
	}
}

// keyNameMap maps Playwright-style key names to Rod input.Key values.
var keyNameMap = map[string]input.Key{
	// Navigation / editing
	"Backspace":  input.Backspace,
	"Tab":        input.Tab,
	"Enter":      input.Enter,
	"Escape":     input.Escape,
	"Delete":     input.Delete,
	"Insert":     input.Insert,
	"Home":       input.Home,
	"End":        input.End,
	"PageUp":     input.PageUp,
	"PageDown":   input.PageDown,
	"ArrowUp":    input.ArrowUp,
	"ArrowDown":  input.ArrowDown,
	"ArrowLeft":  input.ArrowLeft,
	"ArrowRight": input.ArrowRight,
	"Space":      input.Space,

	// Modifiers
	"Shift":      input.ShiftLeft,
	"ShiftLeft":  input.ShiftLeft,
	"ShiftRight": input.ShiftRight,
	"Control":    input.ControlLeft,
	"Alt":        input.AltLeft,
	"Meta":       input.MetaLeft,
	"CapsLock":   input.CapsLock,

	// Function keys
	"F1":  input.F1,
	"F2":  input.F2,
	"F3":  input.F3,
	"F4":  input.F4,
	"F5":  input.F5,
	"F6":  input.F6,
	"F7":  input.F7,
	"F8":  input.F8,
	"F9":  input.F9,
	"F10": input.F10,
	"F11": input.F11,
	"F12": input.F12,
}

// mapKeyName converts a Playwright-style key name to a Rod input.Key. It
// first checks the known name map, then falls back to interpreting single
// characters via mapRuneToKey.
func mapKeyName(name string) input.Key {
	if k, ok := keyNameMap[name]; ok {
		return k
	}
	// Single character — try rune lookup.
	runes := []rune(name)
	if len(runes) == 1 {
		return mapRuneToKey(runes[0])
	}
	return 0
}

// mapRuneToKey maps a single Unicode rune to the corresponding Rod input.Key.
// Returns 0 for unmapped characters.
func mapRuneToKey(r rune) input.Key {
	switch {
	case r >= 'a' && r <= 'z':
		// input.KeyA through input.KeyZ are sequential rune values.
		return input.KeyA + input.Key(r-'a')
	case r >= 'A' && r <= 'Z':
		return input.KeyA + input.Key(r-'A')
	case r >= '0' && r <= '9':
		return input.Digit0 + input.Key(r-'0')
	case r == ' ':
		return input.Space
	case r == '\t':
		return input.Tab
	case r == '\r' || r == '\n':
		return input.Enter
	}
	return 0
}
