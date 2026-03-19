package worker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// HttpClient Unit Tests
// =============================================================================

func TestHttpClient_Do_SimpleGet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "hello")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response body"))
	}))
	defer srv.Close()

	hc := NewHttpClient()
	result, err := hc.Do(context.Background(), "GET", srv.URL+"/test", nil, nil, false, nil, false)
	require.NoError(t, err)

	assert.Equal(t, int32(http.StatusOK), result.StatusCode)
	assert.Equal(t, []byte("response body"), result.Body)
	assert.True(t, result.DurationMs >= 0)

	// Check response header is present.
	found := false
	for _, h := range result.Headers {
		if h.Name == "X-Custom" && h.Value == "hello" {
			found = true
		}
	}
	assert.True(t, found, "expected X-Custom header in response")
}

func TestHttpClient_Do_PostWithBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"key":"value"}`, string(body))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	hc := NewHttpClient()
	headers := []*remotehandsv1.HttpHeader{
		{Name: "Content-Type", Value: "application/json"},
	}
	result, err := hc.Do(
		context.Background(), "POST", srv.URL,
		headers, []byte(`{"key":"value"}`), false, nil, false,
	)
	require.NoError(t, err)
	assert.Equal(t, int32(http.StatusCreated), result.StatusCode)
}

func TestHttpClient_Do_AllMethods(t *testing.T) {
	t.Parallel()

	methods := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, method, r.Method)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			hc := NewHttpClient()
			result, err := hc.Do(context.Background(), method, srv.URL, nil, nil, false, nil, false)
			require.NoError(t, err)
			assert.Equal(t, int32(http.StatusOK), result.StatusCode)
		})
	}
}

func TestHttpClient_Do_MethodCaseInsensitive(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hc := NewHttpClient()
	result, err := hc.Do(context.Background(), "get", srv.URL, nil, nil, false, nil, false)
	require.NoError(t, err)
	assert.Equal(t, int32(http.StatusOK), result.StatusCode)
}

func TestHttpClient_Do_InvalidMethod(t *testing.T) {
	t.Parallel()

	hc := NewHttpClient()
	_, err := hc.Do(context.Background(), "INVALID", "http://localhost", nil, nil, false, nil, false)
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestHttpClient_Do_InvalidURL(t *testing.T) {
	t.Parallel()

	hc := NewHttpClient()

	t.Run("no scheme", func(t *testing.T) {
		t.Parallel()
		_, err := hc.Do(context.Background(), "GET", "localhost:8080/path", nil, nil, false, nil, false)
		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})

	t.Run("no host", func(t *testing.T) {
		t.Parallel()
		_, err := hc.Do(context.Background(), "GET", "http://", nil, nil, false, nil, false)
		require.Error(t, err)
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	})
}

func TestHttpClient_Do_RedirectNotFollowed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final"))
	}))
	defer srv.Close()

	hc := NewHttpClient()
	result, err := hc.Do(context.Background(), "GET", srv.URL+"/start", nil, nil, false, nil, false)
	require.NoError(t, err)

	// Should stop at the redirect, returning 302.
	assert.Equal(t, int32(http.StatusFound), result.StatusCode)
}

func TestHttpClient_Do_RedirectFollowed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final"))
	}))
	defer srv.Close()

	hc := NewHttpClient()
	result, err := hc.Do(context.Background(), "GET", srv.URL+"/start", nil, nil, true, nil, false)
	require.NoError(t, err)

	// Should follow redirect to the final destination.
	assert.Equal(t, int32(http.StatusOK), result.StatusCode)
	assert.Equal(t, []byte("final"), result.Body)
}

func TestHttpClient_Do_Timeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled by the client timeout.
		<-r.Context().Done()
	}))
	defer srv.Close()

	hc := NewHttpClient()
	timeout := int32(50) // 50ms
	_, err := hc.Do(context.Background(), "GET", srv.URL, nil, nil, false, &timeout, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute request")
}

func TestHttpClient_CookiePersistence(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{
				Name:  "session",
				Value: "abc123",
				Path:  "/",
			})
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/protected" {
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "abc123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("authenticated"))
			return
		}
	}))
	defer srv.Close()

	hc := NewHttpClient()

	// First request: sets cookie.
	_, err := hc.Do(context.Background(), "GET", srv.URL+"/login", nil, nil, false, nil, false)
	require.NoError(t, err)

	// Second request: cookie should be sent automatically.
	result, err := hc.Do(context.Background(), "GET", srv.URL+"/protected", nil, nil, false, nil, false)
	require.NoError(t, err)
	assert.Equal(t, int32(http.StatusOK), result.StatusCode)
	assert.Equal(t, []byte("authenticated"), result.Body)
}

func TestHttpClient_ClearCookies(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{
				Name:  "session",
				Value: "abc123",
				Path:  "/",
			})
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/protected" {
			_, err := r.Cookie("session")
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer srv.Close()

	hc := NewHttpClient()

	// Login to set cookie.
	_, err := hc.Do(context.Background(), "GET", srv.URL+"/login", nil, nil, false, nil, false)
	require.NoError(t, err)

	// Clear cookies.
	hc.ClearCookies()

	// Cookie should no longer be sent.
	result, err := hc.Do(context.Background(), "GET", srv.URL+"/protected", nil, nil, false, nil, false)
	require.NoError(t, err)
	assert.Equal(t, int32(http.StatusUnauthorized), result.StatusCode)
}

func TestHttpClient_Do_ClearCookiesFlag(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{
				Name:  "session",
				Value: "abc123",
				Path:  "/",
			})
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/protected" {
			_, err := r.Cookie("session")
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer srv.Close()

	hc := NewHttpClient()

	// Login.
	_, err := hc.Do(context.Background(), "GET", srv.URL+"/login", nil, nil, false, nil, false)
	require.NoError(t, err)

	// Request with clearCookies=true should clear before executing.
	result, err := hc.Do(context.Background(), "GET", srv.URL+"/protected", nil, nil, false, nil, true)
	require.NoError(t, err)
	assert.Equal(t, int32(http.StatusUnauthorized), result.StatusCode)
}

func TestHttpClient_Do_RequestHeaders(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hc := NewHttpClient()
	headers := []*remotehandsv1.HttpHeader{
		{Name: "Authorization", Value: "Bearer token123"},
		{Name: "X-Custom", Value: "custom-value"},
	}
	result, err := hc.Do(context.Background(), "GET", srv.URL, headers, nil, false, nil, false)
	require.NoError(t, err)
	assert.Equal(t, int32(http.StatusOK), result.StatusCode)
}

// =============================================================================
// Service RPC Integration Tests
// =============================================================================

func TestService_HttpRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	req := connect.NewRequest(&remotehandsv1.HttpRequestRequest{
		Method: "GET",
		Url:    srv.URL,
	})

	resp, err := svc.HttpRequest(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, int32(http.StatusOK), resp.Msg.StatusCode)
	assert.Equal(t, []byte("ok"), resp.Msg.Body)
	assert.True(t, resp.Msg.DurationMs >= 0)
}

func TestService_HttpClearCookies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	// Just verify it doesn't error.
	req := connect.NewRequest(&remotehandsv1.HttpClearCookiesRequest{})
	resp, err := svc.HttpClearCookies(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}
