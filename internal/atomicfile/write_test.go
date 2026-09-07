package atomicfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesAndReplacesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	result, err := Write(context.Background(), path, []byte("first"), 0o600)
	if err != nil || !result.Committed {
		t.Fatalf("first write = %#v, %v", result, err)
	}
	result, err = Write(context.Background(), path, []byte("second"), 0o600)
	if err != nil || !result.Committed {
		t.Fatalf("replacement = %#v, %v", result, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second" {
		t.Fatalf("content = %q, %v", got, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info, err)
	}
}

func TestWriteCancellationDoesNotCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Write(ctx, path, []byte("secret"), 0o600)
	if err == nil || result.Committed {
		t.Fatalf("write = %#v, %v", result, err)
	}
	if !os.IsNotExist(statError(path)) {
		t.Fatalf("target exists after cancelled write")
	}
}

func statError(path string) error {
	_, err := os.Lstat(path)
	return err
}
