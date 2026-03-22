package worker

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo initializes a git repository in the given directory.
// Returns a cleanup function that should be deferred.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git init failed")

	// Configure user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git config user.email failed")

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git config user.name failed")
}

// createInitialCommit creates an initial commit so git operations work properly.
func createInitialCommit(t *testing.T, dir string) {
	t.Helper()

	// Create a file and commit it
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0644))

	cmd := exec.Command("git", "add", ".gitkeep")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())
}

// =============================================================================
// GitStatus Tests
// =============================================================================

func TestService_GitStatus_ModifiedFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and commit a file
	testFile := filepath.Join(homeDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("original"), 0644))
	cmd := exec.Command("git", "add", "test.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add test.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Modify the file
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{Path: ""})
	resp, err := svc.GitStatus(ctx, req)
	require.NoError(t, err)

	require.Len(t, resp.Msg.Files, 1)
	assert.Equal(t, "test.txt", resp.Msg.Files[0].Path)
	assert.Equal(t, "modified", resp.Msg.Files[0].Status)
}

func TestService_GitStatus_UntrackedFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create an untracked file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "new.txt"), []byte("content"), 0644))

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{Path: ""})
	resp, err := svc.GitStatus(ctx, req)
	require.NoError(t, err)

	require.Len(t, resp.Msg.Files, 1)
	assert.Equal(t, "new.txt", resp.Msg.Files[0].Path)
	assert.Equal(t, "untracked", resp.Msg.Files[0].Status)
}

func TestService_GitStatus_AddedFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and stage a new file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "staged.txt"), []byte("content"), 0644))
	cmd := exec.Command("git", "add", "staged.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{Path: ""})
	resp, err := svc.GitStatus(ctx, req)
	require.NoError(t, err)

	require.Len(t, resp.Msg.Files, 1)
	assert.Equal(t, "staged.txt", resp.Msg.Files[0].Path)
	assert.Equal(t, "added", resp.Msg.Files[0].Status)
}

func TestService_GitStatus_DeletedFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and commit a file
	testFile := filepath.Join(homeDir, "todelete.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0644))
	cmd := exec.Command("git", "add", "todelete.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add file")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Delete the file
	require.NoError(t, os.Remove(testFile))

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{Path: ""})
	resp, err := svc.GitStatus(ctx, req)
	require.NoError(t, err)

	require.Len(t, resp.Msg.Files, 1)
	assert.Equal(t, "todelete.txt", resp.Msg.Files[0].Path)
	assert.Equal(t, "deleted", resp.Msg.Files[0].Status)
}

func TestService_GitStatus_MultipleFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create committed file, then modify
	committedFile := filepath.Join(homeDir, "committed.txt")
	require.NoError(t, os.WriteFile(committedFile, []byte("original"), 0644))
	cmd := exec.Command("git", "add", "committed.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add file")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	require.NoError(t, os.WriteFile(committedFile, []byte("modified"), 0644))

	// Create untracked file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "untracked.txt"), []byte("new"), 0644))

	// Create and stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "staged.txt"), []byte("staged"), 0644))
	cmd = exec.Command("git", "add", "staged.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{Path: ""})
	resp, err := svc.GitStatus(ctx, req)
	require.NoError(t, err)

	require.Len(t, resp.Msg.Files, 3)

	// Build map for easier assertions
	statuses := make(map[string]string)
	for _, f := range resp.Msg.Files {
		statuses[f.Path] = f.Status
	}

	assert.Equal(t, "modified", statuses["committed.txt"])
	assert.Equal(t, "untracked", statuses["untracked.txt"])
	assert.Equal(t, "added", statuses["staged.txt"])
}

func TestService_GitStatus_CleanRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{Path: ""})
	resp, err := svc.GitStatus(ctx, req)
	require.NoError(t, err)

	assert.Empty(t, resp.Msg.Files)
}

func TestService_GitStatus_NotARepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{Path: ""})
	_, err := svc.GitStatus(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())
	assert.Contains(t, err.Error(), "not a git repository")
}

// =============================================================================
// GitDiff Tests
// =============================================================================

func TestService_GitDiff_WorkingTreeChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and commit a file
	testFile := filepath.Join(homeDir, "diff.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("line1\nline2\n"), 0644))
	cmd := exec.Command("git", "add", "diff.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add diff.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Modify the file
	require.NoError(t, os.WriteFile(testFile, []byte("line1\nline2 modified\n"), 0644))

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: false})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)

	assert.Contains(t, resp.Msg.Diff, "diff.txt")
	assert.Contains(t, resp.Msg.Diff, "-line2")
	assert.Contains(t, resp.Msg.Diff, "+line2 modified")
}

func TestService_GitDiff_StagedChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and commit a file
	testFile := filepath.Join(homeDir, "staged.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("original\n"), 0644))
	cmd := exec.Command("git", "add", "staged.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add staged.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Modify and stage the file
	require.NoError(t, os.WriteFile(testFile, []byte("staged change\n"), 0644))
	cmd = exec.Command("git", "add", "staged.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Regular diff should be empty (no unstaged changes)
	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: false})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Diff)

	// Staged diff should show changes
	req = connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: true})
	resp, err = svc.GitDiff(ctx, req)
	require.NoError(t, err)
	assert.Contains(t, resp.Msg.Diff, "staged.txt")
	assert.Contains(t, resp.Msg.Diff, "-original")
	assert.Contains(t, resp.Msg.Diff, "+staged change")
}

// Task 1: Updated to use FilePath instead of Path for file filtering.
func TestService_GitDiff_SpecificFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and commit two files
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file1.txt"), []byte("content1\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file2.txt"), []byte("content2\n"), 0644))
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add files")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Modify both files
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file1.txt"), []byte("modified1\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file2.txt"), []byte("modified2\n"), 0644))

	// Diff only file1.txt using FilePath (Path now means repo path)
	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Path: "", FilePath: "file1.txt", Staged: false})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)

	assert.Contains(t, resp.Msg.Diff, "file1.txt")
	assert.NotContains(t, resp.Msg.Diff, "file2.txt")
}

func TestService_GitDiff_NoChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: false})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)

	assert.Empty(t, resp.Msg.Diff)
}

func TestService_GitDiff_NotARepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: false})
	_, err := svc.GitDiff(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())
}

// Task 11: New file diff (staged).
func TestService_GitDiff_NewFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create a new file and stage it
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "newfile.txt"), []byte("line1\nline2\n"), 0644))
	cmd := exec.Command("git", "add", "newfile.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: true})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)

	assert.Contains(t, resp.Msg.Diff, "newfile.txt")
	// All lines should be additions
	for _, line := range strings.Split(resp.Msg.Diff, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "diff") || strings.HasPrefix(trimmed, "---") ||
			strings.HasPrefix(trimmed, "+++") || strings.HasPrefix(trimmed, "@@") ||
			strings.HasPrefix(trimmed, "index") || strings.HasPrefix(trimmed, "new file") {
			continue
		}
		assert.True(t, strings.HasPrefix(line, "+"), "expected all content lines to be additions, got: %q", line)
	}
}

// Task 12: Deleted file diff (unstaged).
func TestService_GitDiff_DeletedFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and commit a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "todelete.txt"), []byte("line1\nline2\n"), 0644))
	cmd := exec.Command("git", "add", "todelete.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add todelete.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Delete the file from filesystem
	require.NoError(t, os.Remove(filepath.Join(homeDir, "todelete.txt")))

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: false})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)

	assert.Contains(t, resp.Msg.Diff, "todelete.txt")
	// All content lines should be deletions
	for _, line := range strings.Split(resp.Msg.Diff, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "diff") || strings.HasPrefix(trimmed, "---") ||
			strings.HasPrefix(trimmed, "+++") || strings.HasPrefix(trimmed, "@@") ||
			strings.HasPrefix(trimmed, "index") || strings.HasPrefix(trimmed, "deleted") {
			continue
		}
		assert.True(t, strings.HasPrefix(line, "-"), "expected all content lines to be deletions, got: %q", line)
	}
}

// Task 13: Binary file diff.
func TestService_GitDiff_BinaryFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create a file with binary content (null bytes), commit it
	binaryContent := []byte("hello\x00world\x00binary")
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "binary.dat"), binaryContent, 0644))
	cmd := exec.Command("git", "add", "binary.dat")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add binary file")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Modify the binary file
	modifiedContent := []byte("modified\x00binary\x00data")
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "binary.dat"), modifiedContent, 0644))

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Staged: false})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)

	assert.Contains(t, resp.Msg.Diff, "Binary files")
}

// Task 14: FilePath traversal.
func TestService_GitDiff_FilePathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{
		FilePath: "../../../etc/passwd",
	})
	_, err := svc.GitDiff(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_GitDiff_FilePathNoChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and commit two files
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "changed.txt"), []byte("original\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "unchanged.txt"), []byte("stable\n"), 0644))
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Add files")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Modify only one file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "changed.txt"), []byte("modified\n"), 0644))

	// Diff the unmodified file — should be empty
	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{FilePath: "unchanged.txt", Staged: false})
	resp, err := svc.GitDiff(ctx, req)
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Diff)
}

// =============================================================================
// GitCommit Tests
// =============================================================================

func TestService_GitCommit_CommitsAllStaged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "commit.txt"), []byte("content"), 0644))
	cmd := exec.Command("git", "add", "commit.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "Test commit",
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)

	// SHA should be a valid git hash (40 hex chars)
	assert.Len(t, resp.Msg.CommitSha, 40)
	assert.True(t, isHexString(resp.Msg.CommitSha), "SHA should be hex: %s", resp.Msg.CommitSha)

	// Verify the commit exists
	cmd = exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(output), "Test commit")
}

func TestService_GitCommit_StagesAndCommitsFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create files but don't stage them
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file1.txt"), []byte("content1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file2.txt"), []byte("content2"), 0644))

	// Commit with specific files - should stage them first
	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "Add specific files",
		Files:       []string{"file1.txt", "file2.txt"},
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)

	assert.Len(t, resp.Msg.CommitSha, 40)

	// Verify files are committed
	cmd := exec.Command("git", "show", "--name-only", "--format=")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(output), "file1.txt")
	assert.Contains(t, string(output), "file2.txt")
}

func TestService_GitCommit_PartialStaging(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create multiple files
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "include.txt"), []byte("include"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "exclude.txt"), []byte("exclude"), 0644))

	// Commit only one file
	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "Partial commit",
		Files:       []string{"include.txt"},
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.CommitSha, 40)

	// exclude.txt should still be untracked
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(output), "?? exclude.txt")
}

func TestService_GitCommit_EmptyMessageError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message: "",
	})
	_, err := svc.GitCommit(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
	assert.Contains(t, err.Error(), "commit message is required")
}

func TestService_GitCommit_NothingToCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "Empty commit",
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
	})
	_, err := svc.GitCommit(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())
	assert.Contains(t, err.Error(), "nothing to commit")
}

func TestService_GitCommit_NotARepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create a file without initializing git
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.txt"), []byte("content"), 0644))

	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "Test",
		Files:       []string{"file.txt"},
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
	})
	_, err := svc.GitCommit(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())
}

// Task 5: Renamed from TestService_GitCommit_WorksWithNoLocalConfig.
// Tests per-call author with no local git config.
func TestService_GitCommit_WorksWithPerCallAuthor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Initialize repo without configuring local user
	cmd := exec.Command("git", "init")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Create initial commit using inline config so we have a HEAD
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".gitkeep"), []byte(""), 0644))
	cmd = exec.Command("git", "add", ".gitkeep")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-c", "user.name=Setup", "-c", "user.email=setup@test.com", "commit", "-m", "Initial commit")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Create and stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte("content"), 0644))
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Commit with per-call author (no local git config)
	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "First commit",
		AuthorName:  "Per Call User",
		AuthorEmail: "percall@test.com",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.CommitSha, 40)

	// Verify the commit author
	cmd = exec.Command("git", "log", "--format=%an %ae", "-1")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, strings.TrimSpace(string(output)), "Per Call User percall@test.com")
}

// Task 6: Per-call author overrides repo config.
func TestService_GitCommit_PerCallAuthor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Create and stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "percall.txt"), []byte("content"), 0644))
	cmd := exec.Command("git", "add", "percall.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "Per-call author commit",
		AuthorName:  "Call User",
		AuthorEmail: "call@test.com",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.CommitSha, 40)

	// Verify the commit author is the per-call author, not the repo config
	cmd = exec.Command("git", "log", "--format=%an %ae", "-1")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "Call User call@test.com", strings.TrimSpace(string(output)))
}

// Task 7: Init-time author via ServiceGitOptions.
func TestService_GitCommit_InitTimeAuthor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	homeDir := t.TempDir()
	svc, err := NewServiceWithGitAuth(homeDir, nil, ServiceGitOptions{
		AuthorName:  "Init User",
		AuthorEmail: "init@test.com",
	})
	require.NoError(t, err)

	// Init repo WITHOUT user config
	cmd := exec.Command("git", "init")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Create initial commit using inline config
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".gitkeep"), []byte(""), 0644))
	cmd = exec.Command("git", "add", ".gitkeep")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-c", "user.name=Setup", "-c", "user.email=setup@test.com", "commit", "-m", "Initial commit")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "inittime.txt"), []byte("content"), 0644))
	cmd = exec.Command("git", "add", "inittime.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Commit WITHOUT per-call author fields
	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message: "Init-time author commit",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.CommitSha, 40)

	// Verify commit author is from init-time config
	cmd = exec.Command("git", "log", "--format=%an %ae", "-1")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "Init User init@test.com", strings.TrimSpace(string(output)))
}

// Task 8: Per-call author overrides init-time author.
func TestService_GitCommit_AuthorResolutionOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	homeDir := t.TempDir()
	svc, err := NewServiceWithGitAuth(homeDir, nil, ServiceGitOptions{
		AuthorName:  "Init",
		AuthorEmail: "init@test.com",
	})
	require.NoError(t, err)

	// Init repo and create initial commit
	cmd := exec.Command("git", "init")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".gitkeep"), []byte(""), 0644))
	cmd = exec.Command("git", "add", ".gitkeep")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-c", "user.name=Setup", "-c", "user.email=setup@test.com", "commit", "-m", "Initial commit")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "override.txt"), []byte("content"), 0644))
	cmd = exec.Command("git", "add", "override.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Commit with per-call author that should override init-time
	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message:     "Override author commit",
		AuthorName:  "Override",
		AuthorEmail: "override@test.com",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.CommitSha, 40)

	// Verify per-call author wins
	cmd = exec.Command("git", "log", "--format=%an %ae", "-1")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "Override override@test.com", strings.TrimSpace(string(output)))
}

// Task 9: Missing author produces CodeInvalidArgument.
// Cannot use t.Parallel() because we must override HOME to prevent go-git
// from reading the host's global ~/.gitconfig (which may contain user.name/email).
func TestService_GitCommit_MissingAuthorError(t *testing.T) {
	// Override HOME so go-git's ConfigScoped(GlobalScope) won't find ~/.gitconfig.
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)

	ctx := context.Background()

	homeDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc, err := NewService(homeDir, logger)
	require.NoError(t, err)

	// Init repo WITHOUT user config
	cmd := exec.Command("git", "init")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Create initial commit using inline config
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".gitkeep"), []byte(""), 0644))
	cmd = exec.Command("git", "add", ".gitkeep")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-c", "user.name=Setup", "-c", "user.email=setup@test.com", "commit", "-m", "Initial commit")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "noauthor.txt"), []byte("content"), 0644))
	cmd = exec.Command("git", "add", "noauthor.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Commit without any author
	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message: "No author commit",
	})
	_, err = svc.GitCommit(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

// Task 10: Partial author (name without email or vice versa) produces CodeInvalidArgument.
func TestService_GitCommit_PartialAuthorError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	// Stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "partial.txt"), []byte("content"), 0644))
	cmd := exec.Command("git", "add", "partial.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	t.Run("NameWithoutEmail", func(t *testing.T) {
		t.Parallel()
		req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
			Message:    "Partial author",
			AuthorName: "Foo",
		})
		_, err := svc.GitCommit(ctx, req)
		require.Error(t, err)

		var connErr *connect.Error
		require.ErrorAs(t, err, &connErr)
		assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
	})

	t.Run("EmailWithoutName", func(t *testing.T) {
		t.Parallel()
		req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
			Message:     "Partial author",
			AuthorEmail: "foo@test.com",
		})
		_, err := svc.GitCommit(ctx, req)
		require.Error(t, err)

		var connErr *connect.Error
		require.ErrorAs(t, err, &connErr)
		assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
	})
}

// =============================================================================
// Path Traversal Tests
// =============================================================================

func TestService_GitStatus_PathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GitStatusRequest{
		Path: "../../../etc",
	})
	_, err := svc.GitStatus(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

// Task 2: Path field now means repo path. This still tests repo path traversal.
func TestService_GitDiff_PathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)

	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{
		Path: "../../../etc/passwd",
	})
	_, err := svc.GitDiff(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_GitCommit_FilePathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)
	initGitRepo(t, homeDir)
	createInitialCommit(t, homeDir)

	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message: "Malicious",
		Files:   []string{"../../../etc/passwd"},
	})
	_, err := svc.GitCommit(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

// =============================================================================
// NewServiceWithGitAuth Tests
// =============================================================================

// Task 15: Verify NewServiceWithGitAuth stores options correctly.
func TestNewServiceWithGitAuth_ValidOptions(t *testing.T) {
	t.Parallel()

	svc, err := NewServiceWithGitAuth(t.TempDir(), nil, ServiceGitOptions{
		AuthorName:  "Test",
		AuthorEmail: "test@test.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "Test", svc.gitAuthorName)
	assert.Equal(t, "test@test.com", svc.gitAuthorEmail)
}

// =============================================================================
// Helpers
// =============================================================================

func isHexString(s string) bool {
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		isUpperHex := c >= 'A' && c <= 'F'
		if !isDigit && !isLowerHex && !isUpperHex {
			return false
		}
	}
	return true
}

// =============================================================================
// GitClone / GitPush Tests (go-git)
// =============================================================================

func TestGitClone_InvalidSSHKey(t *testing.T) {
	t.Parallel()

	_, err := NewServiceWithGitAuth(t.TempDir(), nil, ServiceGitOptions{SSHKey: "not-a-valid-pem-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse SSH key")
}

func TestGitClone_NoAuth_LocalRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create a local bare repo to clone from.
	bareDir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = bareDir
	require.NoError(t, cmd.Run())

	// Create a working repo, add a commit, push to the bare repo.
	workDir := t.TempDir()
	initGitRepo(t, workDir)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "remote", "add", "origin", bareDir)
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "push", "origin", "master")
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())

	// Now clone the bare repo using our service.
	homeDir := t.TempDir()
	svc, err := NewServiceWithGitAuth(homeDir, nil, ServiceGitOptions{})
	require.NoError(t, err)

	sha, err := svc.gitClone(ctx, bareDir, "cloned", "", 0)
	require.NoError(t, err)
	assert.Len(t, sha, 40)

	// Verify the cloned file exists.
	content, err := os.ReadFile(filepath.Join(homeDir, "cloned", "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestGitPush_LocalRemote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create a local bare repo.
	bareDir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = bareDir
	require.NoError(t, cmd.Run())

	// Create a working repo, add a commit, push to the bare repo.
	homeDir := t.TempDir()
	workDir := filepath.Join(homeDir, "repo")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	initGitRepo(t, workDir)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "remote", "add", "origin", bareDir)
	cmd.Dir = workDir
	require.NoError(t, cmd.Run())

	// Push using our service.
	svc, err := NewServiceWithGitAuth(homeDir, nil, ServiceGitOptions{})
	require.NoError(t, err)

	err = svc.gitPush(ctx, "repo", "origin", "master", false)
	require.NoError(t, err)

	// Verify the bare repo has the commit.
	cmd = exec.Command("git", "log", "--oneline")
	cmd.Dir = bareDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(output), "init")
}
