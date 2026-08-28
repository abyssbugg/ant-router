package responses

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestResponseAdapterErrorsAreTypedWrappedAndNilSafe(t *testing.T) {
	cause := errors.New("safe cause")
	err := newError(CodeInvalidField, "input[0]", cause)
	if CodeOf(err) != CodeInvalidField || PathOf(err) != "input[0]" || !errors.Is(err, cause) {
		t.Fatalf("error helpers lost metadata: %v", err)
	}
	var typedNil *Error
	if typedNil.Error() != "" || typedNil.Unwrap() != nil || CodeOf(typedNil) != "" || PathOf(typedNil) != "" {
		t.Fatal("typed nil error helpers are unsafe")
	}
	secret := "request-secret"
	if strings.Contains(fmt.Sprint(err), secret) {
		t.Fatal("error exposed unrelated secret")
	}
}
