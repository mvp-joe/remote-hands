# Interface & Types

## Proto service definition (`proto/remotehands/v1/service.proto`)

The proto defines the `Service` RPC interface matching the 27 methods already implemented in `internal/worker/service.go`. After running `buf generate`, the generated `gen/remotehands/v1/` package provides the types and ConnectRPC server interface that the existing code already imports.

New RPCs added in this spec:
```protobuf
// GitClone clones a remote repository into a path under homeDir.
rpc GitClone(GitCloneRequest) returns (GitCloneResponse);

// GitPush pushes a local repository's branch to the remote.
rpc GitPush(GitPushRequest) returns (GitPushResponse);
```

```protobuf
// GitCloneRequest
// repo_url:    remote URL (git@github.com:org/repo.git or https://github.com/org/repo.git)
// local_path:  destination path relative to homeDir
// branch:      branch to checkout after clone (empty = default branch)

// GitCloneResponse
// commit_sha:  HEAD commit SHA after clone

// GitPushRequest
// repo_path:  path to local repo relative to homeDir
// remote:     remote name (default "origin")
// branch:     branch to push (empty = current branch)
// force:      force push flag (default false)

// GitPushResponse
// (empty — success indicated by no error)
```

## go-git auth in Service

```go
type Service struct {
    // ... existing fields ...
    gitSSHAuth   transport.AuthMethod  // set if SSH deploy key provided
    gitHTTPSAuth transport.AuthMethod  // set if HTTPS token provided
}

// NewServiceWithGitAuth creates a Service with git credentials loaded into memory.
// sshKey is PEM-encoded private key content (empty = no SSH auth).
// httpsToken is a PAT or OAuth token (empty = no HTTPS auth).
// Credentials are never written to disk — stored as go-git transport.AuthMethod objects.
func NewServiceWithGitAuth(homeDir string, logger *slog.Logger, sshKey, httpsToken string) (*Service, error)
```

Auth method selection: if `RepoURL` starts with `git@` (SSH), use `gitSSHAuth`; if `https://`, use `gitHTTPSAuth`.

## Auth interceptor (`internal/worker/auth.go`)

```go
// NewAuthInterceptor returns a connect.UnaryInterceptorFunc.
// If token is empty, all requests are allowed (no-op).
// Otherwise validates "Authorization: Bearer <token>" on every request.
// Returns connect.CodeUnauthenticated on mismatch or missing header.
func NewAuthInterceptor(token string) connect.UnaryInterceptorFunc

// NewStreamAuthInterceptor is the streaming variant for RunBash, WatchFiles, ProcessTail.
func NewStreamAuthInterceptor(token string) connect.StreamInterceptorFunc
```

## cmd/remotehands flags

```
--listen addr          TCP listen address (default "0.0.0.0:19051")
--socket path          Unix socket path (alternative to --listen; exactly one required)
--home dir             Working directory root (required)
--auth-token-env name  Env var containing bearer token (optional; empty = no auth)
```

Resolves the auth token from the named env var at startup. If both `--listen` and `--socket` are provided, exits with an error.

## cmd/remotehands-mcp flags

```
--addr addr        remotehands server address (default "localhost:19051") [direct mode]
--relay addr       Relay URL (e.g. https://overwatch-relay.fly.dev) [relay mode]
--machine id       Fly machine ID [relay mode; required when --relay is set]
```

**Direct mode** (no `--relay`): connects to `--addr`, injects `Authorization: Bearer $REMOTEHANDS_AUTH_TOKEN` on all requests.

**Relay mode** (`--relay` set): connects to relay URL, injects:
- `Authorization: Bearer $REMOTEHANDS_RELAY_SECRET`
- `X-Target-Machine: <machine-id>`

Header injection is implemented via a ConnectRPC client interceptor, not inside `DirectOps`.

The binary creates a `remotehandsv1connect.ServiceClient` with the appropriate interceptor, wraps it in `DirectOps`, and registers all MCP tools using the `Ops` interface. Tools not in the `Ops` interface (process/watch streaming RPCs) call the ConnectRPC client directly.

## New go-git dependencies

```
github.com/go-git/go-git/v5
github.com/go-git/go-git/v5/plumbing/transport/ssh
```
