# Implementation Plan

## Phase 1: Proto and Generated Code

- [x] Update `GitDiffRequest` in `proto/remotehands/v1/service.proto`: add `string file_path = 3`
- [x] Update `GitCommitRequest` in `proto/remotehands/v1/service.proto`: add `string path = 3`, `string author_name = 4`, `string author_email = 5`
- [x] Update service comment block: change "Git operations (exec.Command-based)" to "Git operations (go-git)"
- [x] Run `buf generate` to regenerate `gen/remotehands/v1/service.pb.go`
- [x] Verify build: `go build ./...`

## Phase 2: Constructor and Service Struct

- [x] Define `ServiceGitOptions` struct in `worker/git_gogit.go`
- [x] Rewrite `NewServiceWithGitAuth` to accept `ServiceGitOptions` instead of `sshKey, httpsToken string`
- [x] Add `gitAuthorName` and `gitAuthorEmail` fields to the `Service` struct in `worker/service.go`
- [x] Populate `gitAuthorName`/`gitAuthorEmail` from `ServiceGitOptions` in the constructor
- [x] Update all callers of `NewServiceWithGitAuth` in test helpers: `TestGitClone_InvalidSSHKey`, `TestGitClone_NoAuth_LocalRepo`, `TestGitPush_LocalRemote` in `worker/git_test.go` (`cmd/remotehands/main.go` uses `NewService`, not affected)
- [x] Verify build: `go build ./...`

## Phase 3: gitStatus Rewrite

- [x] Create `worker/git_status.go`
- [x] Implement `gitStatus` using `git.PlainOpen` + `Worktree.Status()`
- [x] Map go-git `StatusCode` values to the existing status strings, preserving current priority: `Staging` field takes precedence over `Worktree` field (e.g., `Staging==Added` → `"added"` even if `Worktree==Modified`); untracked requires both fields == `Untracked`; deleted if either field == `Deleted`; modified if either field == `Modified`; renamed → `"modified"`; copied → `"added"`
- [x] Handle "not a git repository" error from `git.PlainOpen` returning `CodeFailedPrecondition`
- [x] Preserve `ValidatePath` call for repo path sandboxing
- [x] Remove `gitStatus` from `worker/git.go`
- [x] Remove `parseGitStatus` and `mapGitStatus` helper functions (no longer needed)
- [x] Verify tests pass: `go test ./worker/ -run TestService_GitStatus`

## Phase 4: gitDiff Rewrite

- [x] Create `worker/git_diff.go`
- [x] Define custom types implementing `fdiff.Patch`, `fdiff.FilePatch`, `fdiff.File`, `fdiff.Chunk` interfaces (requires `"github.com/go-git/go-git/v5/plumbing/filemode"` import; use `filemode.Regular` for text files)
- [x] Map `diff.Do()` results (`[]diffmatchpatch.Diff`) to `chunk` types: `diffmatchpatch.DiffInsert` → `fdiff.Add`, `DiffDelete` → `fdiff.Delete`, `DiffEqual` → `fdiff.Equal`
- [x] Implement unstaged diff: read "from" content from index blobs, "to" from filesystem via `os.ReadFile`
- [x] Implement staged diff: read "from" content from HEAD tree blobs, "to" from index blobs
- [x] Implement file path filtering: when `filePath` is non-empty, only include matching file in the patch
- [x] Handle new files (no "from" blob): treat as add with empty "from"
- [x] Handle deleted files (no "to" content): treat as delete with empty "to"
- [x] Handle binary files: use content sniffing (`bytes.Contains(content[:min(8000, len)], []byte{0})`) to detect, set `IsBinary() = true`
- [x] Encode output via `fdiff.NewUnifiedEncoder(buf, fdiff.DefaultContextLines).Encode(patch)`
- [x] Handle "not a git repository" error returning `CodeFailedPrecondition`
- [x] Preserve `ValidatePath` calls for both repo path and file path
- [x] Remove `gitDiff` from `worker/git.go`
- [x] Verify tests pass: `go test ./worker/ -run TestService_GitDiff`

## Phase 5: gitCommit Rewrite

- [ ] Create `worker/git_commit.go`
- [ ] Implement `resolveAuthor` with the four-level resolution chain (per-call, init-time, .gitconfig, error)
- [ ] Implement `gitCommit` using `git.PlainOpen` + `Worktree.Add` + `Worktree.Commit`
- [ ] When `repoPath` is empty, default to `s.homeDir` (preserved via `ValidatePath`)
- [ ] Use `object.Signature{Name, Email, When: time.Now()}` for the commit author
- [ ] Return `plumbing.Hash.String()` as the commit SHA
- [ ] Handle "nothing to commit": check `Worktree.Status()` after staging, error if no staged changes
- [ ] Handle "not a git repository" error returning `CodeFailedPrecondition`
- [ ] Handle empty commit message returning `CodeInvalidArgument`
- [ ] Handle missing author config returning `CodeInvalidArgument`
- [ ] Preserve `ValidatePath` calls for repo path and each file path
- [ ] Remove `gitCommit`, `ensureGitConfig`, `getHeadCommitSHA` from `worker/git.go`
- [ ] Verify tests pass: `go test ./worker/ -run TestService_GitCommit`

## Phase 6: Delete git.go and Update Delegation

- [ ] Delete `worker/git.go` entirely
- [ ] Update `Service.GitDiff` in `worker/service.go`: pass `req.Msg.Path` as repoPath, `req.Msg.FilePath` as filePath
- [ ] Update `Service.GitCommit` in `worker/service.go`: change call from `s.gitCommit(ctx, "", req.Msg.Message, req.Msg.Files)` to `s.gitCommit(ctx, req.Msg.Path, req.Msg.Message, req.Msg.Files, req.Msg.AuthorName, req.Msg.AuthorEmail)`
- [ ] Confirm `Service.GitStatus` delegation is already correct (passes `req.Msg.Path` as repoPath -- no change needed)
- [ ] Verify build: `go build ./...`

## Phase 7: Ops Interface and DirectOps

- [ ] Update `GitDiff` signature in `mcptools/ops.go`: add `filePath` parameter
- [ ] Update `GitCommit` signature in `mcptools/ops.go`: add `repoPath`, `authorName`, `authorEmail` parameters
- [ ] Update `DirectOps.GitDiff` in `mcptools/direct_ops.go`: pass `filePath` on proto request
- [ ] Update `DirectOps.GitCommit` in `mcptools/direct_ops.go`: pass `repoPath`, `authorName`, `authorEmail` on proto request
- [ ] Verify build: `go build ./...`

## Phase 8: MCP Tool Registration

- [ ] Add `file_path` string argument to `git_diff` tool in `cmd/remotehands-mcp/main.go`
- [ ] Update `git_diff` handler to pass `args.FilePath` to `ops.GitDiff`
- [ ] Note: `git_diff` tool's `path` argument changes semantics from file filter to repo path -- this is a breaking change for MCP callers
- [ ] Add `path`, `author_name`, `author_email` string arguments to `git_commit` tool
- [ ] Update `git_commit` handler to pass new args to `ops.GitCommit`
- [ ] Verify build: `go build ./cmd/remotehands-mcp/`

## Phase 9: Test Updates

- [ ] Update `TestService_GitDiff_SpecificFile`: change `Path` to repo path (or empty), set `FilePath` to the target filename
- [ ] Update `TestService_GitDiff_PathTraversal`: note that `Path` now means repo path (test still validates traversal rejection, just different semantics)
- [ ] Remove `TestParseGitStatus` and `TestMapGitStatus` unit tests (helpers deleted)
- [ ] Update all existing commit tests (`CommitsAllStaged`, `StagesAndCommitsFiles`, `PartialStaging`, `NothingToCommit`, etc.) to pass `authorName`/`authorEmail` explicitly rather than relying on repo-local `.gitconfig`
- [ ] Rename `TestService_GitCommit_WorksWithNoLocalConfig` to `TestService_GitCommit_WorksWithPerCallAuthor` -- pass `authorName`/`authorEmail` per-call instead of relying on the deleted `ensureGitConfig` fallback
- [ ] Add new test: `TestService_GitCommit_PerCallAuthor` -- verifies per-call author name/email appear in commit
- [ ] Add new test: `TestService_GitCommit_InitTimeAuthor` -- verifies init-time author from `ServiceGitOptions`
- [ ] Add new test: `TestService_GitCommit_AuthorResolutionOrder` -- verifies per-call overrides init-time
- [ ] Add new test: `TestService_GitCommit_MissingAuthorError` -- verifies `CodeInvalidArgument` when no author config exists
- [ ] Add new test: `TestService_GitCommit_PartialAuthorError` -- verifies `CodeInvalidArgument` when only `author_name` or only `author_email` is provided
- [ ] Add new test: `TestService_GitDiff_NewFile` -- staged diff for a brand-new file (staged via `git add`, then `staged=true`)
- [ ] Add new test: `TestService_GitDiff_DeletedFile` -- diff for a deleted tracked file
- [ ] Add new test: `TestService_GitDiff_BinaryFile` -- binary file produces "Binary files differ" output
- [ ] Add new test: `TestService_GitDiff_FilePathTraversal` -- `file_path` set to `"../../../etc/passwd"` returns `CodePermissionDenied`
- [ ] Add or update test: `TestNewServiceWithGitAuth_ValidOptions` -- verify `ServiceGitOptions` with author fields populates service correctly
- [ ] Verify all tests pass: `go test ./...`

## Phase 10: Server Binary Author Config

- [ ] Update `cmd/remotehands/main.go` to read `GIT_AUTHOR_NAME` and `GIT_AUTHOR_EMAIL` env vars
- [ ] If either env var is set, use `NewServiceWithGitAuth` instead of `NewService`, passing author config via `ServiceGitOptions`
- [ ] This is optional for callers — the four-level resolution chain still falls through to `.gitconfig` and per-call params

## Phase 11: Cleanup and Verification

- [ ] Remove `"os/exec"` import from any git-related worker files (should be gone with git.go deletion)
- [ ] Verify `git` binary is not referenced in worker package: `grep -r 'exec.Command.*"git"' worker/`
- [ ] Run full test suite: `go test ./...`
- [ ] Run build for both binaries: `go build ./cmd/remotehands && go build ./cmd/remotehands-mcp`

## Notes

- Phases 3-5 can be done incrementally: implement the new file, remove the old function from `git.go`, test. Only delete `git.go` entirely in Phase 6 once all three functions are moved out.
- The `parseGitStatus` and `mapGitStatus` unit tests are deleted because those functions no longer exist -- go-git's `Worktree.Status()` handles parsing internally. The integration tests (e.g., `TestService_GitStatus_ModifiedFile`) remain and validate the same behavior.
- The constructor signature change (`NewServiceWithGitAuth`) is a breaking change for direct library consumers. This is acceptable since the module is pre-1.0.
