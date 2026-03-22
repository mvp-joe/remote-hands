# Architectural Decisions

## 2026-03-22: Custom diff types implementing fdiff interfaces instead of using go-git's built-in diff

**Context:** go-git provides `commit.Patch(other)` for tree-to-tree diffs, but has no built-in API for worktree-to-index or index-to-HEAD diffs. The `Worktree.Status()` method identifies which files changed, but does not produce diff output. We need unified diff output for both staged and unstaged changes.

**Decision:** Implement custom types (`patch`, `filePatch`, `file`, `chunk`) that satisfy the `plumbing/format/diff` interfaces (`Patch`, `FilePatch`, `File`, `Chunk`). Read blob content from the appropriate source (index for unstaged "from", HEAD tree for staged "from"), read the counterpart from filesystem or index respectively, compute diffs using `go-git/v5/utils/diff.Do()`, and feed the results through `fdiff.NewUnifiedEncoder`.

**Consequences:**
- (+) Produces standard unified diff format identical to `git diff` output
- (+) Leverages go-git's existing encoder rather than building a custom formatter
- (+) Works for all change types: modified, added, deleted, binary
- (-) Approximately 150-200 lines of boilerplate for the interface implementations
- (-) Must handle edge cases (new files with no HEAD entry, deleted files with no worktree entry) manually

## 2026-03-22: Four-level author resolution chain instead of silently writing to .gitconfig

**Context:** The current implementation calls `ensureGitConfig` which silently writes `user.email=remotehands@local` and `user.name=Remote Hands` to the repo's git config if not already set. This is a side effect that mutates the repository state and can cause unexpected author names in commits.

**Decision:** Replace `ensureGitConfig` with a read-only resolution chain: (1) per-call `author_name`/`author_email` from the RPC request, (2) init-time config from `ServiceGitOptions`, (3) `.gitconfig` values read via go-git's config API, (4) error if none found. The service never writes to `.gitconfig`.

**Consequences:**
- (+) No hidden side effects on the repository
- (+) Callers have explicit control over author identity
- (+) Init-time defaults cover the common case where a single identity is used for all commits
- (-) Existing callers that relied on the silent fallback to "remotehands@local" will get an error unless they pass author config. This is intentional -- explicit is better than implicit.
- (-) The `TestService_GitCommit_WorksWithNoLocalConfig` test must be updated since the silent fallback no longer exists.

## 2026-03-22: Splitting git.go into one file per operation

**Context:** The current `git.go` contains `gitStatus`, `gitDiff`, `gitCommit`, and helper functions (`parseGitStatus`, `mapGitStatus`, `ensureGitConfig`, `getHeadCommitSHA`). The diff rewrite will add significant code (custom diff types, blob reading logic). Keeping everything in one file would make it ~500+ lines.

**Decision:** Split into `git_status.go`, `git_diff.go`, `git_commit.go`. The existing `git_gogit.go` (clone, push, auth, constructor) stays as-is. Delete `git.go` entirely.

**Consequences:**
- (+) Each file is focused on one operation, matching the project's existing pattern (e.g., `bash.go`, `browser.go`, `process.go`)
- (+) `git_diff.go` can contain the custom diff types alongside the diff logic without cluttering other operations
- (+) Easier to review and navigate
- (-) More files in the worker package (3 new, 1 deleted = net +2 files)

## 2026-03-22: Reusing GitDiffRequest.path for repo path (semantic change)

**Context:** The existing `GitDiffRequest.path` field (field number 1) was being passed as the `filePath` parameter in `service.go` line 190: `s.gitDiff(ctx, "", req.Msg.Path, req.Msg.Staged)`. The repo path was always hardcoded to empty string (defaulting to homeDir). This was inconsistent with `GitStatusRequest.path` which means repo path, and with `GitCommitRequest` which had no path field at all.

**Decision:** Change `GitDiffRequest.path` to mean repo path (consistent with `GitStatusRequest`), and add a new `file_path` field (field 3) for the file filter. Also add `path` field to `GitCommitRequest`. All three git RPCs now consistently use `path` for the repository path.

**Consequences:**
- (+) Consistent semantics across all git RPCs
- (+) Enables operating on repos in subdirectories of homeDir
- (-) This is a semantic breaking change for the `path` field on `GitDiffRequest`. Any existing caller passing a file path in `path` will now interpret it as a repo path. However, since the MCP tool layer controls the mapping and the proto API is not yet public/stable, this is acceptable.

## 2026-03-22: No similarity-based rename detection

**Context:** `git diff` has `-M` (rename detection) which uses content similarity heuristics. go-git's `Worktree.Status()` only reports renames from explicit `git mv` operations (recorded in the index). Implementing similarity detection would require comparing file contents pair-wise.

**Decision:** Only report renames that go-git's `Status()` natively identifies. Do not implement similarity-based rename detection.

**Consequences:**
- (+) Simpler implementation, no quadratic file comparison
- (+) The current CLI-based implementation also did not enable rename detection by default (`git diff` without `-M`)
- (-) Files that are deleted and re-created with similar content will show as separate delete + add rather than a rename
