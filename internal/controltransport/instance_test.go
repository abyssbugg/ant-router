package controltransport

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInstanceName(t *testing.T) {
	valid := []string{"default", "dev", "test-1", "staging", "a", "a" + strings.Repeat("1", 31)}
	for _, input := range valid {
		t.Run("valid-"+input, func(t *testing.T) {
			name, err := ParseInstanceName(input)
			if err != nil {
				t.Fatalf("ParseInstanceName(%q) failed: %v", input, err)
			}
			if name.String() != input {
				t.Errorf("String() = %q, want %q", name.String(), input)
			}
		})
	}

	invalid := []string{"", "A", "1dev", "-dev", "dev_1", "dev.test", "dev/test", " dev", "dev ", "dev\n", "défaut", "a" + strings.Repeat("1", 32)}
	for index, input := range invalid {
		t.Run(fmt.Sprintf("invalid-%02d", index), func(t *testing.T) {
			_, err := ParseInstanceName(input)
			if code, ok := CodeOf(err); !ok || code != CodeInvalidInstance {
				t.Fatalf("code = %q, present = %v, want %q", code, ok, CodeInvalidInstance)
			}
			if strings.Contains(err.Error(), input) && input != "" {
				t.Errorf("error echoed invalid instance %q", input)
			}
		})
	}
}

func TestDefaultInstance(t *testing.T) {
	if DefaultInstance.String() != "default" {
		t.Errorf("DefaultInstance = %q, want default", DefaultInstance)
	}
}

func TestResolveInstancePaths(t *testing.T) {
	root := filepath.Join("state", "root")
	name, _ := ParseInstanceName("test-1")
	paths, err := ResolveInstancePaths(root, name)
	if err != nil {
		t.Fatalf("ResolveInstancePaths() failed: %v", err)
	}
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs() failed: %v", err)
	}
	wantState := filepath.Join(filepath.Clean(rootAbsolute), "instances", "test-1")
	if paths.Instance != name || paths.StateDir != wantState || paths.TokenPath != filepath.Join(wantState, "control.token") {
		t.Errorf("paths = %#v, want state %q and token %q", paths, wantState, filepath.Join(wantState, "control.token"))
	}
}

func TestResolveInstancePathsIsolation(t *testing.T) {
	root := t.TempDir()
	first, _ := ParseInstanceName("dev")
	second, _ := ParseInstanceName("test-1")
	firstPaths, _ := ResolveInstancePaths(root, first)
	secondPaths, _ := ResolveInstancePaths(root, second)
	if firstPaths.StateDir == secondPaths.StateDir || firstPaths.TokenPath == secondPaths.TokenPath {
		t.Fatal("different instances resolved to the same state")
	}
}

func TestResolveInstancePathsRejectsInvalidInputs(t *testing.T) {
	_, err := ResolveInstancePaths("", DefaultInstance)
	if code, ok := CodeOf(err); !ok || code != CodePathResolutionFailed {
		t.Fatalf("blank root code = %q, present = %v", code, ok)
	}
	_, err = ResolveInstancePaths(t.TempDir(), InstanceName("../escape"))
	if code, ok := CodeOf(err); !ok || code != CodeInvalidInstance {
		t.Fatalf("invalid instance code = %q, present = %v", code, ok)
	}
}
