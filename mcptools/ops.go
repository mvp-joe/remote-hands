package mcptools

import "context"

// Ops defines the interface for remote operations.
// This interface is implemented by both DirectOps (for remotehands-mcp)
// and DirectOps (direct worker client).
type Ops interface {
	// RunBash executes a bash command and returns the collected output.
	RunBash(ctx context.Context, cmd string, timeoutMs int32, env map[string]string, workingDir string) (*BashResult, error)

	// ReadFile reads content from a file at the given path.
	ReadFile(ctx context.Context, path string, offset, limit int64) ([]byte, error)

	// WriteFile writes content to a file at the given path.
	WriteFile(ctx context.Context, path string, content []byte, mode int32) error

	// DeleteFile deletes a file or directory at the given path.
	DeleteFile(ctx context.Context, path string, recursive bool) error

	// ListFiles lists files in the given directory.
	ListFiles(ctx context.Context, path string, recursive bool) ([]*FileEntry, error)

	// Glob finds files matching the given pattern.
	Glob(ctx context.Context, pattern string, basePath string) ([]string, error)

	// Grep searches for a pattern in files.
	Grep(ctx context.Context, pattern, path, glob string, ignoreCase bool, contextLines int32) ([]*GrepMatch, error)

	// GitStatus returns the git status of files in the repository.
	GitStatus(ctx context.Context, path string) ([]*GitFileStatus, error)

	// GitDiff returns the diff output for the repository.
	GitDiff(ctx context.Context, path string, staged bool) (string, error)

	// GitCommit creates a git commit with the given message and files.
	GitCommit(ctx context.Context, message string, files []string) (string, error)

	// Browser operations
	BrowserStart(ctx context.Context) error
	BrowserStop(ctx context.Context) error
	BrowserNavigate(ctx context.Context, url string, pageID *string) (*BrowserNavigateResult, error)
	BrowserListPages(ctx context.Context) ([]*PageInfo, error)
	BrowserClosePage(ctx context.Context, pageID string) error
	BrowserScreenshot(ctx context.Context, pageID *string, format string, fullPage bool, selector *string, quality *int32) (*ScreenshotResult, error)
	BrowserSnapshot(ctx context.Context, pageID *string, types []string, includeBounds bool) (*SnapshotResult, error)
	BrowserAct(ctx context.Context, pageID *string, actions []*BrowserAction) ([]*BrowserActionResult, error)

	// HTTP/gRPC operations
	HttpRequest(ctx context.Context, method, url string, headers []*HttpHeader, body []byte, followRedirects bool, timeoutMs *int32, clearCookies bool) (*HttpResult, error)
	HttpClearCookies(ctx context.Context) error
	GrpcRequest(ctx context.Context, address, method string, body *string, metadata []*GrpcMetadata, protoFile *string, useReflection bool) (*GrpcResult, error)
}
