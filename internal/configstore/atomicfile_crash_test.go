//go:build fault

package configstore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const atomicCrashModeEnvironment = "ANT_ROUTER_ATOMIC_CRASH_MODE"
const atomicCrashTargetEnvironment = "ANT_ROUTER_ATOMIC_CRASH_TARGET"

func TestAtomicWriteCrashHelper(t *testing.T) {
	mode := os.Getenv(atomicCrashModeEnvironment)
	if mode == "" {
		t.Skip("subprocess helper")
	}
	target := os.Getenv(atomicCrashTargetEnvironment)
	hooks := &atomicWriteHooks{}
	switch mode {
	case "before":
		hooks.beforeReplace = func() { os.Exit(71) }
	case "after":
		hooks.afterReplace = func() { os.Exit(72) }
	default:
		os.Exit(73)
	}
	_ = writeAtomicWithHooks(context.Background(), target, []byte("new"), 0o600, hooks)
	os.Exit(74)
}

func TestAtomicWriteCrashRecovery(t *testing.T) {
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "manifest.json")
			if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
				t.Fatalf("os.WriteFile old failed: %v", err)
			}
			wantExitCode := 71
			if mode == "after" {
				wantExitCode = 72
			}
			command := exec.Command(os.Args[0], "-test.run=^TestAtomicWriteCrashHelper$")
			command.Env = append(os.Environ(),
				atomicCrashModeEnvironment+"="+mode,
				atomicCrashTargetEnvironment+"="+target,
			)
			runErr := command.Run()
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				t.Fatalf("crash helper did not exit with a status: %v", runErr)
			}
			if exitErr.ExitCode() != wantExitCode {
				t.Fatalf("crash helper exit code = %d, want %d", exitErr.ExitCode(), wantExitCode)
			}

			content, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("os.ReadFile target failed: %v", err)
			}
			expected := "old"
			if mode == "after" {
				expected = "new"
			}
			if string(content) != expected {
				t.Errorf("mode %s content = %q, want %q", mode, content, expected)
			}

			store, err := NewFileStore(root)
			if err != nil {
				t.Fatalf("NewFileStore failed: %v", err)
			}
			if err := store.CleanupTemps(context.Background()); err != nil {
				t.Fatalf("CleanupTemps failed: %v", err)
			}
			matches, err := filepath.Glob(filepath.Join(root, ".manifest.json.tmp-*"))
			if err != nil {
				t.Fatalf("filepath.Glob failed: %v", err)
			}
			if len(matches) != 0 {
				t.Errorf("mode %s left temporary files: %v", mode, matches)
			}
		})
	}
}
