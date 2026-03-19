package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// validHTTPMethods is the set of HTTP methods we accept.
var validHTTPMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// HttpClient wraps a standard net/http Client with a persistent cookie jar
// for session continuity across requests.
type HttpClient struct {
	client *http.Client   // default: does NOT follow redirects
	jar    http.CookieJar // shared cookie jar
	mu     sync.Mutex
}

// NewHttpClient creates an HttpClient with a cookie jar and a default
// redirect policy that stops at the first redirect (returns
// http.ErrUseLastResponse). Callers that want redirect-following use a
// per-request client that shares the same jar.
func NewHttpClient() *HttpClient {
	jar, _ := cookiejar.New(nil) // only errors on invalid PublicSuffixList
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &HttpClient{client: client, jar: jar}
}

// HttpResult is the structured result of an HTTP request, ready to be
// mapped to the proto response.
type HttpResult struct {
	StatusCode int32
	Headers    []*remotehandsv1.HttpHeader
	Body       []byte
	DurationMs int64
}

// Do executes an HTTP request. It validates the method and URL, optionally
// clears cookies, builds the request, applies headers, handles redirect
// policy, and measures duration.
func (hc *HttpClient) Do(
	ctx context.Context,
	method, rawURL string,
	headers []*remotehandsv1.HttpHeader,
	body []byte,
	followRedirects bool,
	timeoutMs *int32,
	clearCookies bool,
) (*HttpResult, error) {
	// Validate method.
	method = strings.ToUpper(method)
	if !validHTTPMethods[method] {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid HTTP method: %q", method))
	}

	// Validate URL.
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid URL: %w", err))
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("URL must have scheme and host: %q", rawURL))
	}

	if clearCookies {
		hc.ClearCookies()
	}

	// Apply per-request timeout if specified.
	if timeoutMs != nil && *timeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeoutMs)*time.Millisecond)
		defer cancel()
	}

	// Build the request.
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	for _, h := range headers {
		httpReq.Header.Add(h.Name, h.Value)
	}

	// Choose client based on redirect policy.
	hc.mu.Lock()
	jar := hc.jar
	hc.mu.Unlock()

	var resp *http.Response
	start := time.Now()

	if followRedirects {
		// Temporary client that follows redirects, sharing the same jar.
		reqClient := &http.Client{Jar: jar}
		resp, err = reqClient.Do(httpReq)
	} else {
		resp, err = hc.client.Do(httpReq)
	}

	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// Convert response headers.
	respHeaders := make([]*remotehandsv1.HttpHeader, 0, len(resp.Header))
	for name, values := range resp.Header {
		for _, v := range values {
			respHeaders = append(respHeaders, &remotehandsv1.HttpHeader{
				Name:  name,
				Value: v,
			})
		}
	}

	return &HttpResult{
		StatusCode: int32(resp.StatusCode),
		Headers:    respHeaders,
		Body:       respBody,
		DurationMs: duration.Milliseconds(),
	}, nil
}

// ClearCookies replaces the cookie jar with a fresh empty one. This is
// useful for testing auth flows that require a clean session.
func (hc *HttpClient) ClearCookies() {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	jar, _ := cookiejar.New(nil)
	hc.jar = jar
	hc.client.Jar = jar
}
