package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"connectrpc.com/connect"
	"github.com/go-git/go-git/v5/plumbing/transport"

	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/mvp-joe/remote-hands/gen/remotehands/v1/remotehandsv1connect"
)

// CmdCustomizer is called with every *exec.Cmd created for bash execution
// (both RunBash and ProcessStart) before the command is started. Use it to
// set SysProcAttr credentials, replace the environment, or apply any other
// pre-start customization. The callback MUST NOT call cmd.Start().
//
// Note: runBash and ProcessManager always set SysProcAttr.Setpgid = true
// after the customizer returns, so the customizer does not need to preserve it.
type CmdCustomizer func(cmd *exec.Cmd)

// Service implements the ServiceHandler interface.
type Service struct {
	remotehandsv1connect.UnimplementedServiceHandler

	homeDir        string
	logger         *slog.Logger
	browser        *BrowserManager
	httpClient     *HttpClient
	process        *ProcessManager
	cmdCustomizer  CmdCustomizer

	// go-git auth — set via NewServiceWithGitAuth, used only by GitClone/GitPush
	gitSSHAuth   transport.AuthMethod
	gitHTTPSAuth transport.AuthMethod

	// go-git commit author defaults — set via NewServiceWithGitAuth
	gitAuthorName  string
	gitAuthorEmail string
}

// NewService creates a new Service with the given home directory.
// All file paths are validated to be under homeDir.
func NewService(homeDir string, logger *slog.Logger) (*Service, error) {
	pm, err := NewProcessManager(homeDir, logger)
	if err != nil {
		return nil, fmt.Errorf("create process manager: %w", err)
	}

	return &Service{
		homeDir:    homeDir,
		logger:     logger,
		browser:    NewBrowserManager(),
		httpClient: NewHttpClient(),
		process:    pm,
	}, nil
}

// SetCmdCustomizer sets a callback that is invoked on every *exec.Cmd created
// for bash execution (RunBash and ProcessStart) before the command starts.
// This allows the caller to drop privileges (e.g. set UID/GID via
// SysProcAttr.Credential) or replace the environment for sandboxing.
//
// Setpgid is always re-applied after the customizer runs, so the customizer
// does not need to preserve it.
func (s *Service) SetCmdCustomizer(fn CmdCustomizer) {
	s.cmdCustomizer = fn
	s.process.SetCmdCustomizer(fn)
}

// ReadFile reads a file's content with optional offset and limit.
func (s *Service) ReadFile(
	ctx context.Context,
	req *connect.Request[remotehandsv1.ReadFileRequest],
) (*connect.Response[remotehandsv1.ReadFileResponse], error) {
	content, err := s.readFile(ctx, req.Msg.Path, req.Msg.Offset, req.Msg.Limit)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.ReadFileResponse{
		Content: content,
	}), nil
}

// WriteFile writes content to a file, creating parent directories as needed.
func (s *Service) WriteFile(
	ctx context.Context,
	req *connect.Request[remotehandsv1.WriteFileRequest],
) (*connect.Response[remotehandsv1.Empty], error) {
	if err := s.writeFile(ctx, req.Msg.Path, req.Msg.Content, req.Msg.Mode); err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.Empty{}), nil
}

// DeleteFile deletes a file or directory.
func (s *Service) DeleteFile(
	ctx context.Context,
	req *connect.Request[remotehandsv1.DeleteFileRequest],
) (*connect.Response[remotehandsv1.Empty], error) {
	if err := s.deleteFile(ctx, req.Msg.Path, req.Msg.Recursive); err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.Empty{}), nil
}

// ListFiles lists files at a path, optionally recursively.
func (s *Service) ListFiles(
	ctx context.Context,
	req *connect.Request[remotehandsv1.ListFilesRequest],
) (*connect.Response[remotehandsv1.ListFilesResponse], error) {
	entries, err := s.listFiles(ctx, req.Msg.Path, req.Msg.Recursive)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.ListFilesResponse{
		Files: entries,
	}), nil
}

// Glob matches files against a glob pattern.
func (s *Service) Glob(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GlobRequest],
) (*connect.Response[remotehandsv1.GlobResponse], error) {
	matches, err := s.glob(ctx, req.Msg.Pattern, req.Msg.Path)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GlobResponse{
		Matches: matches,
	}), nil
}

// Grep searches file contents with a regex pattern.
func (s *Service) Grep(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GrepRequest],
) (*connect.Response[remotehandsv1.GrepResponse], error) {
	matches, err := s.grep(ctx, req.Msg.Pattern, req.Msg.Path, req.Msg.Glob, req.Msg.IgnoreCase, req.Msg.ContextLines)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GrepResponse{
		Matches: matches,
	}), nil
}

// RunBash executes a bash command and streams stdout/stderr events.
func (s *Service) RunBash(
	ctx context.Context,
	req *connect.Request[remotehandsv1.RunBashRequest],
	stream *connect.ServerStream[remotehandsv1.RunBashEvent],
) error {
	return s.runBash(
		ctx,
		req.Msg.Command,
		req.Msg.TimeoutMs,
		req.Msg.Env,
		req.Msg.WorkingDir,
		stream.Send,
	)
}

// GitStatus returns the status of files in a git repository.
func (s *Service) GitStatus(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GitStatusRequest],
) (*connect.Response[remotehandsv1.GitStatusResponse], error) {
	files, err := s.gitStatus(ctx, req.Msg.Path)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GitStatusResponse{
		Files: files,
	}), nil
}

// GitDiff returns the diff of changes in a git repository.
func (s *Service) GitDiff(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GitDiffRequest],
) (*connect.Response[remotehandsv1.GitDiffResponse], error) {
	diff, err := s.gitDiff(ctx, req.Msg.Path, req.Msg.FilePath, req.Msg.Staged)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GitDiffResponse{
		Diff: diff,
	}), nil
}

// GitCommit creates a commit in a git repository.
func (s *Service) GitCommit(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GitCommitRequest],
) (*connect.Response[remotehandsv1.GitCommitResponse], error) {
	sha, err := s.gitCommit(ctx, req.Msg.Path, req.Msg.Message, req.Msg.Files, req.Msg.AuthorName, req.Msg.AuthorEmail)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GitCommitResponse{
		CommitSha: sha,
	}), nil
}

// GitClone clones a remote repository into a path under homeDir.
func (s *Service) GitClone(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GitCloneRequest],
) (*connect.Response[remotehandsv1.GitCloneResponse], error) {
	sha, err := s.gitClone(ctx, req.Msg.RepoUrl, req.Msg.LocalPath, req.Msg.Branch, req.Msg.Depth)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GitCloneResponse{
		CommitSha: sha,
	}), nil
}

// GitPush pushes a local repository's branch to the remote.
func (s *Service) GitPush(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GitPushRequest],
) (*connect.Response[remotehandsv1.GitPushResponse], error) {
	if err := s.gitPush(ctx, req.Msg.RepoPath, req.Msg.Remote, req.Msg.Branch, req.Msg.Force); err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GitPushResponse{}), nil
}

// WatchFiles watches files matching glob patterns and streams file events.
func (s *Service) WatchFiles(
	ctx context.Context,
	req *connect.Request[remotehandsv1.WatchFilesRequest],
	stream *connect.ServerStream[remotehandsv1.FileEvent],
) error {
	return s.watchFiles(ctx, req.Msg.Patterns, stream.Send)
}

// BrowserStart launches a headless Chromium instance.
func (s *Service) BrowserStart(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserStartRequest],
) (*connect.Response[remotehandsv1.BrowserStartResponse], error) {
	if err := s.browser.Start(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("browser start: %w", err))
	}

	return connect.NewResponse(&remotehandsv1.BrowserStartResponse{}), nil
}

// BrowserNavigate navigates a new or existing page to a URL.
func (s *Service) BrowserNavigate(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserNavigateRequest],
) (*connect.Response[remotehandsv1.BrowserNavigateResponse], error) {
	if !s.browser.IsRunning() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("browser not started"))
	}

	result, err := s.browser.Navigate(ctx, req.Msg.Url, req.Msg.PageId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.BrowserNavigateResponse{
		PageId: result.PageID,
		Url:    result.URL,
		Title:  result.Title,
	}), nil
}

// BrowserListPages returns info for all open browser pages.
func (s *Service) BrowserListPages(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserListPagesRequest],
) (*connect.Response[remotehandsv1.BrowserListPagesResponse], error) {
	if !s.browser.IsRunning() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("browser not started"))
	}

	results, err := s.browser.ListPages(ctx)
	if err != nil {
		return nil, err
	}

	pages := make([]*remotehandsv1.PageInfo, len(results))
	for i, r := range results {
		pages[i] = &remotehandsv1.PageInfo{
			PageId: r.PageID,
			Url:    r.URL,
			Title:  r.Title,
		}
	}

	return connect.NewResponse(&remotehandsv1.BrowserListPagesResponse{
		Pages: pages,
	}), nil
}

// BrowserClosePage closes a browser page by its ID.
func (s *Service) BrowserClosePage(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserClosePageRequest],
) (*connect.Response[remotehandsv1.BrowserClosePageResponse], error) {
	if !s.browser.IsRunning() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("browser not started"))
	}

	if err := s.browser.ClosePage(ctx, req.Msg.PageId); err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.BrowserClosePageResponse{}), nil
}

// BrowserScreenshot captures a screenshot of a page or element.
func (s *Service) BrowserScreenshot(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserScreenshotRequest],
) (*connect.Response[remotehandsv1.BrowserScreenshotResponse], error) {
	if !s.browser.IsRunning() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("browser not started"))
	}

	image, contentType, err := s.browser.Screenshot(
		ctx,
		req.Msg.PageId,
		req.Msg.Format,
		req.Msg.FullPage,
		req.Msg.Selector,
		req.Msg.Quality,
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.BrowserScreenshotResponse{
		Image:       image,
		ContentType: contentType,
	}), nil
}

// BrowserSnapshot captures DOM HTML and/or the accessibility tree for a page.
func (s *Service) BrowserSnapshot(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserSnapshotRequest],
) (*connect.Response[remotehandsv1.BrowserSnapshotResponse], error) {
	if !s.browser.IsRunning() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("browser not started"))
	}

	result, err := s.browser.Snapshot(ctx, req.Msg.PageId, req.Msg.Types, req.Msg.IncludeBounds)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.BrowserSnapshotResponse{
		DomHtml:           result.DomHTML,
		AccessibilityTree: result.AccessibilityTree,
		SnapshotId:        result.SnapshotID,
	}), nil
}

// BrowserAct executes a sequence of browser actions on a page.
func (s *Service) BrowserAct(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserActRequest],
) (*connect.Response[remotehandsv1.BrowserActResponse], error) {
	if !s.browser.IsRunning() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("browser not started"))
	}

	results, err := s.browser.Act(ctx, req.Msg.PageId, req.Msg.Actions)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.BrowserActResponse{
		Results: results,
	}), nil
}

// BrowserStop shuts down the running browser instance.
func (s *Service) BrowserStop(
	ctx context.Context,
	req *connect.Request[remotehandsv1.BrowserStopRequest],
) (*connect.Response[remotehandsv1.BrowserStopResponse], error) {
	if err := s.browser.Stop(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("browser stop: %w", err))
	}

	return connect.NewResponse(&remotehandsv1.BrowserStopResponse{}), nil
}

// HttpRequest executes an HTTP request with a persistent cookie jar.
func (s *Service) HttpRequest(
	ctx context.Context,
	req *connect.Request[remotehandsv1.HttpRequestRequest],
) (*connect.Response[remotehandsv1.HttpRequestResponse], error) {
	result, err := s.httpClient.Do(
		ctx,
		req.Msg.Method,
		req.Msg.Url,
		req.Msg.Headers,
		req.Msg.Body,
		req.Msg.FollowRedirects,
		req.Msg.TimeoutMs,
		req.Msg.ClearCookies,
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.HttpRequestResponse{
		StatusCode: result.StatusCode,
		Headers:    result.Headers,
		Body:       result.Body,
		DurationMs: result.DurationMs,
	}), nil
}

// HttpClearCookies resets the HTTP cookie jar.
func (s *Service) HttpClearCookies(
	ctx context.Context,
	req *connect.Request[remotehandsv1.HttpClearCookiesRequest],
) (*connect.Response[remotehandsv1.HttpClearCookiesResponse], error) {
	s.httpClient.ClearCookies()
	return connect.NewResponse(&remotehandsv1.HttpClearCookiesResponse{}), nil
}

// GrpcRequest executes a gRPC call via grpcurl.
func (s *Service) GrpcRequest(
	ctx context.Context,
	req *connect.Request[remotehandsv1.GrpcRequestRequest],
) (*connect.Response[remotehandsv1.GrpcRequestResponse], error) {
	result, err := s.grpcRequest(
		ctx,
		req.Msg.Address,
		req.Msg.Method,
		req.Msg.Body,
		req.Msg.Metadata,
		req.Msg.ProtoFile,
		req.Msg.UseReflection,
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.GrpcRequestResponse{
		ResponseBody:  result.ResponseBody,
		Metadata:      result.Metadata,
		StatusCode:    result.StatusCode,
		StatusMessage: result.StatusMessage,
	}), nil
}

// ProcessStart launches a background process.
func (s *Service) ProcessStart(
	ctx context.Context,
	req *connect.Request[remotehandsv1.ProcessStartRequest],
) (*connect.Response[remotehandsv1.ProcessStartResponse], error) {
	pid, err := s.process.Start(ctx, req.Msg.Command, req.Msg.Name, req.Msg.Env, req.Msg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.ProcessStartResponse{
		Pid: pid,
	}), nil
}

// ProcessStop stops a running process by PID.
func (s *Service) ProcessStop(
	ctx context.Context,
	req *connect.Request[remotehandsv1.ProcessStopRequest],
) (*connect.Response[remotehandsv1.ProcessStopResponse], error) {
	if err := s.process.Stop(req.Msg.Pid); err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.ProcessStopResponse{}), nil
}

// ProcessList returns info for tracked processes.
func (s *Service) ProcessList(
	ctx context.Context,
	req *connect.Request[remotehandsv1.ProcessListRequest],
) (*connect.Response[remotehandsv1.ProcessListResponse], error) {
	infos := s.process.List(req.Msg.IncludeExited)

	processes := make([]*remotehandsv1.ProcessInfo, len(infos))
	for i, info := range infos {
		p := &remotehandsv1.ProcessInfo{
			Pid:        info.PID,
			Name:       info.Name,
			Command:    info.Command,
			WorkingDir: info.WorkingDir,
			Status:     info.Status,
			ExitCode:   info.ExitCode,
			StartedAt:  info.StartedAt.Unix(),
		}
		if info.ExitedAt != nil {
			exitedAt := info.ExitedAt.Unix()
			p.ExitedAt = &exitedAt
		}
		processes[i] = p
	}

	return connect.NewResponse(&remotehandsv1.ProcessListResponse{
		Processes: processes,
	}), nil
}

// ProcessLogs reads buffered stdout/stderr from disk for a process.
func (s *Service) ProcessLogs(
	ctx context.Context,
	req *connect.Request[remotehandsv1.ProcessLogsRequest],
) (*connect.Response[remotehandsv1.ProcessLogsResponse], error) {
	stdout, stderr, err := s.process.Logs(req.Msg.Pid, req.Msg.Head, req.Msg.Tail)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&remotehandsv1.ProcessLogsResponse{
		Stdout: stdout,
		Stderr: stderr,
	}), nil
}

// ProcessTail streams live stdout/stderr from a running process.
func (s *Service) ProcessTail(
	ctx context.Context,
	req *connect.Request[remotehandsv1.ProcessTailRequest],
	stream *connect.ServerStream[remotehandsv1.ProcessTailEvent],
) error {
	return s.process.Tail(ctx, req.Msg.Pid, stream.Send)
}

// Close shuts down all managed resources. It should be called when the
// service is being torn down.
func (s *Service) Close(ctx context.Context) error {
	s.process.StopAll()
	return s.browser.Stop(ctx)
}
