package canonical

import (
	"time"
	"unicode/utf8"
)

const MaxRetryAfter = 24 * time.Hour

// FailureParams contains normalized failure fields for NewFailure.
type FailureParams struct {
	Code       FailureCode
	Message    string
	Retryable  bool
	RetryAfter *time.Duration
}

// Failure is an immutable normalized safe failure.
type Failure struct {
	code          FailureCode
	message       string
	retryable     bool
	retryAfter    time.Duration
	hasRetryAfter bool
}

// NewFailure validates failure code, safe message, retryability, and delay.
func NewFailure(params FailureParams) (Failure, error) {
	failure := Failure{code: params.Code, message: params.Message, retryable: params.Retryable}
	if params.RetryAfter != nil {
		failure.retryAfter, failure.hasRetryAfter = *params.RetryAfter, true
	}
	if !failure.Valid() {
		return Failure{}, responseError(CodeInvalidInvariant, "failure")
	}
	return failure, nil
}

func (f Failure) Code() FailureCode                 { return f.code }
func (f Failure) Message() string                   { return f.message }
func (f Failure) Retryable() bool                   { return f.retryable }
func (f Failure) RetryAfter() (time.Duration, bool) { return f.retryAfter, f.hasRetryAfter }
func (f Failure) Valid() bool {
	if !f.code.Valid() || f.message == "" || !utf8.ValidString(f.message) || len(f.message) > MaxFailureMessageBytes {
		return false
	}
	if f.retryable && !retryableFailureCode(f.code) {
		return false
	}
	if f.hasRetryAfter && (!f.retryable || f.retryAfter < 0 || f.retryAfter > MaxRetryAfter) {
		return false
	}
	return true
}

func retryableFailureCode(code FailureCode) bool {
	return oneOf(string(code), "model_unavailable", "rate_limited", "upstream_timeout", "upstream_connection_failed", "stream_interrupted", "router_overloaded", "internal_error")
}
