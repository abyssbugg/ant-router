package execution

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestExecutionErrorCodesAndRetryRules(t *testing.T) {
	cause := errors.New("safe cause")
	for _, code := range []ErrorCode{CodeInvalidModel, CodeUnavailable, CodeRateLimited, CodeInternal} {
		retry := time.Duration(0)
		if code == CodeRateLimited {
			retry = 2 * time.Second
		}
		err := NewError(code, "executor", retry, cause)
		if CodeOf(err) != code || !errors.Is(err, cause) {
			t.Fatalf("error %q did not preserve code/cause", code)
		}
		if RetryAfter(err) != retry {
			t.Fatalf("RetryAfter(%q) = %v, want %v", code, RetryAfter(err), retry)
		}
	}
}

func TestExecutionErrorRejectsInvalidRetryMetadata(t *testing.T) {
	if err := NewError(CodeUnavailable, "executor", time.Second, nil); CodeOf(err) != CodeInvalidError {
		t.Fatalf("non-rate-limit retry code = %q", CodeOf(err))
	}
	if err := NewError(CodeRateLimited, "executor", -time.Second, nil); CodeOf(err) != CodeInvalidError {
		t.Fatalf("negative retry code = %q", CodeOf(err))
	}
}

func TestExecutionErrorIsNilSafeAndDoesNotRequireSensitiveValues(t *testing.T) {
	var typedNil *Error
	if CodeOf(typedNil) != "" || RetryAfter(typedNil) != 0 || typedNil.Error() != "" || typedNil.Unwrap() != nil {
		t.Fatal("typed nil error helpers are not safe")
	}
	secret := "never-echo-provider-secret"
	err := NewError(CodeInternal, "executor", 0, errors.New("safe cause"))
	if strings.Contains(fmt.Sprint(err), secret) {
		t.Fatal("execution error exposed unrelated secret")
	}
}
