//go:build fault

package configstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicInjectedStages(t *testing.T) {
	stages := []atomicStage{
		atomicStageCreate,
		atomicStageChmod,
		atomicStageWrite,
		atomicStageFileSync,
		atomicStageClose,
		atomicStageReplace,
		atomicStageDirectorySync,
	}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "manifest.json")
			if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
				t.Fatalf("os.WriteFile old failed: %v", err)
			}
			sentinel := errors.New("injected failure")
			hooks := &atomicWriteHooks{failStage: func(got atomicStage) error {
				if got == stage {
					return sentinel
				}
				return nil
			}}
			err := writeAtomicWithHooks(context.Background(), target, []byte("new"), 0o600, hooks)
			expectedCode := CodeStoreUnavailable
			expectedContent := "old"
			if stage == atomicStageDirectorySync {
				expectedCode = CodeDurabilityUncertain
				expectedContent = "new"
			}
			if code, ok := CodeOf(err); !ok || code != expectedCode {
				t.Fatalf("stage %s code = %q, present = %v, want %q; error: %v", stage, code, ok, expectedCode, err)
			}
			content, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatalf("os.ReadFile target failed: %v", readErr)
			}
			if string(content) != expectedContent {
				t.Errorf("stage %s content = %q, want %q", stage, content, expectedContent)
			}
			matches, globErr := filepath.Glob(filepath.Join(dir, ".manifest.json.tmp-*"))
			if globErr != nil {
				t.Fatalf("filepath.Glob failed: %v", globErr)
			}
			if len(matches) != 0 {
				t.Errorf("stage %s left temporary files: %v", stage, matches)
			}
		})
	}
}
