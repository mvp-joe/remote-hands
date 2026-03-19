package worker

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// parseGrpcurlError Tests
// =============================================================================

func TestParseGrpcurlError_KnownCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stderr      string
		wantCode    int32
		wantMessage string
	}{
		{
			name:        "NotFound",
			stderr:      "ERROR:\n  Code: NotFound\n  Message: resource not found\n",
			wantCode:    5,
			wantMessage: "resource not found",
		},
		{
			name:        "Unavailable",
			stderr:      "ERROR:\n  Code: Unavailable\n  Message: connection refused\n",
			wantCode:    14,
			wantMessage: "connection refused",
		},
		{
			name:        "InvalidArgument",
			stderr:      "ERROR:\n  Code: InvalidArgument\n  Message: bad request\n",
			wantCode:    3,
			wantMessage: "bad request",
		},
		{
			name:        "PermissionDenied",
			stderr:      "ERROR:\n  Code: PermissionDenied\n  Message: access denied\n",
			wantCode:    7,
			wantMessage: "access denied",
		},
		{
			name:        "Unimplemented",
			stderr:      "ERROR:\n  Code: Unimplemented\n  Message: method not found\n",
			wantCode:    12,
			wantMessage: "method not found",
		},
		{
			name:        "DeadlineExceeded",
			stderr:      "ERROR:\n  Code: DeadlineExceeded\n  Message: timeout\n",
			wantCode:    4,
			wantMessage: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, message := parseGrpcurlError(tt.stderr)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantMessage, message)
		})
	}
}

func TestParseGrpcurlError_UnknownCode(t *testing.T) {
	t.Parallel()

	code, message := parseGrpcurlError("ERROR:\n  Code: SomethingWeird\n  Message: unexpected\n")
	assert.Equal(t, int32(2), code) // UNKNOWN
	assert.Equal(t, "unexpected", message)
}

func TestParseGrpcurlError_NoStructuredOutput(t *testing.T) {
	t.Parallel()

	stderr := "Failed to dial target host: connection refused"
	code, message := parseGrpcurlError(stderr)
	assert.Equal(t, int32(2), code) // UNKNOWN
	assert.Equal(t, stderr, message)
}

func TestParseGrpcurlError_EmptyStderr(t *testing.T) {
	t.Parallel()

	code, message := parseGrpcurlError("")
	assert.Equal(t, int32(2), code)
	assert.Equal(t, "", message)
}

func TestParseGrpcurlError_AllCodes(t *testing.T) {
	t.Parallel()

	// Verify all entries in grpcCodeMap are reachable.
	for name, expectedCode := range grpcCodeMap {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stderr := "ERROR:\n  Code: " + name + "\n  Message: test\n"
			code, _ := parseGrpcurlError(stderr)
			assert.Equal(t, expectedCode, code)
		})
	}
}

// =============================================================================
// Service RPC Validation Tests
// =============================================================================

func TestService_GrpcRequest_MissingAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GrpcRequestRequest{
		Address: "",
		Method:  "pkg.Service/Method",
	})

	_, err := svc.GrpcRequest(ctx, req)
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestService_GrpcRequest_MissingMethod(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	req := connect.NewRequest(&remotehandsv1.GrpcRequestRequest{
		Address: "localhost:50051",
		Method:  "",
	})

	_, err := svc.GrpcRequest(ctx, req)
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestService_GrpcRequest_ProtoFilePathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	protoFile := "../../etc/passwd"
	req := connect.NewRequest(&remotehandsv1.GrpcRequestRequest{
		Address:   "localhost:50051",
		Method:    "pkg.Service/Method",
		ProtoFile: &protoFile,
	})

	_, err := svc.GrpcRequest(ctx, req)
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeNotFound, connectErr.Code())
}
