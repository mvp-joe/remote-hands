package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/mvp-joe/remote-hands/gen/remotehands/v1/remotehandsv1connect"
)

// startTestServer creates a ConnectRPC server with the given auth token
// and returns a client connected to it.
func startTestServer(t *testing.T, token string) remotehandsv1connect.ServiceClient {
	t.Helper()

	svc, _ := newTestService(t)

	mux := http.NewServeMux()
	path, handler := remotehandsv1connect.NewServiceHandler(svc,
		connect.WithInterceptors(NewAuthInterceptor(token)),
	)
	mux.Handle(path, handler)

	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	client := remotehandsv1connect.NewServiceClient(
		server.Client(),
		server.URL,
	)
	return client
}

// startTestServerWithStreamAuth creates a ConnectRPC server with both unary
// and stream auth interceptors.
func startTestServerWithStreamAuth(t *testing.T, token string) remotehandsv1connect.ServiceClient {
	t.Helper()

	svc, _ := newTestService(t)

	mux := http.NewServeMux()
	path, handler := remotehandsv1connect.NewServiceHandler(svc,
		connect.WithInterceptors(
			NewAuthInterceptor(token),
			NewStreamAuthInterceptor(token),
		),
	)
	mux.Handle(path, handler)

	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	client := remotehandsv1connect.NewServiceClient(
		server.Client(),
		server.URL,
	)
	return client
}

// =============================================================================
// Unary interceptor tests
// =============================================================================

func TestAuthInterceptor_EmptyToken_AllowsAll(t *testing.T) {
	t.Parallel()

	client := startTestServer(t, "")

	// No auth header, should still work
	resp, err := client.ReadFile(context.Background(), connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "nonexistent.txt",
	}))

	// The request should pass auth (we'll get a file-not-found error, not unauthenticated)
	if err != nil {
		assert.NotEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	} else {
		_ = resp
	}
}

func TestAuthInterceptor_ValidToken_Allows(t *testing.T) {
	t.Parallel()

	client := startTestServer(t, "test-secret-token")

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "nonexistent.txt",
	})
	req.Header().Set("Authorization", "Bearer test-secret-token")

	_, err := client.ReadFile(context.Background(), req)

	// Should pass auth — expect file error, not auth error
	if err != nil {
		assert.NotEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	}
}

func TestAuthInterceptor_WrongToken_Rejects(t *testing.T) {
	t.Parallel()

	client := startTestServer(t, "correct-token")

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "test.txt",
	})
	req.Header().Set("Authorization", "Bearer wrong-token")

	_, err := client.ReadFile(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestAuthInterceptor_MissingHeader_Rejects(t *testing.T) {
	t.Parallel()

	client := startTestServer(t, "some-token")

	// No Authorization header
	_, err := client.ReadFile(context.Background(), connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "test.txt",
	}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestAuthInterceptor_MalformedHeader_Rejects(t *testing.T) {
	t.Parallel()

	client := startTestServer(t, "some-token")

	req := connect.NewRequest(&remotehandsv1.ReadFileRequest{
		Path: "test.txt",
	})
	req.Header().Set("Authorization", "Basic dXNlcjpwYXNz")

	_, err := client.ReadFile(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// =============================================================================
// Streaming interceptor tests
// =============================================================================

func TestStreamAuthInterceptor_EmptyToken_AllowsAll(t *testing.T) {
	t.Parallel()

	client := startTestServerWithStreamAuth(t, "")

	// No auth header on a streaming RPC
	stream, err := client.RunBash(context.Background(), connect.NewRequest(&remotehandsv1.RunBashRequest{
		Command: "echo hello",
	}))

	// Should pass auth
	require.NoError(t, err)
	defer stream.Close()

	// Drain the stream
	for stream.Receive() {
	}
	// Check the stream error is not auth-related
	if err := stream.Err(); err != nil {
		assert.NotEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	}
}

func TestStreamAuthInterceptor_ValidToken_Allows(t *testing.T) {
	t.Parallel()

	client := startTestServerWithStreamAuth(t, "stream-token")

	req := connect.NewRequest(&remotehandsv1.RunBashRequest{
		Command: "echo hello",
	})
	req.Header().Set("Authorization", "Bearer stream-token")

	stream, err := client.RunBash(context.Background(), req)
	require.NoError(t, err)
	defer stream.Close()

	// Drain — should get output without auth errors
	for stream.Receive() {
	}
	if err := stream.Err(); err != nil {
		assert.NotEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	}
}

func TestStreamAuthInterceptor_WrongToken_Rejects(t *testing.T) {
	t.Parallel()

	client := startTestServerWithStreamAuth(t, "correct-stream-token")

	req := connect.NewRequest(&remotehandsv1.RunBashRequest{
		Command: "echo hello",
	})
	req.Header().Set("Authorization", "Bearer wrong-stream-token")

	stream, err := client.RunBash(context.Background(), req)
	if err != nil {
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		return
	}
	defer stream.Close()

	// If we got a stream, drain it and check for auth error
	for stream.Receive() {
	}
	require.Error(t, stream.Err())
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(stream.Err()))
}

func TestStreamAuthInterceptor_MissingHeader_Rejects(t *testing.T) {
	t.Parallel()

	client := startTestServerWithStreamAuth(t, "stream-token")

	// No Authorization header
	stream, err := client.RunBash(context.Background(), connect.NewRequest(&remotehandsv1.RunBashRequest{
		Command: "echo hello",
	}))
	if err != nil {
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		return
	}
	defer stream.Close()

	for stream.Receive() {
	}
	require.Error(t, stream.Err())
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(stream.Err()))
}

func TestStreamAuthInterceptor_MalformedHeader_Rejects(t *testing.T) {
	t.Parallel()

	client := startTestServerWithStreamAuth(t, "stream-token")

	req := connect.NewRequest(&remotehandsv1.RunBashRequest{
		Command: "echo hello",
	})
	req.Header().Set("Authorization", "Basic dXNlcjpwYXNz")

	stream, err := client.RunBash(context.Background(), req)
	if err != nil {
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		return
	}
	defer stream.Close()

	for stream.Receive() {
	}
	require.Error(t, stream.Err())
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(stream.Err()))
}
