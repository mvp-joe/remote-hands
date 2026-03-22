# Test Specifications

All tests use `testify/assert` and `testify/require`. Every test and subtest calls `t.Parallel()`. Test repos are initialized using `git init` + `git add` + `git commit` via `exec.Command` in setup helpers (the test infrastructure still uses the git CLI -- only the production code is rewritten).

## Unit Tests

### Status mapping from go-git StatusCodes

Verify that `gitStatus` correctly maps go-git's `StatusCode` values to the project's string statuses.

- Worktree `StatusCode == Modified` on a tracked file produces `"modified"`
- Staging `StatusCode == Added` on a new file produces `"added"`
- Worktree `StatusCode == Deleted` on a tracked file produces `"deleted"`
- Worktree `StatusCode == Untracked` on a new file produces `"untracked"`
- Staging `StatusCode == Renamed` produces `"modified"` (renames treated as modified)
- Staging `StatusCode == Copied` produces `"added"` (copies treated as added)
- Clean repo (no entries with non-Unmodified status) produces empty slice

Note: The old `TestParseGitStatus` and `TestMapGitStatus` tests are removed because the `parseGitStatus` and `mapGitStatus` helper functions no longer exist. The integration tests below cover the same observable behavior.

### Author resolution

- `resolveAuthor` returns per-call values when both `callName` and `callEmail` are non-empty
- `resolveAuthor` returns init-time values when per-call values are empty but `gitAuthorName`/`gitAuthorEmail` are set
- `resolveAuthor` reads `.gitconfig` author when per-call and init-time values are both empty, and the repo has a local git config with `user.name`/`user.email`
- `resolveAuthor` returns error when all three levels are empty (no per-call, no init-time, no .gitconfig)
- `resolveAuthor` returns `CodeInvalidArgument` error when only one of `callName`/`callEmail` is provided (partial per-call)
- Per-call values take priority even when init-time values are also set

## Integration Tests

### GitStatus

**TestService_GitStatus_ModifiedFile**
- Given: a committed file is modified in the working tree
- When: `GitStatus` is called
- Then: response contains one file with status `"modified"`

**TestService_GitStatus_UntrackedFile**
- Given: a new file exists that has never been staged
- When: `GitStatus` is called
- Then: response contains one file with status `"untracked"`

**TestService_GitStatus_AddedFile**
- Given: a new file is staged (`git add`)
- When: `GitStatus` is called
- Then: response contains one file with status `"added"`

**TestService_GitStatus_DeletedFile**
- Given: a committed file is deleted from the filesystem
- When: `GitStatus` is called
- Then: response contains one file with status `"deleted"`

**TestService_GitStatus_MultipleFiles**
- Given: one modified file, one untracked file, one staged new file
- When: `GitStatus` is called
- Then: response contains three files with correct statuses

**TestService_GitStatus_CleanRepo**
- Given: a repo with only committed files, no changes
- When: `GitStatus` is called
- Then: response files list is empty

**TestService_GitStatus_NotARepo**
- Given: a directory that is not a git repository
- When: `GitStatus` is called
- Then: error with `CodeFailedPrecondition`, message contains "not a git repository"

### GitDiff

**TestService_GitDiff_WorkingTreeChanges**
- Given: a committed file is modified in the working tree
- When: `GitDiff` is called with `staged=false`
- Then: output contains the filename, `-` line with old content, `+` line with new content

**TestService_GitDiff_StagedChanges**
- Given: a committed file is modified and staged
- When: `GitDiff` is called with `staged=true`
- Then: output contains the staged changes
- When: `GitDiff` is called with `staged=false`
- Then: output is empty (no unstaged changes)

**TestService_GitDiff_SpecificFile**
- Given: two committed files are both modified in the working tree
- When: `GitDiff` is called with `file_path` set to one file
- Then: output contains only that file's diff, not the other file

**TestService_GitDiff_NoChanges**
- Given: a clean repo with no modifications
- When: `GitDiff` is called
- Then: output is empty string

**TestService_GitDiff_NotARepo**
- Given: a directory that is not a git repository
- When: `GitDiff` is called
- Then: error with `CodeFailedPrecondition`

**TestService_GitDiff_NewFile**
- Given: a new file is staged (`git add`)
- When: `GitDiff` is called with `staged=true`
- Then: output shows the file as a new addition (all lines are `+` lines)

**TestService_GitDiff_DeletedFile**
- Given: a committed file is deleted from the filesystem
- When: `GitDiff` is called with `staged=false`
- Then: output shows the file as deleted (all lines are `-` lines)

**TestService_GitDiff_BinaryFile**
- Given: a committed binary file (containing null bytes) is modified
- When: `GitDiff` is called
- Then: output contains "Binary files" indication rather than line-by-line diff

**TestService_GitDiff_FilePathNoChanges**
- Given: two committed files, only one is modified in the working tree
- When: `GitDiff` is called with `file_path` set to the unmodified file
- Then: output is empty string (no error, just no diff)

### GitCommit

**TestService_GitCommit_CommitsAllStaged**
- Given: a file is staged
- When: `GitCommit` is called with a message
- Then: returns a 40-character hex SHA
- Then: `git log` shows the commit message

**TestService_GitCommit_StagesAndCommitsFiles**
- Given: two unstaged new files exist
- When: `GitCommit` is called with both files in the `files` list
- Then: both files appear in the commit

**TestService_GitCommit_PartialStaging**
- Given: two unstaged new files exist
- When: `GitCommit` is called with only one file in the `files` list
- Then: the other file remains untracked

**TestService_GitCommit_EmptyMessageError**
- Given: any repo state
- When: `GitCommit` is called with empty message
- Then: error with `CodeInvalidArgument`, message contains "commit message is required"

**TestService_GitCommit_NothingToCommit**
- Given: a clean repo with no staged or unstaged changes
- When: `GitCommit` is called
- Then: error with `CodeFailedPrecondition`, message contains "nothing to commit"

**TestService_GitCommit_NotARepo**
- Given: a directory that is not a git repository
- When: `GitCommit` is called
- Then: error with appropriate error code (CodeFailedPrecondition)

**TestService_GitCommit_PerCallAuthor**
- Given: a file is staged, service has no init-time author config
- When: `GitCommit` is called with `author_name="Call User"` and `author_email="call@test.com"`
- Then: the commit's author name is "Call User" and email is "call@test.com"

**TestService_GitCommit_InitTimeAuthor**
- Given: service created with `ServiceGitOptions{AuthorName: "Init User", AuthorEmail: "init@test.com"}`
- Given: a file is staged
- When: `GitCommit` is called without per-call author fields
- Then: the commit's author name is "Init User" and email is "init@test.com"

**TestService_GitCommit_AuthorResolutionOrder**
- Given: service created with `ServiceGitOptions{AuthorName: "Init", AuthorEmail: "init@test.com"}`
- Given: a file is staged
- When: `GitCommit` is called with `author_name="Override"` and `author_email="override@test.com"`
- Then: the commit uses "Override" / "override@test.com" (per-call wins)

**TestService_GitCommit_MissingAuthorError**
- Given: service created without author config, repo has no .gitconfig user settings
- Given: a file is staged
- When: `GitCommit` is called without per-call author fields
- Then: error with `CodeInvalidArgument`

**TestService_GitCommit_PartialAuthorError**
- Given: a file is staged
- When: `GitCommit` is called with `author_name="Foo"` but empty `author_email`
- Then: error with `CodeInvalidArgument`
- When: `GitCommit` is called with empty `author_name` but `author_email="foo@test.com"`
- Then: error with `CodeInvalidArgument`

## Error Scenarios

### Path traversal

**TestService_GitStatus_PathTraversal**
- When: `GitStatus` is called with path `"../../../etc"`
- Then: error with `CodePermissionDenied`

**TestService_GitDiff_PathTraversal**
- When: `GitDiff` is called with repo path `"../../../etc/passwd"`
- Then: error with `CodePermissionDenied`

**TestService_GitCommit_FilePathTraversal**
- When: `GitCommit` is called with a file path `"../../../etc/passwd"` in the files list
- Then: error with `CodePermissionDenied`

**TestService_GitDiff_FilePathTraversal**
- When: `GitDiff` is called with `file_path` set to `"../../../etc/passwd"`
- Then: error with `CodePermissionDenied`

### Constructor errors

**TestNewServiceWithGitAuth_InvalidSSHKey**
- When: `NewServiceWithGitAuth` is called with an invalid SSH key
- Then: returns error containing "parse SSH key"

### Constructor validation

**TestNewServiceWithGitAuth_ValidOptions**
- When: `NewServiceWithGitAuth` is called with valid `ServiceGitOptions` including author name/email
- Then: returns a service with `gitAuthorName` and `gitAuthorEmail` populated
