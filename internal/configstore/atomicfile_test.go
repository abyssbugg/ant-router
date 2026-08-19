package configstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestWriteAtomicNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "newfile.txt")
	data := []byte("hello world")

	err := writeAtomic(context.Background(), filePath, data, 0644)
	if err != nil {
		t.Fatalf("writeAtomic failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(content) != string(data) {
		t.Errorf("content mismatch: got %q, want %q", string(content), string(data))
	}

	// Verify no hidden temp files were left
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file in tmpDir, got %d", len(entries))
	}
}

func TestWriteAtomicReplaceFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "existing.txt")
	oldData := []byte("old content")
	newData := []byte("new content")

	// Create the original file
	if err := os.WriteFile(filePath, oldData, 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	// Replace it
	err := writeAtomic(context.Background(), filePath, newData, 0644)
	if err != nil {
		t.Fatalf("writeAtomic failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(content) != string(newData) {
		t.Errorf("content mismatch: got %q, want %q", string(content), string(newData))
	}

	// Verify no hidden temp files were left
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file in tmpDir, got %d", len(entries))
	}
}

func TestWriteAtomicEmptyData(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.txt")
	data := []byte{}

	err := writeAtomic(context.Background(), filePath, data, 0644)
	if err != nil {
		t.Fatalf("writeAtomic failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if len(content) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(content))
	}
}

func TestWriteAtomicSequentialWrites(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sequential.txt")

	values := []string{"first", "second", "third"}
	for i, val := range values {
		data := []byte(val)
		err := writeAtomic(context.Background(), filePath, data, 0644)
		if err != nil {
			t.Fatalf("writeAtomic iteration %d failed: %v", i, err)
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read file after iteration %d: %v", i, err)
		}

		if string(content) != val {
			t.Errorf("iteration %d: content mismatch: got %q, want %q", i, string(content), val)
		}
	}

	// Verify only the final file exists
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file in tmpDir, got %d", len(entries))
	}
}

func TestWriteAtomicCancelledContext(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancelled.txt")
	data := []byte("should not exist")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := writeAtomic(ctx, filePath, data, 0644)
	if err == nil {
		t.Fatalf("writeAtomic should have returned error for cancelled context")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got code=%q ok=%v", code, ok)
	}

	// Verify file was not created
	_, err = os.Stat(filePath)
	if !os.IsNotExist(err) {
		t.Errorf("file should not exist after cancelled context")
	}

	// Verify no temp files
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 files in tmpDir, got %d", len(entries))
	}
}

func TestWriteAtomicBlankPath(t *testing.T) {
	err := writeAtomic(context.Background(), "", []byte("data"), 0644)
	if err == nil {
		t.Fatalf("writeAtomic should return error for blank path")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got code=%q ok=%v", code, ok)
	}
}

func TestWriteAtomicMissingParent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent", "subdir", "file.txt")

	err := writeAtomic(context.Background(), filePath, []byte("data"), 0644)
	if err == nil {
		t.Fatalf("writeAtomic should return error for missing parent directory")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got code=%q ok=%v", code, ok)
	}
}

func TestWriteAtomicTargetIsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dirPath := filepath.Join(tmpDir, "targetdir")
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	err := writeAtomic(context.Background(), dirPath, []byte("data"), 0644)
	if err == nil {
		t.Fatalf("writeAtomic should return error when target is a directory")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got code=%q ok=%v", code, ok)
	}
}

func TestWriteAtomicBeforeReplaceHookCancels(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "hooktest.txt")
	oldData := []byte("original")
	newData := []byte("replacement")

	// Create original file
	if err := os.WriteFile(filePath, oldData, 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hooks := &atomicWriteHooks{beforeReplace: cancel}

	err := writeAtomicWithHooks(ctx, filePath, newData, 0644, hooks)
	if err == nil {
		t.Fatalf("writeAtomicWithHooks should return error when beforeReplace cancels context")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got code=%q ok=%v", code, ok)
	}

	// Verify original content remains
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(content) != string(oldData) {
		t.Errorf("original content should remain: got %q, want %q", string(content), string(oldData))
	}

	// Verify no hidden temp files
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file in tmpDir (no temp files), got %d", len(entries))
	}
}

func TestWriteAtomicAfterReplaceHookCancels(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "hooktest2.txt")
	oldData := []byte("original")
	newData := []byte("replacement")

	// Create original file
	if err := os.WriteFile(filePath, oldData, 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	hooks := &atomicWriteHooks{
		afterReplace: func() {
			cancel()
		},
	}

	err := writeAtomicWithHooks(ctx, filePath, newData, 0644, hooks)
	// afterReplace hook cancels after the file is already replaced, so operation should succeed
	if err != nil {
		t.Fatalf("writeAtomicWithHooks should succeed even if afterReplace cancels: %v", err)
	}

	// Verify new content is in place (durability continues after commit)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(content) != string(newData) {
		t.Errorf("new content should be in place: got %q, want %q", string(content), string(newData))
	}
}

func TestWriteAtomicNoTempFilesOnSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "success.txt")
	data := []byte("successful write")

	err := writeAtomic(context.Background(), filePath, data, 0644)
	if err != nil {
		t.Fatalf("writeAtomic failed: %v", err)
	}

	// List all files in the directory
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 file, got %d", len(entries))
		for _, e := range entries {
			t.Logf("  file: %s", e.Name())
		}
	}

	// Check that no file starts with a dot (temp file convention)
	for _, e := range entries {
		if e.Name()[0] == '.' {
			t.Errorf("found hidden temp file: %s", e.Name())
		}
	}
}

func TestWriteAtomicErrorsNoDataPayload(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with missing parent
	err := writeAtomic(context.Background(), filepath.Join(tmpDir, "nonexistent", "file.txt"), []byte("secret data"), 0644)
	if err == nil {
		t.Fatalf("expected error")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "secret data") {
		t.Errorf("error message should not contain data payload: %s", errMsg)
	}
}

func TestWriteAtomicBoundedConcurrentReaders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows may deny replacement while readers hold the destination open")
	}
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "concurrent.txt")

	// Create initial file
	payloadA := make([]byte, 8192)
	payloadB := make([]byte, 8192)
	for i := range payloadA {
		payloadA[i] = 'A'
	}
	for i := range payloadB {
		payloadB[i] = 'B'
	}

	if err := os.WriteFile(filePath, payloadA, 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	readerStarted := make(chan struct{})
	stopCh := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	var readsCompleted int

	// Helper to report errors non-blockingly
	reportError := func(err error) {
		select {
		case errCh <- err:
		default:
			// Buffer full, error already reported
		}
	}

	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(readerStarted)
		for {
			select {
			case <-stopCh:
				return
			default:
			}

			content, err := os.ReadFile(filePath)
			if err != nil {
				reportError(fmt.Errorf("read error: %w", err))
				return
			}

			// Verify we got either complete A or complete B
			if len(content) != len(payloadA) {
				reportError(fmt.Errorf("read partial data: got %d bytes, want %d", len(content), len(payloadA)))
				return
			}

			isA := true
			isB := true
			for i := range content {
				if content[i] != 'A' {
					isA = false
					break
				}
			}
			for i := range content {
				if content[i] != 'B' {
					isB = false
					break
				}
			}

			if !isA && !isB {
				reportError(fmt.Errorf("read inconsistent data: not all A or all B"))
				return
			}

			readsCompleted++
		}
	}()

	// Wait for reader to start before launching writer
	<-readerStarted

	// Writer goroutine - alternate between A and B payloads
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stopCh)
		toggle := false
		for i := 0; i < 10; i++ {
			var payload []byte
			if toggle {
				payload = payloadB
			} else {
				payload = payloadA
			}
			toggle = !toggle

			if err := writeAtomic(context.Background(), filePath, payload, 0644); err != nil {
				reportError(fmt.Errorf("write error: %w", err))
				return
			}
		}
	}()

	wg.Wait()

	select {
	case err := <-errCh:
		t.Fatalf("concurrent test failed: %v", err)
	default:
	}

	if readsCompleted == 0 {
		t.Errorf("no reads completed, reader goroutine may not have run")
	}
}

func TestWriteAtomicSyncDirectoryFailure(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "syncfail.txt")
	newData := []byte("new content for sync test")

	// Create initial file
	if err := os.WriteFile(filePath, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	hooks := &atomicWriteHooks{
		syncDirectory: func(dirPath string) error {
			return fmt.Errorf("simulated sync failure")
		},
	}

	err := writeAtomicWithHooks(context.Background(), filePath, newData, 0644, hooks)
	if err == nil {
		t.Fatalf("writeAtomicWithHooks should return error when syncDirectory fails")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeDurabilityUncertain {
		t.Errorf("expected CodeDurabilityUncertain, got code=%q ok=%v", code, ok)
	}

	// Verify target contains new data (replacement succeeded)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != string(newData) {
		t.Errorf("target should contain new data: got %q, want %q", string(content), string(newData))
	}

	// Verify no temp files remain
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read tmpDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("found leftover temp file: %s", e.Name())
		}
	}
}
