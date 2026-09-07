package atomicfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoveDeletesRegularFileAndMissingIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Remove(context.Background(), path)
	if err != nil || !result.Committed {
		t.Fatalf("remove = %#v, %v", result, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("file remains")
	}
	result, err = Remove(context.Background(), path)
	if err != nil || result.Committed {
		t.Fatalf("missing remove = %#v, %v", result, err)
	}
}

func TestRemoveRejectsInvalidTargets(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unclean := root + string(filepath.Separator) + "sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + "regular"
	cases := []string{"relative", unclean, root}
	if _, err := os.Lstat(regular); err != nil {
		t.Fatal(err)
	}
	if link := filepath.Join(root, "link"); makeSymlink(t, regular, link) {
		cases = append(cases, link)
	}
	for _, path := range cases {
		result, err := Remove(context.Background(), path)
		if err == nil || result.Committed {
			t.Errorf("Remove(%q) = %#v, %v", path, result, err)
		}
	}
	if _, err := os.Stat(regular); err != nil {
		t.Fatal("invalid removal changed regular target")
	}
}

func TestRemoveFailureBeforeUnlinkPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected")
	result, err := RemoveWithHooks(context.Background(), path, &Hooks{FailStage: func(stage Stage) error {
		if stage == StageReplace {
			return injected
		}
		return nil
	}})
	if !errors.Is(err, injected) || result.Committed {
		t.Fatalf("remove = %#v, %v", result, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("pre-unlink failure removed file")
	}
}

func TestRemoveReportsCommittedWhenDirectorySyncFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("sync failed")
	result, err := RemoveWithHooks(context.Background(), path, &Hooks{SyncDirectory: func(string) error { return injected }})
	if !result.Committed || !errors.Is(err, injected) {
		t.Fatalf("remove = %#v, %v", result, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("committed removal left file")
	}
}

func TestRemoveHonorsCancellationBeforeUnlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, err := RemoveWithHooks(ctx, path, &Hooks{BeforeReplace: cancel})
	if !errors.Is(err, context.Canceled) || result.Committed {
		t.Fatalf("remove = %#v, %v", result, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("canceled removal changed file")
	}
}

func makeSymlink(t *testing.T, target, link string) bool {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			return false
		}
		t.Fatal(err)
	}
	return true
}
