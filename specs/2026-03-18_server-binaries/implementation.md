# Implementation Plan

## Phase 1: Proto + build fix

> **Current state**: `proto/` is empty, `gen/` doesn't exist — `go build ./...` fails.

- [x] Create `proto/remotehands/v1/service.proto` with all 27 existing RPCs matching `internal/worker/service.go`
  - ReadFile, WriteFile, DeleteFile, ListFiles, Glob, Grep
  - RunBash (server-streaming)
  - GitStatus, GitDiff, GitCommit
  - WatchFiles (server-streaming)
  - BrowserStart, BrowserStop, BrowserNavigate, BrowserListPages, BrowserClosePage, BrowserScreenshot, BrowserSnapshot, BrowserAct
  - HttpRequest, HttpClearCookies
  - GrpcRequest
  - ProcessStart, ProcessStop, ProcessList, ProcessLogs, ProcessTail (server-streaming)
- [x] Run `buf generate` → creates `gen/remotehands/v1/` (proto types + ConnectRPC generated code)
- [x] Verify `go build ./...` passes
- [x] Verify `go test ./...` passes (all existing tests)

## Phase 2: Auth interceptor

- [x] Create `internal/worker/auth.go`
- [x] `NewAuthInterceptor(token string) connect.UnaryInterceptorFunc`
  - Empty token → return no-op interceptor that allows all requests
  - Non-empty token → validate `Authorization: Bearer <token>` header; return `connect.CodeUnauthenticated` on mismatch or missing
- [x] `NewStreamAuthInterceptor(token string) connect.Interceptor` (note: ConnectRPC has no StreamInterceptorFunc; implemented as full Interceptor with no-op unary)
  - Same logic for streaming RPCs (`RunBash`, `WatchFiles`, `ProcessTail`)
- [x] Add `internal/worker/auth_test.go` with the 5 unary + 5 streaming test cases

## Phase 3: GitClone + GitPush (go-git, in-memory auth)

`gitStatus`, `gitDiff`, `gitCommit` remain as `exec.Command("git", ...)` — no migration.

- [x] Add `github.com/go-git/go-git/v5` dependency
- [x] Add `github.com/go-git/go-git/v5/plumbing/transport/ssh` dependency
- [x] Add `gitSSHAuth` and `gitHTTPSAuth` fields to `Service` struct
- [x] Add `NewServiceWithGitAuth(homeDir, logger, sshKey, httpsToken)` constructor
  - Parse SSH key → `ssh.NewPublicKeys` in memory; return error on malformed PEM
  - Parse HTTPS token → `http.BasicAuth{Username: "x-token", Password: httpsToken}` in memory
- [x] Add `gitClone` implementation — `git.PlainClone()` with auth selected by URL scheme (`git@` → SSH, `https://` → HTTPS)
- [x] Add `gitPush` implementation — open repo with `git.PlainOpen()` → `Repository.Push()` with auth selected by URL scheme
- [x] Add proto RPCs: `GitClone`, `GitPush` to `service.proto`, regenerate with `buf generate`
- [x] Add `GitClone` and `GitPush` RPC handlers in `service.go`
- [x] Add `GitClone` and `GitPush` methods to `mcptools/ops.go` Ops interface
- [x] Add `GitClone` and `GitPush` implementations to `mcptools/direct_ops.go`

## Phase 4: cmd/remotehands binary

- [x] Create `cmd/remotehands/main.go`
- [x] Flags: `--listen` (default "0.0.0.0:19051"), `--socket`, `--home` (required), `--auth-token-env`
- [x] Validate exactly one of `--listen`/`--socket` provided
- [x] Resolve auth token from named env var (if `--auth-token-env` provided)
- [x] Create `worker.NewService(homeDir, logger)`
- [x] Build ConnectRPC handler with `NewAuthInterceptor` + `NewStreamAuthInterceptor` (if token set)
- [x] Serve on TCP (`net.Listen("tcp", addr)`) or Unix socket (`net.Listen("unix", socketPath)`)
- [x] SIGTERM/SIGINT graceful shutdown

## Phase 5: cmd/remotehands-mcp binary

- [x] Create `cmd/remotehands-mcp/main.go`
- [x] Flags: `--addr` (default "localhost:19051"), `--relay`, `--machine`
- [x] Validate: if `--relay` set, `--machine` is required
- [x] Build ConnectRPC client interceptor for header injection:
  - Direct mode: `Authorization: Bearer $REMOTEHANDS_AUTH_TOKEN`
  - Relay mode: `Authorization: Bearer $REMOTEHANDS_RELAY_SECRET` + `X-Target-Machine: <machine-id>`
- [x] Create `remotehandsv1connect.ServiceClient` with interceptor
- [x] Wrap in `mcptools.NewDirectOps(client)`
- [x] Register all MCP tools (28 tools):
  - Ops interface tools: delegate to `DirectOps`
  - Process/watch tools (`process_start`, `process_stop`, `process_list`, `process_logs`, `watch_files`): call ConnectRPC client directly
- [x] Serve stdio MCP

## Phase 6: Tests

See [tests.md](tests.md).
