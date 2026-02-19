# CLAUDE.md

## Quick Reference

| Command | Purpose |
|---------|---------|
| `go build ./cmd/remotehands` | Build ConnectRPC server |
| `go build ./cmd/remotehands-mcp` | Build MCP server |
| `go test ./...` | Run all tests |
| `go test ./internal/worker/` | Test core worker package |
| `buf generate` | Regenerate proto/ConnectRPC code |

## Project Overview

Remote Hands is a ConnectRPC API server for secure remote execution. The MCP server is a secondary wrapper that connects to it.

**Module**: `github.com/mvp-joe/remote-hands`
**Go version**: 1.25.5

## Project Structure

| Directory | Purpose |
|-----------|---------|
| `cmd/remotehands/` | ConnectRPC server binary (TCP or Unix socket, h2c) |
| `cmd/remotehands-mcp/` | MCP server binary (connects to remotehands, serves stdio) |
| `internal/worker/` | Core service implementation — one file per domain |
| `mcptools/` | `Ops` interface + `DirectOps` ConnectRPC client adapter |
| `mcputils/` | MCP argument coercion and result helpers |
| `proto/remotehands/v1/` | Protobuf service definition |
| `gen/remotehands/v1/` | Generated Go code (committed, regenerate with `buf generate`) |

## Architecture

Two-binary design:

```
LLM <--stdio/MCP--> remotehands-mcp <--ConnectRPC--> remotehands <--> OS
```

The ConnectRPC server is the primary interface. Any ConnectRPC/gRPC client can use it directly. The MCP server is one client that bridges to LLM tool use.

## Key Patterns

**Service delegation**: `service.go` is a thin adapter — each public RPC method delegates to a private method in the corresponding domain file (e.g., `RunBash` calls `s.runBash` in `bash.go`).

**Path sandboxing**: Every filesystem operation calls `ValidatePath(homeDir, path)` in `path.go`. This resolves symlinks and rejects traversal attempts. It is the security boundary.

**Error codes**: Use `connect.NewError()` with appropriate codes — `CodePermissionDenied` for path traversal, `CodeNotFound` for missing files, `CodeInvalidArgument` for bad input.

**MCP tool handlers**: Bind arguments with `mcputils.CoerceBindArguments`, call `ops.Method()`, return `mcputils.JSONToolResult` or `mcputils.ErrorToolResult`.

**Streaming RPCs**: `RunBash`, `WatchFiles`, `ProcessTail` are server-streaming. `DirectOps` collects `RunBash` streaming output into a single `BashResult` for MCP.

## Key Files

| File | What It Does |
|------|-------------|
| `internal/worker/service.go` | Service struct, RPC delegation layer |
| `internal/worker/path.go` | `ValidatePath` — sandbox enforcement |
| `internal/worker/bash.go` | Command execution with process groups, streaming, timeout |
| `internal/worker/browser.go` | `BrowserManager` — go-rod headless Chromium |
| `internal/worker/process.go` | `ProcessManager` — background process lifecycle |
| `internal/worker/http.go` | `HttpClient` — persistent cookie jar |
| `mcptools/ops.go` | `Ops` interface (abstraction between MCP and backend) |
| `mcptools/direct_ops.go` | `DirectOps` — implements `Ops` via ConnectRPC |
| `mcputils/coerce.go` | `CoerceBindArguments` — handles MCP string-typed args |
| `cmd/remotehands-mcp/main.go` | MCP tool registration (21 tools) |

## Testing Conventions

- All tests use `testify/assert` and `testify/require`
- Every test and subtest calls `t.Parallel()`
- Table-driven tests are the default pattern
- `newTestService(t)` helper (in `file_test.go`) creates a service with `t.TempDir()` as home
- Git tests require `git` in PATH; browser tests are pure unit tests (no real browser)
- No build tags — all tests run with `go test ./...`

## Runtime Dependencies

| Tool | Required By | Required? |
|------|------------|-----------|
| `bash` | `RunBash` | Yes |
| `git` | `GitStatus`, `GitDiff`, `GitCommit` | Yes (for git features) |
| `chromium` | `Browser*` RPCs | Yes (for browser features) |
| `grpcurl` | `GrpcRequest` | Yes (for gRPC proxying) |
| `rg` (ripgrep) | `Grep` | No — pure-Go fallback |
