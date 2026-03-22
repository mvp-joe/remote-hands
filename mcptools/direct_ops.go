package mcptools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	workerv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/mvp-joe/remote-hands/gen/remotehands/v1/remotehandsv1connect"
)

// DirectOps implements Ops by calling the remotehands server directly.
type DirectOps struct {
	client remotehandsv1connect.ServiceClient
}

// NewDirectOps creates a new DirectOps with the given worker client.
func NewDirectOps(client remotehandsv1connect.ServiceClient) *DirectOps {
	return &DirectOps{client: client}
}

func (d *DirectOps) RunBash(ctx context.Context, cmd string, timeoutMs int32, env map[string]string, workingDir string) (*BashResult, error) {
	stream, err := d.client.RunBash(ctx, connect.NewRequest(&workerv1.RunBashRequest{
		Command:    cmd,
		TimeoutMs:  timeoutMs,
		Env:        env,
		WorkingDir: workingDir,
	}))
	if err != nil {
		return nil, fmt.Errorf("run bash: %w", err)
	}

	result := &BashResult{}
	var stdout, stderr strings.Builder

	for stream.Receive() {
		event := stream.Msg()
		switch e := event.Event.(type) {
		case *workerv1.RunBashEvent_Stdout:
			stdout.WriteString(e.Stdout)
		case *workerv1.RunBashEvent_Stderr:
			stderr.WriteString(e.Stderr)
		case *workerv1.RunBashEvent_ExitCode:
			result.ExitCode = e.ExitCode
		}
	}

	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("run bash stream: %w", err)
	}

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	return result, nil
}

func (d *DirectOps) ReadFile(ctx context.Context, path string, offset, limit int64) ([]byte, error) {
	resp, err := d.client.ReadFile(ctx, connect.NewRequest(&workerv1.ReadFileRequest{
		Path:   path,
		Offset: offset,
		Limit:  limit,
	}))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return resp.Msg.Content, nil
}

func (d *DirectOps) WriteFile(ctx context.Context, path string, content []byte, mode int32) error {
	_, err := d.client.WriteFile(ctx, connect.NewRequest(&workerv1.WriteFileRequest{
		Path:    path,
		Content: content,
		Mode:    mode,
	}))
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func (d *DirectOps) DeleteFile(ctx context.Context, path string, recursive bool) error {
	_, err := d.client.DeleteFile(ctx, connect.NewRequest(&workerv1.DeleteFileRequest{
		Path:      path,
		Recursive: recursive,
	}))
	if err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

func (d *DirectOps) ListFiles(ctx context.Context, path string, recursive bool) ([]*FileEntry, error) {
	resp, err := d.client.ListFiles(ctx, connect.NewRequest(&workerv1.ListFilesRequest{
		Path:      path,
		Recursive: recursive,
	}))
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}

	files := make([]*FileEntry, len(resp.Msg.Files))
	for i, f := range resp.Msg.Files {
		files[i] = &FileEntry{
			Path:       f.Path,
			Type:       f.Type,
			Size:       f.Size,
			ModifiedAt: f.ModifiedAt,
			Mode:       f.Mode,
		}
	}
	return files, nil
}

func (d *DirectOps) Glob(ctx context.Context, pattern string, basePath string) ([]string, error) {
	resp, err := d.client.Glob(ctx, connect.NewRequest(&workerv1.GlobRequest{
		Pattern: pattern,
		Path:    basePath,
	}))
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}
	return resp.Msg.Matches, nil
}

func (d *DirectOps) Grep(ctx context.Context, pattern, path, glob string, ignoreCase bool, contextLines int32) ([]*GrepMatch, error) {
	resp, err := d.client.Grep(ctx, connect.NewRequest(&workerv1.GrepRequest{
		Pattern:      pattern,
		Path:         path,
		Glob:         glob,
		IgnoreCase:   ignoreCase,
		ContextLines: contextLines,
	}))
	if err != nil {
		return nil, fmt.Errorf("grep: %w", err)
	}

	matches := make([]*GrepMatch, len(resp.Msg.Matches))
	for i, m := range resp.Msg.Matches {
		matches[i] = &GrepMatch{
			Path:          m.Path,
			Line:          m.Line,
			Content:       m.Content,
			ContextBefore: m.ContextBefore,
			ContextAfter:  m.ContextAfter,
		}
	}
	return matches, nil
}

func (d *DirectOps) GitStatus(ctx context.Context, path string) ([]*GitFileStatus, error) {
	resp, err := d.client.GitStatus(ctx, connect.NewRequest(&workerv1.GitStatusRequest{
		Path: path,
	}))
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	files := make([]*GitFileStatus, len(resp.Msg.Files))
	for i, f := range resp.Msg.Files {
		files[i] = &GitFileStatus{
			Path:   f.Path,
			Status: f.Status,
		}
	}
	return files, nil
}

func (d *DirectOps) GitDiff(ctx context.Context, path string, staged bool) (string, error) {
	resp, err := d.client.GitDiff(ctx, connect.NewRequest(&workerv1.GitDiffRequest{
		Path:   path,
		Staged: staged,
	}))
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return resp.Msg.Diff, nil
}

func (d *DirectOps) GitCommit(ctx context.Context, message string, files []string) (string, error) {
	resp, err := d.client.GitCommit(ctx, connect.NewRequest(&workerv1.GitCommitRequest{
		Message: message,
		Files:   files,
	}))
	if err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	return resp.Msg.CommitSha, nil
}

func (d *DirectOps) GitClone(ctx context.Context, repoURL, localPath, branch string, depth int32) (string, error) {
	resp, err := d.client.GitClone(ctx, connect.NewRequest(&workerv1.GitCloneRequest{
		RepoUrl:   repoURL,
		LocalPath: localPath,
		Branch:    branch,
		Depth:     depth,
	}))
	if err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}
	return resp.Msg.CommitSha, nil
}

func (d *DirectOps) GitPush(ctx context.Context, repoPath, remote, branch string, force bool) error {
	_, err := d.client.GitPush(ctx, connect.NewRequest(&workerv1.GitPushRequest{
		RepoPath: repoPath,
		Remote:   remote,
		Branch:   branch,
		Force:    force,
	}))
	if err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// ============================================================================
// Browser operations
// ============================================================================

func (d *DirectOps) BrowserStart(ctx context.Context) error {
	_, err := d.client.BrowserStart(ctx, connect.NewRequest(&workerv1.BrowserStartRequest{}))
	if err != nil {
		return fmt.Errorf("browser start: %w", err)
	}
	return nil
}

func (d *DirectOps) BrowserStop(ctx context.Context) error {
	_, err := d.client.BrowserStop(ctx, connect.NewRequest(&workerv1.BrowserStopRequest{}))
	if err != nil {
		return fmt.Errorf("browser stop: %w", err)
	}
	return nil
}

func (d *DirectOps) BrowserNavigate(ctx context.Context, url string, pageID *string) (*BrowserNavigateResult, error) {
	resp, err := d.client.BrowserNavigate(ctx, connect.NewRequest(&workerv1.BrowserNavigateRequest{
		Url:    url,
		PageId: pageID,
	}))
	if err != nil {
		return nil, fmt.Errorf("browser navigate: %w", err)
	}
	return &BrowserNavigateResult{
		PageID: resp.Msg.PageId,
		URL:    resp.Msg.Url,
		Title:  resp.Msg.Title,
	}, nil
}

func (d *DirectOps) BrowserListPages(ctx context.Context) ([]*PageInfo, error) {
	resp, err := d.client.BrowserListPages(ctx, connect.NewRequest(&workerv1.BrowserListPagesRequest{}))
	if err != nil {
		return nil, fmt.Errorf("browser list pages: %w", err)
	}
	pages := make([]*PageInfo, len(resp.Msg.Pages))
	for i, p := range resp.Msg.Pages {
		pages[i] = &PageInfo{
			PageID: p.PageId,
			URL:    p.Url,
			Title:  p.Title,
		}
	}
	return pages, nil
}

func (d *DirectOps) BrowserClosePage(ctx context.Context, pageID string) error {
	_, err := d.client.BrowserClosePage(ctx, connect.NewRequest(&workerv1.BrowserClosePageRequest{
		PageId: pageID,
	}))
	if err != nil {
		return fmt.Errorf("browser close page: %w", err)
	}
	return nil
}

func (d *DirectOps) BrowserScreenshot(ctx context.Context, pageID *string, format string, fullPage bool, selector *string, quality *int32) (*ScreenshotResult, error) {
	resp, err := d.client.BrowserScreenshot(ctx, connect.NewRequest(&workerv1.BrowserScreenshotRequest{
		PageId:   pageID,
		Format:   parseScreenshotFormatWorker(format),
		FullPage: fullPage,
		Selector: selector,
		Quality:  quality,
	}))
	if err != nil {
		return nil, fmt.Errorf("browser screenshot: %w", err)
	}
	return &ScreenshotResult{
		Image:       resp.Msg.Image,
		ImageBase64: base64.StdEncoding.EncodeToString(resp.Msg.Image),
		ContentType: resp.Msg.ContentType,
	}, nil
}

func (d *DirectOps) BrowserSnapshot(ctx context.Context, pageID *string, types []string, includeBounds bool) (*SnapshotResult, error) {
	protoTypes := make([]workerv1.SnapshotType, len(types))
	for i, t := range types {
		protoTypes[i] = parseSnapshotTypeWorker(t)
	}
	resp, err := d.client.BrowserSnapshot(ctx, connect.NewRequest(&workerv1.BrowserSnapshotRequest{
		PageId:        pageID,
		Types:         protoTypes,
		IncludeBounds: includeBounds,
	}))
	if err != nil {
		return nil, fmt.Errorf("browser snapshot: %w", err)
	}
	result := &SnapshotResult{}
	if resp.Msg.DomHtml != nil {
		result.DomHTML = *resp.Msg.DomHtml
	}
	if resp.Msg.AccessibilityTree != nil {
		result.AccessibilityTree = convertAccessibilityNodeFromWorker(resp.Msg.AccessibilityTree)
	}
	if resp.Msg.SnapshotId != nil {
		result.SnapshotID = *resp.Msg.SnapshotId
	}
	return result, nil
}

func (d *DirectOps) BrowserAct(ctx context.Context, pageID *string, actions []*BrowserAction) ([]*BrowserActionResult, error) {
	protoActions := make([]*workerv1.BrowserAction, len(actions))
	for i, a := range actions {
		protoActions[i] = convertBrowserActionToWorker(a)
	}
	resp, err := d.client.BrowserAct(ctx, connect.NewRequest(&workerv1.BrowserActRequest{
		PageId:  pageID,
		Actions: protoActions,
	}))
	if err != nil {
		return nil, fmt.Errorf("browser act: %w", err)
	}
	results := make([]*BrowserActionResult, len(resp.Msg.Results))
	for i, r := range resp.Msg.Results {
		result := &BrowserActionResult{Success: r.Success}
		if r.Error != nil {
			result.Error = *r.Error
		}
		if r.Value != nil {
			result.Value = *r.Value
		}
		results[i] = result
	}
	return results, nil
}

// ============================================================================
// HTTP/gRPC operations
// ============================================================================

func (d *DirectOps) HttpRequest(ctx context.Context, method, url string, headers []*HttpHeader, body []byte, followRedirects bool, timeoutMs *int32, clearCookies bool) (*HttpResult, error) {
	protoHeaders := make([]*workerv1.HttpHeader, len(headers))
	for i, h := range headers {
		protoHeaders[i] = &workerv1.HttpHeader{Name: h.Name, Value: h.Value}
	}
	resp, err := d.client.HttpRequest(ctx, connect.NewRequest(&workerv1.HttpRequestRequest{
		Method:          method,
		Url:             url,
		Headers:         protoHeaders,
		Body:            body,
		FollowRedirects: followRedirects,
		TimeoutMs:       timeoutMs,
		ClearCookies:    clearCookies,
	}))
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	respHeaders := make([]*HttpHeader, len(resp.Msg.Headers))
	for i, h := range resp.Msg.Headers {
		respHeaders[i] = &HttpHeader{Name: h.Name, Value: h.Value}
	}
	return &HttpResult{
		StatusCode: resp.Msg.StatusCode,
		Headers:    respHeaders,
		Body:       string(resp.Msg.Body),
		DurationMs: resp.Msg.DurationMs,
	}, nil
}

func (d *DirectOps) HttpClearCookies(ctx context.Context) error {
	_, err := d.client.HttpClearCookies(ctx, connect.NewRequest(&workerv1.HttpClearCookiesRequest{}))
	if err != nil {
		return fmt.Errorf("http clear cookies: %w", err)
	}
	return nil
}

func (d *DirectOps) GrpcRequest(ctx context.Context, address, method string, body *string, metadata []*GrpcMetadata, protoFile *string, useReflection bool) (*GrpcResult, error) {
	protoMetadata := make([]*workerv1.GrpcMetadata, len(metadata))
	for i, m := range metadata {
		protoMetadata[i] = &workerv1.GrpcMetadata{Key: m.Key, Value: m.Value}
	}
	resp, err := d.client.GrpcRequest(ctx, connect.NewRequest(&workerv1.GrpcRequestRequest{
		Address:       address,
		Method:        method,
		Body:          body,
		Metadata:      protoMetadata,
		ProtoFile:     protoFile,
		UseReflection: useReflection,
	}))
	if err != nil {
		return nil, fmt.Errorf("grpc request: %w", err)
	}
	respMetadata := make([]*GrpcMetadata, len(resp.Msg.Metadata))
	for i, m := range resp.Msg.Metadata {
		respMetadata[i] = &GrpcMetadata{Key: m.Key, Value: m.Value}
	}
	return &GrpcResult{
		ResponseBody:  resp.Msg.ResponseBody,
		Metadata:      respMetadata,
		StatusCode:    resp.Msg.StatusCode,
		StatusMessage: resp.Msg.StatusMessage,
	}, nil
}

// ============================================================================
// Worker proto enum conversion helpers
// ============================================================================

func parseScreenshotFormatWorker(format string) workerv1.ScreenshotFormat {
	switch strings.ToLower(format) {
	case "png":
		return workerv1.ScreenshotFormat_SCREENSHOT_FORMAT_PNG
	case "jpeg", "jpg":
		return workerv1.ScreenshotFormat_SCREENSHOT_FORMAT_JPEG
	case "webp":
		return workerv1.ScreenshotFormat_SCREENSHOT_FORMAT_WEBP
	default:
		return workerv1.ScreenshotFormat_SCREENSHOT_FORMAT_PNG
	}
}

func parseSnapshotTypeWorker(t string) workerv1.SnapshotType {
	switch strings.ToLower(t) {
	case "dom":
		return workerv1.SnapshotType_SNAPSHOT_TYPE_DOM
	case "accessibility":
		return workerv1.SnapshotType_SNAPSHOT_TYPE_ACCESSIBILITY
	default:
		return workerv1.SnapshotType_SNAPSHOT_TYPE_UNSPECIFIED
	}
}

func parseActionTypeWorker(t string) workerv1.BrowserActionType {
	switch strings.ToLower(t) {
	case "click":
		return workerv1.BrowserActionType_BROWSER_ACTION_CLICK
	case "fill":
		return workerv1.BrowserActionType_BROWSER_ACTION_FILL
	case "type":
		return workerv1.BrowserActionType_BROWSER_ACTION_TYPE
	case "press":
		return workerv1.BrowserActionType_BROWSER_ACTION_PRESS
	case "select_option":
		return workerv1.BrowserActionType_BROWSER_ACTION_SELECT_OPTION
	case "check":
		return workerv1.BrowserActionType_BROWSER_ACTION_CHECK
	case "uncheck":
		return workerv1.BrowserActionType_BROWSER_ACTION_UNCHECK
	case "hover":
		return workerv1.BrowserActionType_BROWSER_ACTION_HOVER
	case "scroll":
		return workerv1.BrowserActionType_BROWSER_ACTION_SCROLL
	case "focus":
		return workerv1.BrowserActionType_BROWSER_ACTION_FOCUS
	case "evaluate":
		return workerv1.BrowserActionType_BROWSER_ACTION_EVALUATE
	case "wait_for_selector":
		return workerv1.BrowserActionType_BROWSER_ACTION_WAIT_FOR_SELECTOR
	default:
		return workerv1.BrowserActionType_BROWSER_ACTION_UNSPECIFIED
	}
}

func parseMouseButtonWorker(b string) workerv1.MouseButton {
	switch strings.ToLower(b) {
	case "left":
		return workerv1.MouseButton_MOUSE_BUTTON_LEFT
	case "right":
		return workerv1.MouseButton_MOUSE_BUTTON_RIGHT
	case "middle":
		return workerv1.MouseButton_MOUSE_BUTTON_MIDDLE
	default:
		return workerv1.MouseButton_MOUSE_BUTTON_UNSPECIFIED
	}
}

func parseModifierWorker(m string) workerv1.Modifier {
	switch strings.ToLower(m) {
	case "alt":
		return workerv1.Modifier_MODIFIER_ALT
	case "control":
		return workerv1.Modifier_MODIFIER_CONTROL
	case "meta":
		return workerv1.Modifier_MODIFIER_META
	case "shift":
		return workerv1.Modifier_MODIFIER_SHIFT
	default:
		return workerv1.Modifier_MODIFIER_UNSPECIFIED
	}
}

func parseWaitStateWorker(s string) workerv1.WaitForState {
	switch strings.ToLower(s) {
	case "visible":
		return workerv1.WaitForState_WAIT_FOR_STATE_VISIBLE
	case "hidden":
		return workerv1.WaitForState_WAIT_FOR_STATE_HIDDEN
	case "attached":
		return workerv1.WaitForState_WAIT_FOR_STATE_ATTACHED
	case "detached":
		return workerv1.WaitForState_WAIT_FOR_STATE_DETACHED
	default:
		return workerv1.WaitForState_WAIT_FOR_STATE_UNSPECIFIED
	}
}

func convertBrowserActionToWorker(a *BrowserAction) *workerv1.BrowserAction {
	pa := &workerv1.BrowserAction{
		Type:   parseActionTypeWorker(a.Type),
		Values: a.Values,
	}
	if a.Selector != "" {
		pa.Selector = &a.Selector
	}
	if a.Value != "" {
		pa.Value = &a.Value
	}
	if a.DelayMs != 0 {
		pa.DelayMs = &a.DelayMs
	}
	if a.Button != "" {
		b := parseMouseButtonWorker(a.Button)
		pa.Button = &b
	}
	if a.ClickCount != 0 {
		pa.ClickCount = &a.ClickCount
	}
	for _, m := range a.Modifiers {
		pa.Modifiers = append(pa.Modifiers, parseModifierWorker(m))
	}
	if a.ScrollX != 0 {
		pa.ScrollX = &a.ScrollX
	}
	if a.ScrollY != 0 {
		pa.ScrollY = &a.ScrollY
	}
	if a.WaitState != "" {
		ws := parseWaitStateWorker(a.WaitState)
		pa.WaitState = &ws
	}
	if a.TimeoutMs != 0 {
		pa.TimeoutMs = &a.TimeoutMs
	}
	return pa
}

func convertAccessibilityNodeFromWorker(n *workerv1.AccessibilityNode) *AccessibilityNode {
	if n == nil {
		return nil
	}
	node := &AccessibilityNode{
		Ref:  n.Ref,
		Role: n.Role,
		Name: n.Name,
	}
	if n.Description != nil {
		node.Description = *n.Description
	}
	if n.Value != nil {
		node.Value = *n.Value
	}
	if n.Bounds != nil {
		node.Bounds = &BoundingBox{
			X:      n.Bounds.X,
			Y:      n.Bounds.Y,
			Width:  n.Bounds.Width,
			Height: n.Bounds.Height,
		}
	}
	if len(n.Children) > 0 {
		node.Children = make([]*AccessibilityNode, len(n.Children))
		for i, c := range n.Children {
			node.Children[i] = convertAccessibilityNodeFromWorker(c)
		}
	}
	return node
}

// Compile-time check that DirectOps implements Ops.
var _ Ops = (*DirectOps)(nil)
