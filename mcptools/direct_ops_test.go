package mcptools_test

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workerv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/mvp-joe/remote-hands/mcptools"
)

// mockWorkerClient implements remotehandsv1connect.ServiceClient for testing.
type mockWorkerClient struct {
	runBashFn    func(ctx context.Context, req *connect.Request[workerv1.RunBashRequest]) (*connect.ServerStreamForClient[workerv1.RunBashEvent], error)
	readFileFn   func(ctx context.Context, req *connect.Request[workerv1.ReadFileRequest]) (*connect.Response[workerv1.ReadFileResponse], error)
	writeFileFn  func(ctx context.Context, req *connect.Request[workerv1.WriteFileRequest]) (*connect.Response[workerv1.Empty], error)
	deleteFileFn func(ctx context.Context, req *connect.Request[workerv1.DeleteFileRequest]) (*connect.Response[workerv1.Empty], error)
	listFilesFn  func(ctx context.Context, req *connect.Request[workerv1.ListFilesRequest]) (*connect.Response[workerv1.ListFilesResponse], error)
	globFn       func(ctx context.Context, req *connect.Request[workerv1.GlobRequest]) (*connect.Response[workerv1.GlobResponse], error)
	grepFn       func(ctx context.Context, req *connect.Request[workerv1.GrepRequest]) (*connect.Response[workerv1.GrepResponse], error)
	watchFilesFn func(ctx context.Context, req *connect.Request[workerv1.WatchFilesRequest]) (*connect.ServerStreamForClient[workerv1.FileEvent], error)
	gitCommitFn  func(ctx context.Context, req *connect.Request[workerv1.GitCommitRequest]) (*connect.Response[workerv1.GitCommitResponse], error)
	gitStatusFn  func(ctx context.Context, req *connect.Request[workerv1.GitStatusRequest]) (*connect.Response[workerv1.GitStatusResponse], error)
	gitDiffFn    func(ctx context.Context, req *connect.Request[workerv1.GitDiffRequest]) (*connect.Response[workerv1.GitDiffResponse], error)
}

func (m *mockWorkerClient) RunBash(ctx context.Context, req *connect.Request[workerv1.RunBashRequest]) (*connect.ServerStreamForClient[workerv1.RunBashEvent], error) {
	if m.runBashFn != nil {
		return m.runBashFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) ReadFile(ctx context.Context, req *connect.Request[workerv1.ReadFileRequest]) (*connect.Response[workerv1.ReadFileResponse], error) {
	if m.readFileFn != nil {
		return m.readFileFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) WriteFile(ctx context.Context, req *connect.Request[workerv1.WriteFileRequest]) (*connect.Response[workerv1.Empty], error) {
	if m.writeFileFn != nil {
		return m.writeFileFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) DeleteFile(ctx context.Context, req *connect.Request[workerv1.DeleteFileRequest]) (*connect.Response[workerv1.Empty], error) {
	if m.deleteFileFn != nil {
		return m.deleteFileFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) ListFiles(ctx context.Context, req *connect.Request[workerv1.ListFilesRequest]) (*connect.Response[workerv1.ListFilesResponse], error) {
	if m.listFilesFn != nil {
		return m.listFilesFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) Glob(ctx context.Context, req *connect.Request[workerv1.GlobRequest]) (*connect.Response[workerv1.GlobResponse], error) {
	if m.globFn != nil {
		return m.globFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) Grep(ctx context.Context, req *connect.Request[workerv1.GrepRequest]) (*connect.Response[workerv1.GrepResponse], error) {
	if m.grepFn != nil {
		return m.grepFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) WatchFiles(ctx context.Context, req *connect.Request[workerv1.WatchFilesRequest]) (*connect.ServerStreamForClient[workerv1.FileEvent], error) {
	if m.watchFilesFn != nil {
		return m.watchFilesFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) GitCommit(ctx context.Context, req *connect.Request[workerv1.GitCommitRequest]) (*connect.Response[workerv1.GitCommitResponse], error) {
	if m.gitCommitFn != nil {
		return m.gitCommitFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) GitStatus(ctx context.Context, req *connect.Request[workerv1.GitStatusRequest]) (*connect.Response[workerv1.GitStatusResponse], error) {
	if m.gitStatusFn != nil {
		return m.gitStatusFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) GitDiff(ctx context.Context, req *connect.Request[workerv1.GitDiffRequest]) (*connect.Response[workerv1.GitDiffResponse], error) {
	if m.gitDiffFn != nil {
		return m.gitDiffFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserStart(context.Context, *connect.Request[workerv1.BrowserStartRequest]) (*connect.Response[workerv1.BrowserStartResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserStop(context.Context, *connect.Request[workerv1.BrowserStopRequest]) (*connect.Response[workerv1.BrowserStopResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserNavigate(context.Context, *connect.Request[workerv1.BrowserNavigateRequest]) (*connect.Response[workerv1.BrowserNavigateResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserListPages(context.Context, *connect.Request[workerv1.BrowserListPagesRequest]) (*connect.Response[workerv1.BrowserListPagesResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserClosePage(context.Context, *connect.Request[workerv1.BrowserClosePageRequest]) (*connect.Response[workerv1.BrowserClosePageResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserScreenshot(context.Context, *connect.Request[workerv1.BrowserScreenshotRequest]) (*connect.Response[workerv1.BrowserScreenshotResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserSnapshot(context.Context, *connect.Request[workerv1.BrowserSnapshotRequest]) (*connect.Response[workerv1.BrowserSnapshotResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) BrowserAct(context.Context, *connect.Request[workerv1.BrowserActRequest]) (*connect.Response[workerv1.BrowserActResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) HttpRequest(context.Context, *connect.Request[workerv1.HttpRequestRequest]) (*connect.Response[workerv1.HttpRequestResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) HttpClearCookies(context.Context, *connect.Request[workerv1.HttpClearCookiesRequest]) (*connect.Response[workerv1.HttpClearCookiesResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) GrpcRequest(context.Context, *connect.Request[workerv1.GrpcRequestRequest]) (*connect.Response[workerv1.GrpcRequestResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) ProcessStart(context.Context, *connect.Request[workerv1.ProcessStartRequest]) (*connect.Response[workerv1.ProcessStartResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) ProcessStop(context.Context, *connect.Request[workerv1.ProcessStopRequest]) (*connect.Response[workerv1.ProcessStopResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) ProcessList(context.Context, *connect.Request[workerv1.ProcessListRequest]) (*connect.Response[workerv1.ProcessListResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) ProcessLogs(context.Context, *connect.Request[workerv1.ProcessLogsRequest]) (*connect.Response[workerv1.ProcessLogsResponse], error) {
	return nil, errors.New("not implemented")
}

func (m *mockWorkerClient) ProcessTail(context.Context, *connect.Request[workerv1.ProcessTailRequest]) (*connect.ServerStreamForClient[workerv1.ProcessTailEvent], error) {
	return nil, errors.New("not implemented")
}

func TestDirectOps_ReadFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		offset      int64
		limit       int64
		setupMock   func(*mockWorkerClient)
		wantContent []byte
		wantErr     bool
	}{
		{
			name:   "success",
			path:   "/home/user/test.txt",
			offset: 0,
			limit:  0,
			setupMock: func(m *mockWorkerClient) {
				m.readFileFn = func(ctx context.Context, req *connect.Request[workerv1.ReadFileRequest]) (*connect.Response[workerv1.ReadFileResponse], error) {
					assert.Equal(t, "/home/user/test.txt", req.Msg.Path)
					return connect.NewResponse(&workerv1.ReadFileResponse{
						Content: []byte("hello world"),
					}), nil
				}
			},
			wantContent: []byte("hello world"),
		},
		{
			name:   "with offset and limit",
			path:   "/home/user/test.txt",
			offset: 10,
			limit:  100,
			setupMock: func(m *mockWorkerClient) {
				m.readFileFn = func(ctx context.Context, req *connect.Request[workerv1.ReadFileRequest]) (*connect.Response[workerv1.ReadFileResponse], error) {
					assert.Equal(t, int64(10), req.Msg.Offset)
					assert.Equal(t, int64(100), req.Msg.Limit)
					return connect.NewResponse(&workerv1.ReadFileResponse{
						Content: []byte("partial content"),
					}), nil
				}
			},
			wantContent: []byte("partial content"),
		},
		{
			name:   "error",
			path:   "/nonexistent",
			offset: 0,
			limit:  0,
			setupMock: func(m *mockWorkerClient) {
				m.readFileFn = func(ctx context.Context, req *connect.Request[workerv1.ReadFileRequest]) (*connect.Response[workerv1.ReadFileResponse], error) {
					return nil, errors.New("file not found")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockWorkerClient{}
			tt.setupMock(mock)

			ops := mcptools.NewDirectOps(mock)
			content, err := ops.ReadFile(context.Background(), tt.path, tt.offset, tt.limit)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantContent, content)
		})
	}
}

func TestDirectOps_WriteFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		content   []byte
		mode      int32
		setupMock func(*mockWorkerClient)
		wantErr   bool
	}{
		{
			name:    "success",
			path:    "/home/user/test.txt",
			content: []byte("new content"),
			mode:    0644,
			setupMock: func(m *mockWorkerClient) {
				m.writeFileFn = func(ctx context.Context, req *connect.Request[workerv1.WriteFileRequest]) (*connect.Response[workerv1.Empty], error) {
					assert.Equal(t, "/home/user/test.txt", req.Msg.Path)
					assert.Equal(t, []byte("new content"), req.Msg.Content)
					assert.Equal(t, int32(0644), req.Msg.Mode)
					return connect.NewResponse(&workerv1.Empty{}), nil
				}
			},
		},
		{
			name:    "error",
			path:    "/readonly/test.txt",
			content: []byte("content"),
			mode:    0644,
			setupMock: func(m *mockWorkerClient) {
				m.writeFileFn = func(ctx context.Context, req *connect.Request[workerv1.WriteFileRequest]) (*connect.Response[workerv1.Empty], error) {
					return nil, errors.New("permission denied")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockWorkerClient{}
			tt.setupMock(mock)

			ops := mcptools.NewDirectOps(mock)
			err := ops.WriteFile(context.Background(), tt.path, tt.content, tt.mode)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestDirectOps_ListFiles(t *testing.T) {
	t.Parallel()

	mock := &mockWorkerClient{
		listFilesFn: func(ctx context.Context, req *connect.Request[workerv1.ListFilesRequest]) (*connect.Response[workerv1.ListFilesResponse], error) {
			assert.Equal(t, "/home/user", req.Msg.Path)
			assert.True(t, req.Msg.Recursive)
			return connect.NewResponse(&workerv1.ListFilesResponse{
				Files: []*workerv1.FileEntry{
					{Path: "/home/user/file.txt", Type: "file", Size: 100, ModifiedAt: 1234567890, Mode: "-rw-r--r--"},
					{Path: "/home/user/dir", Type: "directory", Size: 4096, ModifiedAt: 1234567891, Mode: "drwxr-xr-x"},
				},
			}), nil
		},
	}

	ops := mcptools.NewDirectOps(mock)
	files, err := ops.ListFiles(context.Background(), "/home/user", true)

	require.NoError(t, err)
	require.Len(t, files, 2)

	assert.Equal(t, "/home/user/file.txt", files[0].Path)
	assert.Equal(t, "file", files[0].Type)
	assert.Equal(t, int64(100), files[0].Size)

	assert.Equal(t, "/home/user/dir", files[1].Path)
	assert.Equal(t, "directory", files[1].Type)
}

func TestDirectOps_Glob(t *testing.T) {
	t.Parallel()

	mock := &mockWorkerClient{
		globFn: func(ctx context.Context, req *connect.Request[workerv1.GlobRequest]) (*connect.Response[workerv1.GlobResponse], error) {
			assert.Equal(t, "**/*.go", req.Msg.Pattern)
			assert.Equal(t, "/home/user", req.Msg.Path)
			return connect.NewResponse(&workerv1.GlobResponse{
				Matches: []string{"/home/user/main.go", "/home/user/pkg/util.go"},
			}), nil
		},
	}

	ops := mcptools.NewDirectOps(mock)
	matches, err := ops.Glob(context.Background(), "**/*.go", "/home/user")

	require.NoError(t, err)
	assert.Equal(t, []string{"/home/user/main.go", "/home/user/pkg/util.go"}, matches)
}

func TestDirectOps_Grep(t *testing.T) {
	t.Parallel()

	mock := &mockWorkerClient{
		grepFn: func(ctx context.Context, req *connect.Request[workerv1.GrepRequest]) (*connect.Response[workerv1.GrepResponse], error) {
			assert.Equal(t, "TODO", req.Msg.Pattern)
			assert.True(t, req.Msg.IgnoreCase)
			assert.Equal(t, int32(2), req.Msg.ContextLines)
			return connect.NewResponse(&workerv1.GrepResponse{
				Matches: []*workerv1.GrepMatch{
					{
						Path:          "/home/user/main.go",
						Line:          42,
						Content:       "// TODO: fix this",
						ContextBefore: []string{"func main() {", "  // setup"},
						ContextAfter:  []string{"  doSomething()", "}"},
					},
				},
			}), nil
		},
	}

	ops := mcptools.NewDirectOps(mock)
	matches, err := ops.Grep(context.Background(), "TODO", "/home/user", "*.go", true, 2)

	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "/home/user/main.go", matches[0].Path)
	assert.Equal(t, int32(42), matches[0].Line)
	assert.Equal(t, "// TODO: fix this", matches[0].Content)
	assert.Len(t, matches[0].ContextBefore, 2)
	assert.Len(t, matches[0].ContextAfter, 2)
}

func TestDirectOps_GitStatus(t *testing.T) {
	t.Parallel()

	mock := &mockWorkerClient{
		gitStatusFn: func(ctx context.Context, req *connect.Request[workerv1.GitStatusRequest]) (*connect.Response[workerv1.GitStatusResponse], error) {
			return connect.NewResponse(&workerv1.GitStatusResponse{
				Files: []*workerv1.GitFileStatus{
					{Path: "main.go", Status: "modified"},
					{Path: "new.go", Status: "added"},
				},
			}), nil
		},
	}

	ops := mcptools.NewDirectOps(mock)
	files, err := ops.GitStatus(context.Background(), "/home/user")

	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "main.go", files[0].Path)
	assert.Equal(t, "modified", files[0].Status)
}

func TestDirectOps_GitDiff(t *testing.T) {
	t.Parallel()

	mock := &mockWorkerClient{
		gitDiffFn: func(ctx context.Context, req *connect.Request[workerv1.GitDiffRequest]) (*connect.Response[workerv1.GitDiffResponse], error) {
			assert.True(t, req.Msg.Staged)
			return connect.NewResponse(&workerv1.GitDiffResponse{
				Diff: "diff --git a/main.go b/main.go\n+added line",
			}), nil
		},
	}

	ops := mcptools.NewDirectOps(mock)
	diff, err := ops.GitDiff(context.Background(), "/home/user", true)

	require.NoError(t, err)
	assert.Contains(t, diff, "diff --git")
}

func TestDirectOps_GitCommit(t *testing.T) {
	t.Parallel()

	mock := &mockWorkerClient{
		gitCommitFn: func(ctx context.Context, req *connect.Request[workerv1.GitCommitRequest]) (*connect.Response[workerv1.GitCommitResponse], error) {
			assert.Equal(t, "Add new feature", req.Msg.Message)
			assert.Equal(t, []string{"main.go", "util.go"}, req.Msg.Files)
			return connect.NewResponse(&workerv1.GitCommitResponse{
				CommitSha: "abc123def456",
			}), nil
		},
	}

	ops := mcptools.NewDirectOps(mock)
	sha, err := ops.GitCommit(context.Background(), "Add new feature", []string{"main.go", "util.go"})

	require.NoError(t, err)
	assert.Equal(t, "abc123def456", sha)
}

func TestDirectOps_DeleteFile(t *testing.T) {
	t.Parallel()

	mock := &mockWorkerClient{
		deleteFileFn: func(ctx context.Context, req *connect.Request[workerv1.DeleteFileRequest]) (*connect.Response[workerv1.Empty], error) {
			assert.Equal(t, "/home/user/old", req.Msg.Path)
			assert.True(t, req.Msg.Recursive)
			return connect.NewResponse(&workerv1.Empty{}), nil
		},
	}

	ops := mcptools.NewDirectOps(mock)
	err := ops.DeleteFile(context.Background(), "/home/user/old", true)

	require.NoError(t, err)
}
