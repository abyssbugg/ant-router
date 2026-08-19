package configstore

import "testing"

func TestCodeOfTypedNilReturnsFalse(t *testing.T) {
	var typedNil *Error
	var err error = typedNil
	if code, ok := CodeOf(err); ok || code != "" {
		t.Fatalf("CodeOf(typed nil) = %q, %v, want empty, false", code, ok)
	}
}
