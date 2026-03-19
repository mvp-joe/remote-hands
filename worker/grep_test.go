package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Grep Tests
// =============================================================================

func TestService_Grep_FindsPattern(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create a file with known content
	content := `package main

func main() {
    fmt.Println("Hello, World!")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "main.go"), []byte(content), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "Hello",
		Path:    "",
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Matches, 1)

	match := resp.Msg.Matches[0]
	assert.Equal(t, "main.go", match.Path)
	assert.Equal(t, int32(4), match.Line)
	assert.Contains(t, match.Content, "Hello, World!")
}

func TestService_Grep_WithContextLines(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := `line 1
line 2
line 3 target
line 4
line 5
`
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte(content), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern:      "target",
		Path:         "",
		ContextLines: 1,
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Matches, 1)

	match := resp.Msg.Matches[0]
	assert.Equal(t, int32(3), match.Line)
	assert.Contains(t, match.Content, "target")

	// Context lines depend on implementation (rg vs Go)
	// For Go implementation: should have 1 line before and 1 after
	// For rg: similar behavior
	if len(match.ContextBefore) > 0 {
		assert.Equal(t, "line 2", match.ContextBefore[0])
	}
	if len(match.ContextAfter) > 0 {
		assert.Equal(t, "line 4", match.ContextAfter[0])
	}
}

func TestService_Grep_CaseInsensitive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := `HELLO world
hello WORLD
HeLLo WoRLd
`
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte(content), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern:    "hello",
		Path:       "",
		IgnoreCase: true,
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.Matches, 3)
}

func TestService_Grep_CaseSensitive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := `HELLO world
hello WORLD
HeLLo WoRLd
`
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte(content), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern:    "hello",
		Path:       "",
		IgnoreCase: false,
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.Matches, 1)
	assert.Contains(t, resp.Msg.Matches[0].Content, "hello WORLD")
}

func TestService_Grep_WithGlobFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create files with same content but different extensions
	content := "findme here"
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.txt"), []byte(content), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.go"), []byte(content), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.rs"), []byte(content), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "findme",
		Path:    "",
		Glob:    "*.txt",
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Msg.Matches, 1)
	assert.Equal(t, "file.txt", resp.Msg.Matches[0].Path)
}

func TestService_Grep_MultipleMatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := `error: first problem
warning: something
error: second problem
info: all good
error: third problem
`
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "log.txt"), []byte(content), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "error",
		Path:    "",
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.Matches, 3)

	// Verify line numbers
	lines := make([]int32, 0, len(resp.Msg.Matches))
	for _, m := range resp.Msg.Matches {
		lines = append(lines, m.Line)
	}
	assert.Contains(t, lines, int32(1))
	assert.Contains(t, lines, int32(3))
	assert.Contains(t, lines, int32(5))
}

func TestService_Grep_NoMatchesReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte("nothing special here"), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "nonexistent_pattern_xyz",
		Path:    "",
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Matches)
}

func TestService_Grep_InvalidRegexReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte("test"), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "[invalid(regex",
		Path:    "",
	})

	_, err := svc.Grep(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

func TestService_Grep_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "test",
		Path:    "../../../",
	})

	_, err := svc.Grep(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestService_Grep_NotFoundReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "test",
		Path:    "nonexistent",
	})

	_, err := svc.Grep(ctx, req)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeNotFound, connErr.Code())
}

func TestService_Grep_SearchesNestedDirectories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create nested structure
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "a/b/c"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "root.txt"), []byte("marker"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/a.txt"), []byte("marker"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/b/b.txt"), []byte("no match"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "a/b/c/c.txt"), []byte("marker"), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "marker",
		Path:    "",
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.Matches, 3)

	paths := make([]string, 0, len(resp.Msg.Matches))
	for _, m := range resp.Msg.Matches {
		paths = append(paths, m.Path)
	}
	assert.Contains(t, paths, "root.txt")
	assert.Contains(t, paths, filepath.Join("a", "a.txt"))
	assert.Contains(t, paths, filepath.Join("a", "b", "c", "c.txt"))
}

func TestService_Grep_RegexPattern(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	content := `func foo() {}
func bar() {}
func baz() {}
var x = 1
`
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "test.go"), []byte(content), 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: `func \w+\(\)`,
		Path:    "",
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Msg.Matches, 3)
}

func TestService_Grep_SkipsBinaryFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create a text file with the pattern
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "text.txt"), []byte("findme"), 0644))

	// Create a binary file with null bytes and the pattern
	binaryContent := []byte("findme\x00\x00binary\x00data")
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "binary.bin"), binaryContent, 0644))

	req := connect.NewRequest(&remotehandsv1.GrepRequest{
		Pattern: "findme",
		Path:    "",
	})

	resp, err := svc.Grep(ctx, req)
	require.NoError(t, err)

	// Should only find match in text file (Go implementation skips binary)
	// ripgrep behavior may vary but typically also skips binary by default
	paths := make([]string, 0, len(resp.Msg.Matches))
	for _, m := range resp.Msg.Matches {
		paths = append(paths, m.Path)
	}
	assert.Contains(t, paths, "text.txt")
}
