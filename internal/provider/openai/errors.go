package openai

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abyssbugg/ant-router/internal/execution"
	sdk "github.com/openai/openai-go/v3"
)

const maxRetryAfter = 24 * time.Hour

func normalizeProviderError(err error) error {
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
			retryAfter = parseRetryAfter(apiError.Response.Header.Get("Retry-After"), time.Now())
		}
		return execution.NewError(code, "provider.openai", retryAfter, err)
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return execution.NewError(execution.CodeUnavailable, "provider.openai", 0, err)
	}
	return execution.NewError(execution.CodeInternal, "provider.openai", 0, err)
}

func codeForAPIError(err *sdk.Error) execution.ErrorCode {
	switch err.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		if strings.EqualFold(err.Param, "model") || strings.Contains(strings.ToLower(err.Code), "model_not_found") {
			return execution.CodeInvalidModel
		}
		return execution.CodeInvalidRequest
	case http.StatusUnauthorized:
		return execution.CodeAuthenticationFailed
	case http.StatusForbidden:
		return execution.CodePermissionDenied
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly:
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
