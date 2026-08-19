package configstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func TestRepairRevision(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data := []byte("canonical revision")
	id := revision.For(data)
	if err := store.PutRevision(context.Background(), id, data); err != nil {
		t.Fatalf("PutRevision() failed: %v", err)
	}
	path := filepath.Join(root, "revisions", id.String()+".toml")
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt) failed: %v", err)
	}
	if err := store.RepairRevision(context.Background(), id, data); err != nil {
		t.Fatalf("RepairRevision() failed: %v", err)
	}
	repaired, err := store.ReadRevision(context.Background(), id)
	if err != nil {
		t.Fatalf("ReadRevision() failed: %v", err)
	}
	if !bytes.Equal(repaired, data) {
		t.Errorf("repaired bytes = %q, want %q", repaired, data)
	}
}

func TestRepairRevisionRejectsHealthyMissingAndWrongContent(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data := []byte("canonical")
	id := revision.For(data)
	if err := store.PutRevision(context.Background(), id, data); err != nil {
		t.Fatalf("PutRevision() failed: %v", err)
	}
	if err := store.RepairRevision(context.Background(), id, data); CodeValue(err) != CodeRepairUnnecessary {
		t.Fatalf("healthy repair code = %q, want %q", CodeValue(err), CodeRepairUnnecessary)
	}
	missing := revision.For([]byte("missing"))
	if err := store.RepairRevision(context.Background(), missing, []byte("missing")); CodeValue(err) != CodeStoreUnavailable {
		t.Fatalf("missing repair code = %q, want %q", CodeValue(err), CodeStoreUnavailable)
	}
	if err := store.RepairRevision(context.Background(), id, []byte("wrong")); CodeValue(err) != CodeInvalidRevision {
		t.Fatalf("wrong data code = %q, want %q", CodeValue(err), CodeInvalidRevision)
	}
}

func TestRepairRevisionCancellationAndWriteFailure(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data := []byte("canonical")
	id := revision.For(data)
	if err := store.PutRevision(context.Background(), id, data); err != nil {
		t.Fatalf("PutRevision() failed: %v", err)
	}
	path := filepath.Join(root, "revisions", id.String()+".toml")
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt) failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.RepairRevision(ctx, id, data); CodeValue(err) != CodeCancelled {
		t.Fatalf("cancelled repair code = %q, want %q", CodeValue(err), CodeCancelled)
	}
	sentinel := errors.New("write failed")
	store.writeFile = func(context.Context, string, []byte, os.FileMode) error { return sentinel }
	if err := store.RepairRevision(context.Background(), id, data); !errors.Is(err, sentinel) {
		t.Fatalf("write failure = %v, want sentinel", err)
	}
}

func TestRepairRevisionTransientReadFailureDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data := []byte("canonical")
	id := revision.For(data)
	if err := store.PutRevision(context.Background(), id, data); err != nil {
		t.Fatalf("PutRevision() failed: %v", err)
	}
	path := filepath.Join(root, "revisions", id.String()+".toml")
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt) failed: %v", err)
	}
	sentinel := errors.New("transient read failure")
	store.readFile = func(string, int) ([]byte, error) { return nil, sentinel }
	writeCalled := false
	store.writeFile = func(context.Context, string, []byte, os.FileMode) error {
		writeCalled = true
		return nil
	}
	err = store.RepairRevision(context.Background(), id, data)
	if !errors.Is(err, sentinel) || CodeValue(err) != CodeStoreUnavailable {
		t.Fatalf("repair error = %v, code %q", err, CodeValue(err))
	}
	if writeCalled {
		t.Error("repair wrote after transient read failure")
	}
}

func TestRepairRevisionDurabilityUncertaintyIsPreserved(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data := []byte("canonical")
	id := revision.For(data)
	if err := store.PutRevision(context.Background(), id, data); err != nil {
		t.Fatalf("PutRevision() failed: %v", err)
	}
	path := filepath.Join(root, "revisions", id.String()+".toml")
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt) failed: %v", err)
	}
	uncertain := &Error{Code: CodeDurabilityUncertain, Err: errors.New("sync failed")}
	store.writeFile = func(context.Context, string, []byte, os.FileMode) error { return uncertain }
	if err := store.RepairRevision(context.Background(), id, data); !errors.Is(err, uncertain) {
		t.Fatalf("uncertain repair = %v, want original error", err)
	}
}

func CodeValue(err error) Code {
	code, _ := CodeOf(err)
	return code
}
