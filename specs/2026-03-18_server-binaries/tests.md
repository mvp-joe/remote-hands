# Tests

## `internal/worker/auth_test.go`

### Unary interceptor (`NewAuthInterceptor`)

- `TestAuthInterceptor_EmptyToken_AllowsAll` — empty token → all requests pass through
- `TestAuthInterceptor_ValidToken_Allows` — correct `Authorization: Bearer <token>` → request proceeds
- `TestAuthInterceptor_WrongToken_Rejects` — wrong token value → `CodeUnauthenticated`
- `TestAuthInterceptor_MissingHeader_Rejects` — no Authorization header → `CodeUnauthenticated`
- `TestAuthInterceptor_MalformedHeader_Rejects` — `Authorization: Basic xxx` (not Bearer) → `CodeUnauthenticated`

### Streaming interceptor (`NewStreamAuthInterceptor`)

- Same 5 cases as above for the streaming variant

## `internal/worker/git_test.go` additions (GitClone/GitPush)

- `TestGitClone_InvalidSSHKey` — malformed SSH PEM key → `NewServiceWithGitAuth` returns error
- `TestGitClone_NoAuth_LocalRepo` — clone a local bare repo (no network, no auth) → succeeds
- `TestGitPush_LocalRemote` — push to a local bare repo → succeeds

## `cmd/remotehands` tests (`cmd/remotehands/main_test.go`)

- `TestMain_MissingHome` — no `--home` flag → exits non-zero with error message
- `TestMain_BothListenAndSocket` — both `--listen` and `--socket` set → exits non-zero

## `cmd/remotehands-mcp` tests (`cmd/remotehands-mcp/main_test.go`)

- `TestMain_RelayModeRequiresMachine` — `--relay` set without `--machine` → exits non-zero
- `TestHeaderInterceptor_DirectMode` — interceptor injects `Authorization: Bearer` from env
- `TestHeaderInterceptor_RelayMode` — interceptor injects relay secret + `X-Target-Machine` header

## What is NOT tested

- go-git GitClone/GitPush over real SSH/HTTPS to remote servers (requires live server; out of scope)
- Full MCP tool registration (tested by starting the binary and calling tools — manual or integration)
- `cmd/remotehands` serving real RPCs (covered by existing `internal/worker/` tests)
