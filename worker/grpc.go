package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// GrpcResult is the structured result of a gRPC request, ready to be mapped
// to the proto response.
type GrpcResult struct {
	ResponseBody  string
	Metadata      []*remotehandsv1.GrpcMetadata
	StatusCode    int32
	StatusMessage string
}

// grpcRequest executes a gRPC call by shelling out to grpcurl. It supports
// both server reflection (default) and explicit proto file mode.
func (s *Service) grpcRequest(
	ctx context.Context,
	address, method string,
	body *string,
	metadata []*remotehandsv1.GrpcMetadata,
	protoFile *string,
	useReflection bool,
) (*GrpcResult, error) {
	if address == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("address is required"))
	}
	if method == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("method is required"))
	}

	args := []string{"-plaintext"} // local traffic is plaintext

	// Proto file mode vs reflection mode. If useReflection is explicitly
	// true or protoFile is unset, grpcurl uses reflection by default (no
	// flag needed). If protoFile is set, pass -proto.
	if protoFile != nil && *protoFile != "" {
		// Validate the proto file path is under the home directory.
		resolved, err := ValidatePath(s.homeDir, *protoFile)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("proto file path: %w", err))
		}
		args = append(args, "-proto", resolved)
	}

	for _, md := range metadata {
		args = append(args, "-H", fmt.Sprintf("%s: %s", md.Key, md.Value))
	}

	if body != nil && *body != "" {
		args = append(args, "-d", *body)
	}

	args = append(args, address, method)

	cmd := exec.CommandContext(ctx, "grpcurl", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		// Check if grpcurl is not installed.
		if errors.Is(err, exec.ErrNotFound) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("grpcurl not found; ensure it is installed"))
		}

		// Non-zero exit: parse stderr for gRPC status info.
		statusCode, statusMessage := parseGrpcurlError(stderr.String())
		return &GrpcResult{
			ResponseBody:  stdout.String(),
			StatusCode:    statusCode,
			StatusMessage: statusMessage,
		}, nil
	}

	return &GrpcResult{
		ResponseBody: stdout.String(),
		StatusCode:   0, // OK
	}, nil
}

// parseGrpcurlError extracts a gRPC status code and message from grpcurl's
// stderr output. grpcurl typically outputs lines like:
//
//	ERROR:
//	  Code: NotFound
//	  Message: resource not found
//
// If we can't parse a known code, we return UNKNOWN (2).
func parseGrpcurlError(stderr string) (int32, string) {
	code := int32(2) // UNKNOWN
	message := strings.TrimSpace(stderr)

	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Code: ") {
			codeName := strings.TrimPrefix(line, "Code: ")
			if c, ok := grpcCodeMap[codeName]; ok {
				code = c
			}
		}
		if strings.HasPrefix(line, "Message: ") {
			message = strings.TrimPrefix(line, "Message: ")
		}
	}

	return code, message
}

// grpcCodeMap maps grpcurl code names to their numeric gRPC status codes.
var grpcCodeMap = map[string]int32{
	"OK":                 0,
	"Canceled":           1,
	"Unknown":            2,
	"InvalidArgument":    3,
	"DeadlineExceeded":   4,
	"NotFound":           5,
	"AlreadyExists":      6,
	"PermissionDenied":   7,
	"ResourceExhausted":  8,
	"FailedPrecondition": 9,
	"Aborted":            10,
	"OutOfRange":         11,
	"Unimplemented":      12,
	"Internal":           13,
	"Unavailable":        14,
	"DataLoss":           15,
	"Unauthenticated":    16,
}
