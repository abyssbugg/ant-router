package anthropic

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/execution"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func sdkError(status int, errorType, message, retryAfter string) error {
	response := &http.Response{StatusCode: status, Header: make(http.Header)}
	if retryAfter != "" {
		response.Header.Set("Retry-After", retryAfter)
	}

	// Create a proper Anthropic SDK error by unmarshaling from JSON
	errorBody := map[string]interface{}{
		"type":    errorType,
		"message": message,
	}
	jsonBody, _ := json.Marshal(errorBody)
	response.Body = io.NopCloser(io.Reader(nil))

	err := &sdk.Error{
		StatusCode: status,
		Response:   response,
		RequestID:  "test-request-id",
	}
	_ = err.UnmarshalJSON(jsonBody)

	return err
}

func TestNormalizeProviderErrorCodes(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  execution.ErrorCode
		retryable bool
		retry     time.Duration
	}{
		{"invalid_model_404", sdkError(404, "not_found_error", "model not found", ""), execution.CodeInvalidModel, false, 0},
		{"invalid_request_400", sdkError(400, "invalid_request_error", "bad input", ""), execution.CodeInvalidRequest, false, 0},
		{"model_in_message_400", sdkError(400, "invalid_request_error", "vision model not supported", ""), execution.CodeInvalidRequest, false, 0},
		{"conflict_409", sdkError(409, "invalid_request_error", "conflict", ""), execution.CodeInvalidRequest, false, 0},
		{"payload_too_large_413", sdkError(413, "invalid_request_error", "too large", ""), execution.CodeInvalidRequest, false, 0},
		{"unprocessable_422", sdkError(422, "invalid_request_error", "unprocessable", ""), execution.CodeInvalidRequest, false, 0},
		{"overloaded_529", sdkError(529, "overloaded_error", "overloaded", "9"), execution.CodeUnavailable, true, 0},
		{"authentication_401", sdkError(401, "authentication_error", "invalid api key", ""), execution.CodeAuthenticationFailed, false, 0},
		{"permission_403", sdkError(403, "permission_error", "permission denied", ""), execution.CodePermissionDenied, false, 0},
		{"timeout_408", sdkError(408, "timeout_error", "request timeout", ""), execution.CodeUnavailable, true, 0},
		{"rate_limited_429", sdkError(429, "rate_limit_error", "rate limited", "7"), execution.CodeRateLimited, true, 7 * time.Second},
		{"server_error_500", sdkError(500, "internal_server_error", "internal error", ""), execution.CodeUnavailable, true, 0},
		{"service_unavailable_503", sdkError(503, "service_unavailable_error", "service down", ""), execution.CodeUnavailable, true, 0},
		{"unexpected_418", sdkError(418, "teapot", "i am a teapot", ""), execution.CodeInternal, false, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized := normalizeProviderError(test.err)
			if execution.CodeOf(normalized) != test.wantCode || execution.Retryable(normalized) != test.retryable {
				t.Fatalf("code/retryable = %q/%v, want %q/%v", execution.CodeOf(normalized), execution.Retryable(normalized), test.wantCode, test.retryable)
			}
			if delay := execution.RetryAfter(normalized); delay != test.retry {
				t.Fatalf("retry after = %v, want %v", delay, test.retry)
			}
			// Verify no provider message leak
			for _, text := range []string{normalized.Error(), fmt.Sprintf("%+v", normalized)} {
				if containsText(text, "private") || containsText(text, "invalid api key") {
					t.Fatalf("normalized error leaked provider details: %q", text)
				}
			}
		})
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{"7", 7 * time.Second},
		{"60", 60 * time.Second},
		{"3600", time.Hour},
		{"0", 0},
		{"-1", 0},
		{"999999999999999999", 0},
		{"invalid", 0},
	}
	for _, test := range tests {
		if got := parseRetryAfter(test.value, time.Now()); got != test.want {
			t.Fatalf("parseRetryAfter(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, time.August, 30, 15, 0, 0, 0, time.UTC)
	if got := parseRetryAfter(now.Add(5*time.Second).Format(http.TimeFormat), now); got != 5*time.Second {
		t.Fatalf("HTTP-date retry = %v", got)
	}
	for _, when := range []time.Time{now, now.Add(-time.Second), now.Add(maxRetryAfter + time.Second)} {
		if got := parseRetryAfter(when.Format(http.TimeFormat), now); got != 0 {
			t.Fatalf("out-of-range HTTP-date retry = %v", got)
		}
	}
}

func TestNormalizeProviderErrorContextAndNetworkFailures(t *testing.T) {
	for _, contextErr := range []error{context.Canceled, context.DeadlineExceeded} {
		normalized := normalizeProviderError(contextErr)
		if !errors.Is(normalized, contextErr) || execution.CodeOf(normalized) != "" || execution.Retryable(normalized) {
			t.Fatalf("context normalization = %v (%q)", normalized, execution.CodeOf(normalized))
		}
	}
	networkErr := &net.DNSError{Err: "private resolver detail", Name: "private-host.invalid", IsTimeout: true}
	normalized := normalizeProviderError(networkErr)
	if execution.CodeOf(normalized) != execution.CodeUnavailable || !errors.Is(normalized, networkErr) {
		t.Fatalf("network normalization = %v (%q)", normalized, execution.CodeOf(normalized))
	}
	if containsText(normalized.Error(), "private-host") || containsText(normalized.Error(), "private resolver") {
		t.Fatalf("network error leaked details: %v", normalized)
	}
}

func TestNormalizeProviderErrorRateLimitWithoutResponse(t *testing.T) {
	providerError := sdkError(429, "rate_limit_error", "rate limited", "7").(*sdk.Error)
	providerError.Response = nil
	normalized := normalizeProviderError(providerError)
	if execution.CodeOf(normalized) != execution.CodeRateLimited || execution.RetryAfter(normalized) != 0 {
		t.Fatalf("code/retry = %q/%v", execution.CodeOf(normalized), execution.RetryAfter(normalized))
	}
}

func TestNormalizeProviderErrorBoundsRetryAfter(t *testing.T) {
	for _, value := range []string{"-1", "invalid", "999999999", "9223372036854775807"} {
		normalized := normalizeProviderError(sdkError(429, "rate_limit_error", "rate limited", value))
		if delay := execution.RetryAfter(normalized); delay != 0 {
			t.Fatalf("Retry-After %q produced %v", value, delay)
		}
	}
}

func TestNormalizeProviderErrorPreservesContextErrors(t *testing.T) {
	// Wrap a context error in another error
	wrapped := fmt.Errorf("wrapped context error: %w", context.Canceled)
	normalized := normalizeProviderError(wrapped)
	if !errors.Is(normalized, context.Canceled) {
		t.Fatalf("context error not preserved through errors.Is")
	}
}

func TestNormalizeProviderErrorNilInput(t *testing.T) {
	normalized := normalizeProviderError(nil)
	if normalized != nil {
		t.Fatalf("nil input should return nil, got %v", normalized)
	}
}

func TestClientFactoryValidation(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		shouldErr bool
	}{
		{"valid_url", "https://api.anthropic.com", false},
		{"valid_url_with_root_path", "https://api.anthropic.com/", false},
		{"valid_loopback_http", "http://127.0.0.1:8080", false},
		{"invalid_official_path", "https://api.anthropic.com/v1", true},
		{"invalid_other_https", "https://example.com", true},
		{"invalid_other_loopback", "http://127.0.0.2:8080", true},
		{"invalid_scheme", "ftp://api.anthropic.com", true},
		{"invalid_non_loopback_http", "http://example.com", true},
		{"invalid_localhost_hostname", "http://localhost:8080", true},
		{"invalid_no_host", "https:///path", true},
		{"invalid_with_query", "https://api.anthropic.com?key=value", true},
		{"invalid_with_fragment", "https://api.anthropic.com#fragment", true},
		{"invalid_with_userinfo", "https://user:pass@api.anthropic.com", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newClientFactory(test.baseURL, newSecureHTTPClient())
			if (err != nil) != test.shouldErr {
				t.Fatalf("newClientFactory(%q) error = %v, shouldErr = %v", test.baseURL, err, test.shouldErr)
			}
		})
	}
}

func TestSecureHTTPClientConfiguration(t *testing.T) {
	client := newSecureHTTPClient()
	if client == nil {
		t.Fatal("newSecureHTTPClient returned nil")
	}
	if client.Transport == nil {
		t.Fatal("HTTP client transport is nil")
	}
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect is nil")
	}
	// Test that CheckRedirect rejects all redirects
	err := client.CheckRedirect(&http.Request{}, []*http.Request{})
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect did not reject redirect: %v", err)
	}
}

func TestBoundedRoundTripperExactLimit(t *testing.T) {
	const limit = int64(8)
	mockTransport := &mockRoundTripper{responseSize: limit}
	tripper := &boundedRoundTripper{base: mockTransport, maxBytes: limit}

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	resp, err := tripper.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer resp.Body.Close()

	buffer := make([]byte, limit)
	n, err := resp.Body.Read(buffer)
	if int64(n) != limit {
		t.Fatalf("Read %d bytes, want %d", n, limit)
	}
}

func TestBoundedRoundTripperOverflow(t *testing.T) {
	const limit = int64(8)
	mockTransport := &mockRoundTripper{responseSize: limit + 1}
	tripper := &boundedRoundTripper{base: mockTransport, maxBytes: limit}

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	resp, err := tripper.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer resp.Body.Close()

	buffer := make([]byte, limit)
	n, err := resp.Body.Read(buffer)
	if int64(n) != limit {
		t.Fatalf("Read %d bytes, want %d", n, limit)
	}

	// Next read should detect overflow
	smallBuffer := make([]byte, 1)
	_, err = resp.Body.Read(smallBuffer)
	if err != errResponseTooLarge {
		t.Fatalf("Expected errResponseTooLarge, got %v", err)
	}
}

type mockRoundTripper struct {
	responseSize int64
	bytesRead    int64
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       &mockReadCloser{remaining: m.responseSize},
	}
	return resp, nil
}

type mockReadCloser struct {
	remaining int64
}

func (m *mockReadCloser) Read(b []byte) (int, error) {
	if m.remaining <= 0 {
		return 0, nil
	}
	if int64(len(b)) > m.remaining {
		n := m.remaining
		m.remaining = 0
		return int(n), nil
	}
	n := len(b)
	m.remaining -= int64(n)
	return n, nil
}

func (m *mockReadCloser) Close() error {
	return nil
}

func TestTLSMinimumVersion(t *testing.T) {
	client := newSecureHTTPClient()
	transport := client.Transport.(*boundedRoundTripper)
	httpTransport := transport.base.(*http.Transport)
	if httpTransport.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}
	if httpTransport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatalf("MinVersion = %v, want >= %v", httpTransport.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
}

func TestProxyNil(t *testing.T) {
	client := newSecureHTTPClient()
	transport := client.Transport.(*boundedRoundTripper)
	httpTransport := transport.base.(*http.Transport)
	if httpTransport.Proxy != nil {
		t.Fatal("Proxy should be nil")
	}
}

func TestTimeoutsBounded(t *testing.T) {
	client := newSecureHTTPClient()
	transport := client.Transport.(*boundedRoundTripper)
	httpTransport := transport.base.(*http.Transport)

	if httpTransport.TLSHandshakeTimeout != 10*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v", httpTransport.TLSHandshakeTimeout)
	}
	if httpTransport.ResponseHeaderTimeout != 60*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v", httpTransport.ResponseHeaderTimeout)
	}
	if httpTransport.ExpectContinueTimeout != time.Second {
		t.Fatalf("ExpectContinueTimeout = %v", httpTransport.ExpectContinueTimeout)
	}
}
