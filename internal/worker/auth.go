package worker

import (
	"context"
	"strings"

	"connectrpc.com/connect"
)

// NewAuthInterceptor returns a unary interceptor that validates bearer tokens.
// If token is empty, all requests are allowed (no-op).
// Otherwise validates "Authorization: Bearer <token>" on every request.
// Returns connect.CodeUnauthenticated on mismatch or missing header.
func NewAuthInterceptor(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token == "" {
				return next(ctx, req)
			}
			if err := validateBearerToken(req.Header(), token); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}
}

// streamAuthInterceptor validates bearer tokens on streaming RPCs.
// It implements connect.Interceptor, wrapping only the streaming handler side.
// Unary and streaming client methods are pass-through.
type streamAuthInterceptor struct {
	token string
}

// NewStreamAuthInterceptor returns a streaming interceptor that validates bearer tokens.
// Same semantics as NewAuthInterceptor but for streaming RPCs (RunBash, WatchFiles, ProcessTail).
func NewStreamAuthInterceptor(token string) connect.Interceptor {
	return &streamAuthInterceptor{token: token}
}

func (s *streamAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return next // no-op for unary; handled by NewAuthInterceptor
}

func (s *streamAuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next // server-side interceptor; no effect on client streams
}

func (s *streamAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if s.token == "" {
			return next(ctx, conn)
		}
		if err := validateBearerToken(conn.RequestHeader(), s.token); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// validateBearerToken checks the Authorization header for a valid Bearer token.
func validateBearerToken(headers interface{ Get(string) string }, expected string) error {
	auth := headers.Get("Authorization")
	if auth == "" {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	if auth[7:] != expected {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	return nil
}
