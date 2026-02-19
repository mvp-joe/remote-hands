package worker

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	homeDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	service, err := NewService(homeDir, logger)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service, homeDir
}

// =============================================================================
// WriteFile Tests
// =============================================================================

func TestService_WriteFile_CreatesNewFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := []byte("hello world")
	req := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "test.txt",
		Content: content,
	})

	resp, err := svc.WriteFile(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(homeDir, "test.txt"))
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestService_WriteFile_CreatesParentDirectories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := []byte("nested content")
	req := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "a/b/c/file.txt",
		Content: content,
	})

	resp, err := svc.WriteFile(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify file was created with all parent directories
	data, err := os.ReadFile(filepath.Join(homeDir, "a/b/c/file.txt"))
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestService_WriteFile_SetsFileMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "script.sh",
		Content: []byte("#!/bin/bash\necho hello"),
		Mode:    0755,
	})

	resp, err := svc.WriteFile(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify mode was set
	info, err := os.Stat(filepath.Join(homeDir, "script.sh"))
	require.NoError(t, err)
	// On some systems, mode bits might be masked, so check at least execute bit
	assert.True(t, info.Mode()&0100 != 0, "expected execute bit to be set")
}

func TestService_WriteFile_OverwritesExistingFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	filePath := filepath.Join(homeDir, "existing.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("old content"), 0644))

	newContent := []byte("new content")
	req := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "existing.txt",
		Content: newContent,
	})

	resp, err := svc.WriteFile(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, newContent, data)
}

func TestService_WriteFile_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "../../../etc/passwd",
		Content: []byte("malicious"),
	})

	_, err := svc.WriteFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_WriteFile_RejectsEmptyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "",
		Content: []byte("content"),
	})

	_, err := svc.WriteFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

// =============================================================================
// ReadFile Tests
// =============================================================================

func TestService_ReadFile_ReturnsContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := []byte("file content here")
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), content, 0644))

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "test.txt",
	})

	resp, err := svc.ReadFile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, content, resp.Msg.Content)
}

func TestService_ReadFile_WithOffset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := []byte("0123456789")
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), content, 0644))

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path:   "test.txt",
		Offset: 5,
	})

	resp, err := svc.ReadFile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, []byte("56789"), resp.Msg.Content)
}

func TestService_ReadFile_WithLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := []byte("0123456789")
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), content, 0644))

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path:  "test.txt",
		Limit: 5,
	})

	resp, err := svc.ReadFile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, []byte("01234"), resp.Msg.Content)
}

func TestService_ReadFile_WithOffsetAndLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := []byte("0123456789")
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), content, 0644))

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path:   "test.txt",
		Offset: 3,
		Limit:  4,
	})

	resp, err := svc.ReadFile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, []byte("3456"), resp.Msg.Content)
}

func TestService_ReadFile_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "nonexistent.txt",
	})

	_, err := svc.ReadFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeNotFound, connErr.Code())
}

func TestService_ReadFile_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "../../../etc/passwd",
	})

	_, err := svc.ReadFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_ReadFile_RejectsDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "subdir"), 0755))

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "subdir",
	})

	_, err := svc.ReadFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

// =============================================================================
// DeleteFile Tests
// =============================================================================

func TestService_DeleteFile_RemovesFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	filePath := filepath.Join(homeDir, "todelete.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0644))

	req := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path: "todelete.txt",
	})

	resp, err := svc.DeleteFile(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify file was deleted
	_, err = os.Stat(filePath)
	assert.True(t, os.IsNotExist(err))
}

func TestService_DeleteFile_RemovesEmptyDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	dirPath := filepath.Join(homeDir, "emptydir")
	require.NoError(t, os.MkdirAll(dirPath, 0755))

	req := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path: "emptydir",
	})

	resp, err := svc.DeleteFile(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = os.Stat(dirPath)
	assert.True(t, os.IsNotExist(err))
}

func TestService_DeleteFile_FailsOnNonEmptyDirWithoutRecursive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	dirPath := filepath.Join(homeDir, "nonempty")
	require.NoError(t, os.MkdirAll(dirPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dirPath, "file.txt"), []byte("x"), 0644))

	req := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path:      "nonempty",
		Recursive: false,
	})

	_, err := svc.DeleteFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())

	// Directory should still exist
	info, err := os.Stat(dirPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestService_DeleteFile_RecursivelyRemovesDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	dirPath := filepath.Join(homeDir, "nonempty")
	require.NoError(t, os.MkdirAll(filepath.Join(dirPath, "a/b/c"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dirPath, "file.txt"), []byte("x"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dirPath, "a/file.txt"), []byte("x"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dirPath, "a/b/c/file.txt"), []byte("x"), 0644))

	req := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path:      "nonempty",
		Recursive: true,
	})

	resp, err := svc.DeleteFile(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify entire tree was deleted
	_, err = os.Stat(dirPath)
	assert.True(t, os.IsNotExist(err))
}

func TestService_DeleteFile_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path: "nonexistent",
	})

	_, err := svc.DeleteFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeNotFound, connErr.Code())
}

func TestService_DeleteFile_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path: "../../../tmp/something",
	})

	_, err := svc.DeleteFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_DeleteFile_RejectsEmptyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path: "",
	})

	_, err := svc.DeleteFile(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

// =============================================================================
// Integration: Write -> Read -> Delete roundtrip
// =============================================================================

func TestService_WriteReadDeleteRoundtrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	content := []byte("integration test content")

	// Write
	writeReq := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "roundtrip/test.txt",
		Content: content,
		Mode:    0755,
	})
	_, err := svc.WriteFile(ctx, writeReq)
	require.NoError(t, err)

	// Read back
	readReq := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "roundtrip/test.txt",
	})
	readResp, err := svc.ReadFile(ctx, readReq)
	require.NoError(t, err)
	assert.Equal(t, content, readResp.Msg.Content)

	// Delete
	deleteReq := connect.NewRequest(&remotehandsv1.DeleteFileRequest{
		Path:      "roundtrip",
		Recursive: true,
	})
	_, err = svc.DeleteFile(ctx, deleteReq)
	require.NoError(t, err)

	// Verify deleted
	_, err = svc.ReadFile(ctx, readReq)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeNotFound, connErr.Code())
}

// =============================================================================
// File mode verification
// =============================================================================

func TestService_WriteFile_DefaultMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.WriteFileRequest{
		Path:    "defaultmode.txt",
		Content: []byte("test"),
		Mode:    0, // Not specified
	})

	_, err := svc.WriteFile(ctx, req)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(homeDir, "defaultmode.txt"))
	require.NoError(t, err)

	// Default should be 0644
	expectedMode := fs.FileMode(0644)
	assert.Equal(t, expectedMode, info.Mode().Perm())
}

// =============================================================================
// ListFiles Tests
// =============================================================================

func TestService_ListFiles_ListsDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create some files
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file1.txt"), []byte("a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file2.txt"), []byte("bb"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "subdir"), 0755))

	req := connect.NewRequest(&remotehandsv1.ListFilesRequest{
		Path:      "",
		Recursive: false,
	})

	resp, err := svc.ListFiles(ctx, req)
	require.NoError(t, err)

	// Build a map for easier assertions
	entries := make(map[string]*remotehandsv1.FileEntry)
	for _, e := range resp.Msg.Files {
		entries[e.Path] = e
	}

	// NewService creates .process-logs dir, so expect 4 entries total.
	require.Len(t, resp.Msg.Files, 4)

	assert.Contains(t, entries, "file1.txt")
	assert.Equal(t, "file", entries["file1.txt"].Type)
	assert.Equal(t, int64(1), entries["file1.txt"].Size)

	assert.Contains(t, entries, "file2.txt")
	assert.Equal(t, "file", entries["file2.txt"].Type)
	assert.Equal(t, int64(2), entries["file2.txt"].Size)

	assert.Contains(t, entries, "subdir")
	assert.Equal(t, "directory", entries["subdir"].Type)

	assert.Contains(t, entries, ".process-logs")
	assert.Equal(t, "directory", entries[".process-logs"].Type)
}

func TestService_ListFiles_ListsRecursively(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create nested structure
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "a/b/c"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "root.txt"), []byte("r"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/file_a.txt"), []byte("a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/b/file_b.txt"), []byte("b"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/b/c/file_c.txt"), []byte("c"), 0644))

	req := connect.NewRequest(&remotehandsv1.ListFilesRequest{
		Path:      "",
		Recursive: true,
	})

	resp, err := svc.ListFiles(ctx, req)
	require.NoError(t, err)

	// Should have: root.txt, a/, a/file_a.txt, a/b/, a/b/file_b.txt, a/b/c/, a/b/c/file_c.txt
	paths := make([]string, 0, len(resp.Msg.Files))
	for _, e := range resp.Msg.Files {
		paths = append(paths, e.Path)
	}

	assert.Contains(t, paths, "root.txt")
	assert.Contains(t, paths, "a")
	assert.Contains(t, paths, filepath.Join("a", "file_a.txt"))
	assert.Contains(t, paths, filepath.Join("a", "b"))
	assert.Contains(t, paths, filepath.Join("a", "b", "file_b.txt"))
	assert.Contains(t, paths, filepath.Join("a", "b", "c"))
	assert.Contains(t, paths, filepath.Join("a", "b", "c", "file_c.txt"))
}

func TestService_ListFiles_NotFoundReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.ListFilesRequest{
		Path: "nonexistent",
	})

	_, err := svc.ListFiles(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeNotFound, connErr.Code())
}

func TestService_ListFiles_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.ListFilesRequest{
		Path: "../../../",
	})

	_, err := svc.ListFiles(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_ListFiles_RejectsFileAsPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.txt"), []byte("x"), 0644))

	req := connect.NewRequest(&remotehandsv1.ListFilesRequest{
		Path: "file.txt",
	})

	_, err := svc.ListFiles(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

// =============================================================================
// Glob Tests
// =============================================================================

func TestService_Glob_MatchesTxtFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create files
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file1.txt"), []byte("a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file2.txt"), []byte("b"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.go"), []byte("c"), 0644))

	req := connect.NewRequest(&remotehandsv1.GlobRequest{
		Pattern: "*.txt",
		Path:    "",
	})

	resp, err := svc.Glob(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.Matches, 2)
	assert.Contains(t, resp.Msg.Matches, "file1.txt")
	assert.Contains(t, resp.Msg.Matches, "file2.txt")
}

func TestService_Glob_MatchesDoubleStarPattern(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create nested go files
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "a/b/c"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "root.go"), []byte("r"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/a.go"), []byte("a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/b/b.go"), []byte("b"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/b/c/c.go"), []byte("c"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/b/c/c.txt"), []byte("txt"), 0644))

	req := connect.NewRequest(&remotehandsv1.GlobRequest{
		Pattern: "**/*.go",
		Path:    "",
	})

	resp, err := svc.Glob(ctx, req)
	require.NoError(t, err)

	// Should match all .go files in subdirectories
	assert.Contains(t, resp.Msg.Matches, filepath.Join("a", "a.go"))
	assert.Contains(t, resp.Msg.Matches, filepath.Join("a", "b", "b.go"))
	assert.Contains(t, resp.Msg.Matches, filepath.Join("a", "b", "c", "c.go"))
	// root.go might or might not be included depending on ** behavior
	// doublestar's ** requires at least one directory level
	assert.NotContains(t, resp.Msg.Matches, filepath.Join("a", "b", "c", "c.txt"))
}

func TestService_Glob_NoMatchesReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.txt"), []byte("a"), 0644))

	req := connect.NewRequest(&remotehandsv1.GlobRequest{
		Pattern: "*.xyz",
		Path:    "",
	})

	resp, err := svc.Glob(ctx, req)
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Matches)
}

func TestService_Glob_InvalidPatternReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GlobRequest{
		Pattern: "[invalid",
		Path:    "",
	})

	_, err := svc.Glob(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

func TestService_Glob_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GlobRequest{
		Pattern: "*.txt",
		Path:    "../../../",
	})

	_, err := svc.Glob(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_Glob_NotFoundReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GlobRequest{
		Pattern: "*.txt",
		Path:    "nonexistent",
	})

	_, err := svc.Glob(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeNotFound, connErr.Code())
}
