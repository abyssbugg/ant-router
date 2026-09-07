package configstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestWriteAtomicNilContextIsCancelled(t *testing.T) {
	err := writeAtomic(nil, filepath.Join(t.TempDir(), "state.json"), []byte("secret"), 0o600)
	if err == nil {
		t.Fatal("expected nil context error")
	}
	var storeError *Error
	if !errors.As(err, &storeError) || storeError.Code != CodeCancelled {
		t.Fatalf("error = %v, want %s", err, CodeCancelled)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not preserve context cancellation: %v", err)
	}
}
