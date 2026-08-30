package openai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/execution"
	sdk "github.com/openai/openai-go/v3"
)

func sdkError(status int, code, parameter, retryAfter string) error {
	response := &http.Response{StatusCode: status, Header: make(http.Header)}
	if retryAfter != "" {
		response.Header.Set("Retry-After", retryAfter)
	}
	return &sdk.Error{
		StatusCode: status,
		Code:       code,
		Param:      parameter,
		Message:    "private upstream provider message",
		Response:   response,
	}
}

func TestNormalizeProviderErrorCodes(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  execution.ErrorCode
		retryable bool
		retry     time.Duration
	}{
		{"model", sdkError(400, "model_not_found", "model", ""), execution.CodeInvalidModel, false, 0},
		{"request", sdkError(400, "invalid_request_error", "input", ""), execution.CodeInvalidRequest, false, 0},
		{"authentication", sdkError(401, "invalid_api_key", "", ""), execution.CodeAuthenticationFailed, false, 0},
		{"permission", sdkError(403, "permission_denied", "", ""), execution.CodePermissionDenied, false, 0},
		{"timeout", sdkError(408, "request_timeout", "", ""), execution.CodeUnavailable, true, 0},
		{"conflict", sdkError(409, "conflict", "", ""), execution.CodeUnavailable, true, 0},
		{"rate limited", sdkError(429, "rate_limit_exceeded", "", "7"), execution.CodeRateLimited, true, 7 * time.Second},
		{"unavailable", sdkError(503, "server_error", "", ""), execution.CodeUnavailable, true, 0},
		{"unexpected status", sdkError(418, "teapot", "", ""), execution.CodeInternal, false, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized := normalizeProviderError(test.err)
			if execution.CodeOf(normalized) != test.wantCode || execution.Retryable(normalized) != test.retryable {
				t.Fatalf("code/retryable = %q/%v", execution.CodeOf(normalized), execution.Retryable(normalized))
			}
			if delay := execution.RetryAfter(normalized); delay != test.retry {
				t.Fatalf("retry after = %v, want %v", delay, test.retry)
			}
			for _, text := range []string{normalized.Error(), fmt.Sprintf("%+v", normalized)} {
				if containsText(text, "private upstream provider message") || containsText(text, "invalid_api_key") {
					t.Fatalf("normalized error leaked provider details: %q", text)
				}
			}
		})
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
		if !errors.Is(normalized, contextErr) || execution.CodeOf(normalized) != execution.CodeUnavailable {
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
	providerError := sdkError(429, "rate_limit_exceeded", "", "7").(*sdk.Error)
	providerError.Response = nil
	normalized := normalizeProviderError(providerError)
	if execution.CodeOf(normalized) != execution.CodeRateLimited || execution.RetryAfter(normalized) != 0 {
		t.Fatalf("code/retry = %q/%v", execution.CodeOf(normalized), execution.RetryAfter(normalized))
	}
}

func TestNormalizeProviderErrorBoundsRetryAfter(t *testing.T) {
	for _, value := range []string{"-1", "invalid", "999999999", "9223372036854775807"} {
		normalized := normalizeProviderError(sdkError(429, "rate_limit_exceeded", "", value))
		if delay := execution.RetryAfter(normalized); delay != 0 {
			t.Fatalf("Retry-After %q produced %v", value, delay)
		}
	}
}

func containsText(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
