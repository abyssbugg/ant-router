package filelock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("ANT_ROUTER_LOCK_HELPER") != "1" {
		return
	}
	path, ready, release := os.Getenv("ANT_ROUTER_LOCK_PATH"), os.Getenv("ANT_ROUTER_LOCK_READY"), os.Getenv("ANT_ROUTER_LOCK_RELEASE")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := Acquire(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(release); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAcquireContendsAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lock")
	ready := filepath.Join(root, "ready")
	release := filepath.Join(root, "release")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestLockHelperProcess$")
	command.Env = append(os.Environ(), "ANT_ROUTER_LOCK_HELPER=1", "ANT_ROUTER_LOCK_PATH="+path, "ANT_ROUTER_LOCK_READY="+ready, "ANT_ROUTER_LOCK_RELEASE="+release)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err = Acquire(ctx, path)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contender error = %v", err)
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseIsConcurrentAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	lock, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errorsSeen := make(chan error, 16)
	for range 16 {
		group.Add(1)
		go func() { defer group.Done(); errorsSeen <- lock.Release() }()
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAcquireRejectsInvalidAndSymlinkPaths(t *testing.T) {
	if _, err := Acquire(nil, "/lock"); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := Acquire(context.Background(), "relative"); err == nil {
		t.Fatal("relative path accepted")
	}
	root := t.TempDir()
	unclean := root + string(filepath.Separator) + "sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + "lock"
	if _, err := Acquire(context.Background(), unclean); err == nil {
		t.Fatal("unclean path accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(context.Background(), link); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestAcquireCreatesPrivateLockFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are not Windows ACLs")
	}
	path := filepath.Join(t.TempDir(), "lock")
	lock, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
