# Implementation Log

## Phase 1: Proto + build fix

**Started**: 2026-03-18

### Task: Create service.proto

Need to reverse-engineer all 27 RPCs from `service.go` and supporting files to create the proto definition. The existing code imports `gen/remotehands/v1` and `gen/remotehands/v1/remotehandsv1connect`, so the proto must produce those packages.

Key observations:
- `buf.yaml` has `modules: [{path: proto}]`
- `buf.gen.yaml` outputs to `gen/` with `paths=source_relative`
- So `proto/remotehands/v1/service.proto` generates into `gen/remotehands/v1/`
- The go_package must be `github.com/mvp-joe/remote-hands/gen/remotehands/v1;remotehandsv1`

Messages needed (derived from service.go, direct_ops.go, and domain files):
- ReadFile: Request(path, offset, limit) / Response(content bytes)
- WriteFile: Request(path, content bytes, mode) / Empty
- DeleteFile: Request(path, recursive) / Empty
- ListFiles: Request(path, recursive) / Response(files[FileEntry])
- Glob: Request(pattern, path) / Response(matches[])
- Grep: Request(pattern, path, glob, ignore_case, context_lines) / Response(matches[GrepMatch])
- RunBash (server-streaming): Request(command, timeout_ms, env map, working_dir) / RunBashEvent oneof(stdout, stderr, exit_code)
- GitStatus: Request(path) / Response(files[GitFileStatus])
- GitDiff: Request(path, staged) / Response(diff)
- GitCommit: Request(message, files[]) / Response(commit_sha)
- WatchFiles (server-streaming): Request(patterns[]) / FileEvent(path, event_type)
- Browser*: 8 RPCs with complex messages
- HttpRequest/HttpClearCookies
- GrpcRequest
- Process*: 5 RPCs including ProcessTail (server-streaming)

Also need enums: ScreenshotFormat, SnapshotType, BrowserActionType, MouseButton, Modifier, WaitForState

### Decisions

- **Empty message**: Using a shared `Empty` message since WriteFile, DeleteFile return it.
- **Enum naming**: Following proto3 convention with SCREAMING_SNAKE_CASE and type prefixes (e.g., `SCREENSHOT_FORMAT_PNG`).

**Result**: Proto generated successfully, `go build ./...` and `go test ./...` pass.

## Phase 2: Auth interceptor

**Reasoning**: ConnectRPC v1.19.1 doesn't have `connect.StreamInterceptorFunc` — only `connect.UnaryInterceptorFunc` and the full `connect.Interceptor` interface. Implemented `NewStreamAuthInterceptor` returning `connect.Interceptor` with no-op unary/streaming-client wraps.

**Result**: 5 unary + 5 streaming auth tests pass via real ConnectRPC server (httptest.NewServer + TLS).

## Phase 3: GitClone + GitPush

**Reasoning**: Created `git_gogit.go` to keep go-git code separate from the exec.Command-based git operations in `git.go`. Used `ssh.InsecureIgnoreHostKey()` for deploy key workflows since known_hosts checking is impractical in automated environments.

**Result**: 3 new tests (invalid SSH key, local clone, local push) pass. All existing tests unaffected.

## Phase 4: cmd/remotehands

Standard ConnectRPC server binary with h2c, signal handling, TCP/Unix socket support.

**Result**: Binary builds, 2 flag validation tests pass.

## Phase 5: cmd/remotehands-mcp

28 MCP tools registered: 23 via Ops interface, 5 via direct ConnectRPC client (process_start, process_stop, process_list, process_logs, watch_files). Used `streamingHeaderInterceptor` implementing full `connect.Interceptor` to inject auth headers on both unary and streaming client calls.

**Result**: Binary builds, 3 tests (relay flag validation, direct mode headers, relay mode headers) pass.

## Final verification

All packages build, all tests pass:
- `cmd/remotehands` - 2 tests
- `cmd/remotehands-mcp` - 3 tests
- `internal/worker` - all existing + 10 auth + 3 git go-git tests
- `mcptools` - all existing
- `mcputils` - all existing
