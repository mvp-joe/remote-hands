// Command remotehands-mcp serves an MCP server that proxies to a remotehands
// ConnectRPC server. Supports direct mode (connecting to a local/remote server)
// and relay mode (connecting through an HTTP relay like overwatch-fly-relay).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"connectrpc.com/connect"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/mvp-joe/remote-hands/gen/remotehands/v1/remotehandsv1connect"
	"github.com/mvp-joe/remote-hands/mcptools"
	"github.com/mvp-joe/remote-hands/mcputils"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "localhost:19051", "remotehands server address [direct mode]")
	relay := flag.String("relay", "", "Relay URL (e.g. https://overwatch-relay.fly.dev) [relay mode]")
	machine := flag.String("machine", "", "Fly machine ID [relay mode; required when --relay is set]")
	flag.Parse()

	if *relay != "" && *machine == "" {
		return fmt.Errorf("--machine is required when --relay is set")
	}

	// Build the ConnectRPC client with header injection interceptor.
	var interceptor connect.Interceptor
	var baseURL string

	if *relay != "" {
		// Relay mode.
		baseURL = *relay
		secret := os.Getenv("REMOTEHANDS_RELAY_SECRET")
		machineID := *machine
		interceptor = &streamingHeaderInterceptor{inject: func(h http.Header) {
			if secret != "" {
				h.Set("Authorization", "Bearer "+secret)
			}
			h.Set("X-Target-Machine", machineID)
		}}
	} else {
		// Direct mode.
		baseURL = "http://" + *addr
		token := os.Getenv("REMOTEHANDS_AUTH_TOKEN")
		interceptor = &streamingHeaderInterceptor{inject: func(h http.Header) {
			if token != "" {
				h.Set("Authorization", "Bearer "+token)
			}
		}}
	}

	client := remotehandsv1connect.NewServiceClient(
		http.DefaultClient,
		baseURL,
		connect.WithInterceptors(interceptor),
	)

	ops := mcptools.NewDirectOps(client)

	// Create MCP server.
	mcpServer := server.NewMCPServer(
		"remotehands",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerTools(mcpServer, ops, client)

	return server.ServeStdio(mcpServer)
}

// registerTools registers all MCP tools on the server.
func registerTools(s *server.MCPServer, ops mcptools.Ops, client remotehandsv1connect.ServiceClient) {
	// File operations
	s.AddTool(newTool("read_file", "Read a file's content",
		mcp.WithString("path", mcp.Required(), mcp.Description("File path relative to home directory")),
		mcp.WithNumber("offset", mcp.Description("Byte offset to start reading from")),
		mcp.WithNumber("limit", mcp.Description("Maximum bytes to read")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Path   string `json:"path"`
			Offset int64  `json:"offset"`
			Limit  int64  `json:"limit"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		content, err := ops.ReadFile(ctx, args.Path, args.Offset, args.Limit)
		if err != nil {
			return mcputils.ErrorToolResult("read_file", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(content))},
		}, nil
	})

	s.AddTool(newTool("write_file", "Write content to a file",
		mcp.WithString("path", mcp.Required(), mcp.Description("File path relative to home directory")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write")),
		mcp.WithNumber("mode", mcp.Description("File mode (e.g. 0644)")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			Mode    int32  `json:"mode"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		err := ops.WriteFile(ctx, args.Path, []byte(args.Content), args.Mode)
		if err != nil {
			return mcputils.ErrorToolResult("write_file", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	s.AddTool(newTool("delete_file", "Delete a file or directory",
		mcp.WithString("path", mcp.Required(), mcp.Description("File path relative to home directory")),
		mcp.WithBoolean("recursive", mcp.Description("Delete recursively")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		err := ops.DeleteFile(ctx, args.Path, args.Recursive)
		if err != nil {
			return mcputils.ErrorToolResult("delete_file", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	s.AddTool(newTool("list_files", "List files in a directory",
		mcp.WithString("path", mcp.Description("Directory path (empty = home)")),
		mcp.WithBoolean("recursive", mcp.Description("List recursively")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		files, err := ops.ListFiles(ctx, args.Path, args.Recursive)
		if err != nil {
			return mcputils.ErrorToolResult("list_files", err)
		}
		return mcputils.JSONToolResult(files)
	})

	s.AddTool(newTool("glob", "Find files matching a glob pattern",
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Glob pattern (supports **)")),
		mcp.WithString("path", mcp.Description("Base directory path")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		matches, err := ops.Glob(ctx, args.Pattern, args.Path)
		if err != nil {
			return mcputils.ErrorToolResult("glob", err)
		}
		return mcputils.JSONToolResult(matches)
	})

	s.AddTool(newTool("grep", "Search file contents with regex",
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex pattern")),
		mcp.WithString("path", mcp.Description("Directory to search")),
		mcp.WithString("glob", mcp.Description("File glob filter")),
		mcp.WithBoolean("ignore_case", mcp.Description("Case-insensitive search")),
		mcp.WithNumber("context_lines", mcp.Description("Lines of context around matches")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Pattern      string `json:"pattern"`
			Path         string `json:"path"`
			Glob         string `json:"glob"`
			IgnoreCase   bool   `json:"ignore_case"`
			ContextLines int32  `json:"context_lines"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		matches, err := ops.Grep(ctx, args.Pattern, args.Path, args.Glob, args.IgnoreCase, args.ContextLines)
		if err != nil {
			return mcputils.ErrorToolResult("grep", err)
		}
		return mcputils.JSONToolResult(matches)
	})

	// Bash execution
	s.AddTool(newTool("run_bash", "Execute a bash command",
		mcp.WithString("command", mcp.Required(), mcp.Description("Command to execute")),
		mcp.WithNumber("timeout_ms", mcp.Description("Timeout in milliseconds")),
		mcp.WithObject("env", mcp.Description("Environment variables")),
		mcp.WithString("working_dir", mcp.Description("Working directory")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Command    string            `json:"command"`
			TimeoutMs  int32             `json:"timeout_ms"`
			Env        map[string]string `json:"env"`
			WorkingDir string            `json:"working_dir"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		result, err := ops.RunBash(ctx, args.Command, args.TimeoutMs, args.Env, args.WorkingDir)
		if err != nil {
			return mcputils.ErrorToolResult("run_bash", err)
		}
		return mcputils.JSONToolResult(result)
	})

	// Git operations
	s.AddTool(newTool("git_status", "Get git status of a repository",
		mcp.WithString("path", mcp.Description("Repository path")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Path string `json:"path"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		files, err := ops.GitStatus(ctx, args.Path)
		if err != nil {
			return mcputils.ErrorToolResult("git_status", err)
		}
		return mcputils.JSONToolResult(files)
	})

	s.AddTool(newTool("git_diff", "Get diff of changes in a git repository",
		mcp.WithString("path", mcp.Description("Repository path")),
		mcp.WithBoolean("staged", mcp.Description("Show staged changes")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Path   string `json:"path"`
			Staged bool   `json:"staged"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		diff, err := ops.GitDiff(ctx, args.Path, args.Staged)
		if err != nil {
			return mcputils.ErrorToolResult("git_diff", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(diff)},
		}, nil
	})

	s.AddTool(newTool("git_commit", "Create a git commit",
		mcp.WithString("message", mcp.Required(), mcp.Description("Commit message")),
		mcp.WithArray("files", mcp.Description("Files to stage before committing")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Message string   `json:"message"`
			Files   []string `json:"files"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		sha, err := ops.GitCommit(ctx, args.Message, args.Files)
		if err != nil {
			return mcputils.ErrorToolResult("git_commit", err)
		}
		return mcputils.JSONToolResult(map[string]string{"commit_sha": sha})
	})

	s.AddTool(newTool("git_clone", "Clone a remote git repository",
		mcp.WithString("repo_url", mcp.Required(), mcp.Description("Remote repository URL")),
		mcp.WithString("local_path", mcp.Required(), mcp.Description("Destination path relative to home")),
		mcp.WithString("branch", mcp.Description("Branch to checkout after clone")),
		mcp.WithNumber("depth", mcp.Description("Clone depth: 0 = shallow (depth 1, default), -1 = full history, >0 = specific depth")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			RepoURL   string  `json:"repo_url"`
			LocalPath string  `json:"local_path"`
			Branch    string  `json:"branch"`
			Depth     float64 `json:"depth"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		sha, err := ops.GitClone(ctx, args.RepoURL, args.LocalPath, args.Branch, int32(args.Depth))
		if err != nil {
			return mcputils.ErrorToolResult("git_clone", err)
		}
		return mcputils.JSONToolResult(map[string]string{"commit_sha": sha})
	})

	s.AddTool(newTool("git_push", "Push a local repository to remote",
		mcp.WithString("repo_path", mcp.Required(), mcp.Description("Local repository path")),
		mcp.WithString("remote", mcp.Description("Remote name (default: origin)")),
		mcp.WithString("branch", mcp.Description("Branch to push")),
		mcp.WithBoolean("force", mcp.Description("Force push")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			RepoPath string `json:"repo_path"`
			Remote   string `json:"remote"`
			Branch   string `json:"branch"`
			Force    bool   `json:"force"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		err := ops.GitPush(ctx, args.RepoPath, args.Remote, args.Branch, args.Force)
		if err != nil {
			return mcputils.ErrorToolResult("git_push", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	// Browser operations
	s.AddTool(newTool("browser_start", "Launch headless browser"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := ops.BrowserStart(ctx); err != nil {
			return mcputils.ErrorToolResult("browser_start", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	s.AddTool(newTool("browser_stop", "Stop the browser"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := ops.BrowserStop(ctx); err != nil {
			return mcputils.ErrorToolResult("browser_stop", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	s.AddTool(newTool("browser_navigate", "Navigate browser to a URL",
		mcp.WithString("url", mcp.Required(), mcp.Description("URL to navigate to")),
		mcp.WithString("page_id", mcp.Description("Page ID (empty = new page)")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			URL    string  `json:"url"`
			PageID *string `json:"page_id"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		result, err := ops.BrowserNavigate(ctx, args.URL, args.PageID)
		if err != nil {
			return mcputils.ErrorToolResult("browser_navigate", err)
		}
		return mcputils.JSONToolResult(result)
	})

	s.AddTool(newTool("browser_list_pages", "List open browser pages"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pages, err := ops.BrowserListPages(ctx)
		if err != nil {
			return mcputils.ErrorToolResult("browser_list_pages", err)
		}
		return mcputils.JSONToolResult(pages)
	})

	s.AddTool(newTool("browser_close_page", "Close a browser page",
		mcp.WithString("page_id", mcp.Required(), mcp.Description("Page ID")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			PageID string `json:"page_id"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		if err := ops.BrowserClosePage(ctx, args.PageID); err != nil {
			return mcputils.ErrorToolResult("browser_close_page", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	s.AddTool(newTool("browser_screenshot", "Capture a browser screenshot",
		mcp.WithString("page_id", mcp.Description("Page ID")),
		mcp.WithString("format", mcp.Description("Image format: png, jpeg, webp")),
		mcp.WithBoolean("full_page", mcp.Description("Capture full page")),
		mcp.WithString("selector", mcp.Description("Element selector to capture")),
		mcp.WithNumber("quality", mcp.Description("JPEG/WebP quality (1-100)")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			PageID   *string `json:"page_id"`
			Format   string  `json:"format"`
			FullPage bool    `json:"full_page"`
			Selector *string `json:"selector"`
			Quality  *int32  `json:"quality"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		result, err := ops.BrowserScreenshot(ctx, args.PageID, args.Format, args.FullPage, args.Selector, args.Quality)
		if err != nil {
			return mcputils.ErrorToolResult("browser_screenshot", err)
		}
		return mcputils.JSONToolResult(result)
	})

	s.AddTool(newTool("browser_snapshot", "Capture DOM/accessibility snapshot",
		mcp.WithString("page_id", mcp.Description("Page ID")),
		mcp.WithArray("types", mcp.Description("Snapshot types: dom, accessibility")),
		mcp.WithBoolean("include_bounds", mcp.Description("Include element bounding boxes")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			PageID        *string  `json:"page_id"`
			Types         []string `json:"types"`
			IncludeBounds bool     `json:"include_bounds"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		result, err := ops.BrowserSnapshot(ctx, args.PageID, args.Types, args.IncludeBounds)
		if err != nil {
			return mcputils.ErrorToolResult("browser_snapshot", err)
		}
		return mcputils.JSONToolResult(result)
	})

	s.AddTool(newTool("browser_act", "Execute browser actions",
		mcp.WithString("page_id", mcp.Description("Page ID")),
		mcp.WithArray("actions", mcp.Required(), mcp.Description("Actions to execute")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			PageID  *string                  `json:"page_id"`
			Actions []*mcptools.BrowserAction `json:"actions"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		results, err := ops.BrowserAct(ctx, args.PageID, args.Actions)
		if err != nil {
			return mcputils.ErrorToolResult("browser_act", err)
		}
		return mcputils.JSONToolResult(results)
	})

	// HTTP operations
	s.AddTool(newTool("http_request", "Execute an HTTP request",
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method")),
		mcp.WithString("url", mcp.Required(), mcp.Description("Request URL")),
		mcp.WithArray("headers", mcp.Description("Request headers [{name, value}]")),
		mcp.WithString("body", mcp.Description("Request body")),
		mcp.WithBoolean("follow_redirects", mcp.Description("Follow redirects")),
		mcp.WithNumber("timeout_ms", mcp.Description("Timeout in milliseconds")),
		mcp.WithBoolean("clear_cookies", mcp.Description("Clear cookies before request")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Method          string                `json:"method"`
			URL             string                `json:"url"`
			Headers         []*mcptools.HttpHeader `json:"headers"`
			Body            string                `json:"body"`
			FollowRedirects bool                  `json:"follow_redirects"`
			TimeoutMs       *int32                `json:"timeout_ms"`
			ClearCookies    bool                  `json:"clear_cookies"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		result, err := ops.HttpRequest(ctx, args.Method, args.URL, args.Headers, []byte(args.Body), args.FollowRedirects, args.TimeoutMs, args.ClearCookies)
		if err != nil {
			return mcputils.ErrorToolResult("http_request", err)
		}
		return mcputils.JSONToolResult(result)
	})

	s.AddTool(newTool("http_clear_cookies", "Clear HTTP cookies"), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := ops.HttpClearCookies(ctx); err != nil {
			return mcputils.ErrorToolResult("http_clear_cookies", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	// gRPC operations
	s.AddTool(newTool("grpc_request", "Execute a gRPC request",
		mcp.WithString("address", mcp.Required(), mcp.Description("gRPC server address")),
		mcp.WithString("method", mcp.Required(), mcp.Description("gRPC method name")),
		mcp.WithString("body", mcp.Description("JSON request body")),
		mcp.WithArray("metadata", mcp.Description("gRPC metadata [{key, value}]")),
		mcp.WithString("proto_file", mcp.Description("Proto file path")),
		mcp.WithBoolean("use_reflection", mcp.Description("Use server reflection")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Address       string                    `json:"address"`
			Method        string                    `json:"method"`
			Body          *string                   `json:"body"`
			Metadata      []*mcptools.GrpcMetadata   `json:"metadata"`
			ProtoFile     *string                   `json:"proto_file"`
			UseReflection bool                      `json:"use_reflection"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		result, err := ops.GrpcRequest(ctx, args.Address, args.Method, args.Body, args.Metadata, args.ProtoFile, args.UseReflection)
		if err != nil {
			return mcputils.ErrorToolResult("grpc_request", err)
		}
		return mcputils.JSONToolResult(result)
	})

	// Process management — these call the ConnectRPC client directly
	// because the Ops interface doesn't cover streaming RPCs.
	s.AddTool(newTool("process_start", "Start a background process",
		mcp.WithString("command", mcp.Required(), mcp.Description("Command to execute")),
		mcp.WithString("name", mcp.Description("Process name")),
		mcp.WithObject("env", mcp.Description("Environment variables")),
		mcp.WithString("working_dir", mcp.Description("Working directory")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Command    string            `json:"command"`
			Name       string            `json:"name"`
			Env        map[string]string `json:"env"`
			WorkingDir string            `json:"working_dir"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		resp, err := client.ProcessStart(ctx, connect.NewRequest(&remotehandsv1.ProcessStartRequest{
			Command:    args.Command,
			Name:       args.Name,
			Env:        args.Env,
			WorkingDir: args.WorkingDir,
		}))
		if err != nil {
			return mcputils.ErrorToolResult("process_start", err)
		}
		return mcputils.JSONToolResult(map[string]int32{"pid": resp.Msg.Pid})
	})

	s.AddTool(newTool("process_stop", "Stop a background process",
		mcp.WithNumber("pid", mcp.Required(), mcp.Description("Process ID")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			PID int32 `json:"pid"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		_, err := client.ProcessStop(ctx, connect.NewRequest(&remotehandsv1.ProcessStopRequest{
			Pid: args.PID,
		}))
		if err != nil {
			return mcputils.ErrorToolResult("process_stop", err)
		}
		return mcputils.JSONToolResult(map[string]bool{"success": true})
	})

	s.AddTool(newTool("process_list", "List managed processes",
		mcp.WithBoolean("include_exited", mcp.Description("Include exited processes")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			IncludeExited bool `json:"include_exited"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		resp, err := client.ProcessList(ctx, connect.NewRequest(&remotehandsv1.ProcessListRequest{
			IncludeExited: args.IncludeExited,
		}))
		if err != nil {
			return mcputils.ErrorToolResult("process_list", err)
		}
		return mcputils.JSONToolResult(resp.Msg.Processes)
	})

	s.AddTool(newTool("process_logs", "Read process logs from disk",
		mcp.WithNumber("pid", mcp.Required(), mcp.Description("Process ID")),
		mcp.WithNumber("head", mcp.Description("First N lines")),
		mcp.WithNumber("tail", mcp.Description("Last N lines")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			PID  int32 `json:"pid"`
			Head int32 `json:"head"`
			Tail int32 `json:"tail"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}
		resp, err := client.ProcessLogs(ctx, connect.NewRequest(&remotehandsv1.ProcessLogsRequest{
			Pid:  args.PID,
			Head: args.Head,
			Tail: args.Tail,
		}))
		if err != nil {
			return mcputils.ErrorToolResult("process_logs", err)
		}
		return mcputils.JSONToolResult(map[string]string{
			"stdout": resp.Msg.Stdout,
			"stderr": resp.Msg.Stderr,
		})
	})

	s.AddTool(newTool("watch_files", "Watch for file changes matching patterns",
		mcp.WithArray("patterns", mcp.Required(), mcp.Description("Glob patterns to watch")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Patterns []string `json:"patterns"`
		}
		if err := mcputils.CoerceBindArguments(&req, &args); err != nil {
			return mcputils.ErrorToolResult("invalid_arguments", err)
		}

		stream, err := client.WatchFiles(ctx, connect.NewRequest(&remotehandsv1.WatchFilesRequest{
			Patterns: args.Patterns,
		}))
		if err != nil {
			return mcputils.ErrorToolResult("watch_files", err)
		}
		defer stream.Close()

		// Collect events until stream ends or context is cancelled.
		var events []map[string]string
		for stream.Receive() {
			msg := stream.Msg()
			events = append(events, map[string]string{
				"path":       msg.Path,
				"event_type": msg.EventType,
			})
		}
		if err := stream.Err(); err != nil {
			return mcputils.ErrorToolResult("watch_files", err)
		}
		return mcputils.JSONToolResult(events)
	})
}

// newTool is a convenience wrapper around mcp.NewTool.
func newTool(name, description string, opts ...mcp.ToolOption) mcp.Tool {
	return mcp.NewTool(name, append([]mcp.ToolOption{mcp.WithDescription(description)}, opts...)...)
}

// streamingHeaderInterceptor wraps the unary interceptor to also inject headers
// on streaming requests. This is needed for relay mode.
type streamingHeaderInterceptor struct {
	inject func(http.Header)
}

func (s *streamingHeaderInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		s.inject(req.Header())
		return next(ctx, req)
	}
}

func (s *streamingHeaderInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		s.inject(conn.RequestHeader())
		return conn
	}
}

func (s *streamingHeaderInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
