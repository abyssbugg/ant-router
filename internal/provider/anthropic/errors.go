package anthropic

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/abyssbugg/ant-router/internal/execution"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

const maxRetryAfter = 24 * time.Hour

func normalizeProviderError(err error) error {
	return normalizeProviderErrorAt(err, time.Now())
}

func normalizeProviderErrorAt(err error, now time.Time) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var apiError *sdk.Error
	if errors.As(err, &apiError) && apiError != nil {
		code := codeForAPIError(apiError)
		retryAfter := time.Duration(0)
		if code == execution.CodeRateLimited && apiError.Response != nil {
			retryAfter = parseRetryAfter(apiError.Response.Header.Get("Retry-After"), now)
		}
		return execution.NewError(code, "provider.anthropic", retryAfter, err)
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return execution.NewError(execution.CodeUnavailable, "provider.anthropic", 0, err)
	}
	return execution.NewError(execution.CodeInternal, "provider.anthropic", 0, err)
}

func codeForAPIError(err *sdk.Error) execution.ErrorCode {
	switch err.StatusCode {
	case http.StatusBadRequest, http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return execution.CodeInvalidRequest
	case http.StatusUnauthorized:
		return execution.CodeAuthenticationFailed
	case http.StatusForbidden:
		return execution.CodePermissionDenied
	case http.StatusNotFound:
		return execution.CodeInvalidModel
	case http.StatusRequestTimeout:
		return execution.CodeUnavailable
	case http.StatusTooManyRequests:
		return execution.CodeRateLimited
	default:
		if err.StatusCode >= 500 && err.StatusCode <= 599 {
			return execution.CodeUnavailable
		}
		return execution.CodeInternal
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 || seconds > int64(maxRetryAfter/time.Second) {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := when.Sub(now)
	if delay <= 0 || delay > maxRetryAfter {
		return 0
	}
	return delay
}

func containsText(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
