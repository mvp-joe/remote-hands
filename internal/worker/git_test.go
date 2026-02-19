package worker

import (
	"context"
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

	// Diff only file1.txt
	req := connect.NewRequest(&remotehandsv1.GitDiffRequest{Path: "file1.txt", Staged: false})
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
		Message: "Test commit",
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
		Message: "Add specific files",
		Files:   []string{"file1.txt", "file2.txt"},
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
		Message: "Partial commit",
		Files:   []string{"include.txt"},
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
		Message: "Empty commit",
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
		Message: "Test",
		Files:   []string{"file.txt"},
	})
	_, err := svc.GitCommit(ctx, req)
	require.Error(t, err)

	// The error will be from git config or git add
	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
}

func TestService_GitCommit_WorksWithNoLocalConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Initialize repo without configuring local user
	// (global config may or may not exist, which is fine)
	cmd := exec.Command("git", "init")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// Create and stage a file
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte("content"), 0644))
	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = homeDir
	require.NoError(t, cmd.Run())

	// GitCommit should succeed - either using existing global config
	// or setting remotehands@local as fallback
	req := connect.NewRequest(&remotehandsv1.GitCommitRequest{
		Message: "First commit",
	})
	resp, err := svc.GitCommit(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.CommitSha, 40)

	// Verify the commit was actually created
	cmd = exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = homeDir
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(output), "First commit")
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
// parseGitStatus unit tests
// =============================================================================

func TestParseGitStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []*remotehandsv1.GitFileStatus
	}{
		{
			name:  "modified in working tree",
			input: " M file.txt\n",
			expected: []*remotehandsv1.GitFileStatus{
				{Path: "file.txt", Status: "modified"},
			},
		},
		{
			name:  "modified and staged",
			input: "M  file.txt\n",
			expected: []*remotehandsv1.GitFileStatus{
				{Path: "file.txt", Status: "modified"},
			},
		},
		{
			name:  "new file staged",
			input: "A  newfile.txt\n",
			expected: []*remotehandsv1.GitFileStatus{
				{Path: "newfile.txt", Status: "added"},
			},
		},
		{
			name:  "deleted in working tree",
			input: " D deleted.txt\n",
			expected: []*remotehandsv1.GitFileStatus{
				{Path: "deleted.txt", Status: "deleted"},
			},
		},
		{
			name:  "untracked file",
			input: "?? untracked.txt\n",
			expected: []*remotehandsv1.GitFileStatus{
				{Path: "untracked.txt", Status: "untracked"},
			},
		},
		{
			name:  "renamed file",
			input: "R  old.txt -> new.txt\n",
			expected: []*remotehandsv1.GitFileStatus{
				{Path: "new.txt", Status: "modified"},
			},
		},
		{
			name: "multiple files",
			input: ` M modified.txt
A  added.txt
 D deleted.txt
?? untracked.txt
`,
			expected: []*remotehandsv1.GitFileStatus{
				{Path: "modified.txt", Status: "modified"},
				{Path: "added.txt", Status: "added"},
				{Path: "deleted.txt", Status: "deleted"},
				{Path: "untracked.txt", Status: "untracked"},
			},
		},
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name:     "empty lines only",
			input:    "\n\n",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseGitStatus(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			require.Len(t, result, len(tt.expected))
			for i, exp := range tt.expected {
				assert.Equal(t, exp.Path, result[i].Path)
				assert.Equal(t, exp.Status, result[i].Status)
			}
		})
	}
}

func TestMapGitStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		index    byte
		workTree byte
		expected string
	}{
		{'?', '?', "untracked"},
		{'A', ' ', "added"},
		{'A', 'M', "added"},
		{'D', ' ', "deleted"},
		{' ', 'D', "deleted"},
		{'M', ' ', "modified"},
		{' ', 'M', "modified"},
		{'M', 'M', "modified"},
		{'R', ' ', "modified"},
		{'C', ' ', "added"},
		{' ', ' ', ""},
	}

	for _, tt := range tests {
		name := strings.ReplaceAll(string([]byte{tt.index, tt.workTree}), " ", "_")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := mapGitStatus(tt.index, tt.workTree)
			assert.Equal(t, tt.expected, result)
		})
	}
}
