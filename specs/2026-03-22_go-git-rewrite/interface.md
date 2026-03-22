# Interface Definitions

## Proto Changes

### GitDiffRequest (service.proto)

```protobuf
message GitDiffRequest {
  string path = 1;           // repo path (empty = homeDir) -- EXISTING, semantics change
  bool staged = 2;           // EXISTING
  string file_path = 3;      // NEW: optional, filter diff to a specific file
}
```

### GitCommitRequest (service.proto)

```protobuf
message GitCommitRequest {
  string message = 1;        // EXISTING
  repeated string files = 2; // EXISTING
  string path = 3;           // NEW: repo path (empty = homeDir)
  string author_name = 4;    // NEW: optional per-call author name
  string author_email = 5;   // NEW: optional per-call author email
}
```

### GitStatusRequest (service.proto) -- no change

```protobuf
message GitStatusRequest {
  string path = 1;           // repo path (empty = homeDir) -- UNCHANGED
}
```

## Worker Package

### ServiceGitOptions (worker/git_gogit.go)

```go
type ServiceGitOptions struct {
    SSHKey      string // PEM-encoded private key (empty = no SSH auth)
    HTTPSToken  string // PAT or OAuth token (empty = no HTTPS auth)
    AuthorName  string // default author name for commits
    AuthorEmail string // default author email for commits
}
```

### NewServiceWithGitAuth (worker/git_gogit.go)

```go
func NewServiceWithGitAuth(homeDir string, logger *slog.Logger, opts ServiceGitOptions) (*Service, error)
```

### Service struct additions (worker/service.go)

```go
type Service struct {
    // ... existing fields ...

    // go-git auth
    gitSSHAuth     transport.AuthMethod
    gitHTTPSAuth   transport.AuthMethod

    // go-git commit author defaults
    gitAuthorName  string
    gitAuthorEmail string
}
```

### gitStatus (worker/git_status.go)

```go
func (s *Service) gitStatus(ctx context.Context, repoPath string) ([]*remotehandsv1.GitFileStatus, error)
```

Unchanged external signature. Internally uses `git.PlainOpen` + `Worktree.Status()`.

### gitDiff (worker/git_diff.go)

```go
func (s *Service) gitDiff(ctx context.Context, repoPath, filePath string, staged bool) (string, error)
```

Unchanged method signature. The caller semantics change: `repoPath` now receives the proto `path` field directly (was previously hardcoded to `""`), and `filePath` receives the new proto `file_path` field.

### Custom diff types (worker/git_diff.go)

All unexported, implementing `plumbing/format/diff` interfaces.
Hashes computed via `plumbing.ComputeHash(plumbing.BlobObject, []byte(content))` which returns a single `plumbing.Hash` (no error return).
Binary detection: use `object.File.IsBinary()` (returns `(bool, error)`) when reading from object store; null-byte sniff first 8KB for filesystem content.

```go
type patch struct       // implements fdiff.Patch
type filePatch struct    // implements fdiff.FilePatch
type file struct         // implements fdiff.File
type chunk struct        // implements fdiff.Chunk
```

### gitCommit (worker/git_commit.go)

```go
func (s *Service) gitCommit(ctx context.Context, repoPath, message string, files []string, authorName, authorEmail string) (string, error)
```

Signature changes: adds `authorName` and `authorEmail` parameters.

### resolveAuthor (worker/git_commit.go)

```go
func (s *Service) resolveAuthor(repoPath, callName, callEmail string) (string, string, error)
```

Returns `(name, email, error)`. Resolution order:
1. Per-call parameters — only if **both** `callName` and `callEmail` are non-empty. If only one is provided, return `CodeInvalidArgument` error.
2. Init-time config (`s.gitAuthorName`/`s.gitAuthorEmail`) — only if both are non-empty.
3. `.gitconfig` from repo-local or global config via go-git's config reader.
4. Error (`CodeInvalidArgument`) if none found.

## Service RPC Delegation Changes (worker/service.go)

### GitDiff

```go
func (s *Service) GitDiff(
    ctx context.Context,
    req *connect.Request[remotehandsv1.GitDiffRequest],
) (*connect.Response[remotehandsv1.GitDiffResponse], error)
```

Now passes `req.Msg.Path` as repoPath and `req.Msg.FilePath` as filePath.

### GitCommit

```go
func (s *Service) GitCommit(
    ctx context.Context,
    req *connect.Request[remotehandsv1.GitCommitRequest],
) (*connect.Response[remotehandsv1.GitCommitResponse], error)
```

Now passes `req.Msg.Path`, `req.Msg.AuthorName`, `req.Msg.AuthorEmail`.

## Ops Interface Changes (mcptools/ops.go)

### GitDiff -- signature change to add filePath

```go
GitDiff(ctx context.Context, repoPath string, filePath string, staged bool) (string, error)
```

### GitCommit -- signature change

```go
GitCommit(ctx context.Context, repoPath string, message string, files []string, authorName, authorEmail string) (string, error)
```

## DirectOps Changes (mcptools/direct_ops.go)

### GitDiff

```go
func (d *DirectOps) GitDiff(ctx context.Context, repoPath string, filePath string, staged bool) (string, error)
```

Passes `repoPath` and `filePath` as separate fields on the proto request.

### GitCommit

```go
func (d *DirectOps) GitCommit(ctx context.Context, repoPath string, message string, files []string, authorName, authorEmail string) (string, error)
```

Passes `authorName` and `authorEmail` as fields on the proto request.

## MCP Tool Changes (cmd/remotehands-mcp/main.go)

### git_diff tool

Add optional `file_path` argument. Tool handler calls `ops.GitDiff(ctx, args.Path, args.FilePath, args.Staged)`.

### git_commit tool

Add optional `path`, `author_name`, `author_email` arguments. Tool handler calls `ops.GitCommit(ctx, args.Path, args.Message, args.Files, args.AuthorName, args.AuthorEmail)`.
