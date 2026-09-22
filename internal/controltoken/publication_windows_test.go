//go:build windows

package controltoken

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/abyssbugg/ant-router/internal/controltransport"
	"golang.org/x/sys/windows"
)

func TestWindowsReadPublishedTokenWithDeleteHandle(t *testing.T) {
	for _, operation := range []string{"Load", "LoadOrCreate"} {
		t.Run(operation, func(t *testing.T) {
			store := newTestStore(t)
			paths, err := store.ensureInstance(controltransport.DefaultInstance)
			if err != nil {
				t.Fatalf("prepare instance: %v", err)
			}
			original, err := Generate(bytes.NewReader(bytes.Repeat([]byte{0x61}, TokenSize)))
			if err != nil {
				t.Fatalf("generate fixture token: %v", err)
			}
			before, release := publishWindowsTokenWithDeleteHandle(t, paths.TokenPath, original)
			store.random = windowsTokenReadFunc(func([]byte) (int, error) {
				t.Fatal("reading a published token requested entropy")
				return 0, errors.New("unexpected token generation")
			})
			var loaded Token
			if operation == "Load" {
				loaded, err = store.Load(controltransport.DefaultInstance)
			} else {
				loaded, err = store.LoadOrCreate(controltransport.DefaultInstance)
			}
			if err != nil || loaded != original {
				t.Fatalf("published-token read failed (code=%q, error=%t, same_token=%t)", CodeOfValue(err), err != nil, loaded == original)
			}
			release()
			assertWindowsTokenUnchanged(t, store, paths.TokenPath, before, original)
		})
	}
}

func TestWindowsCreationCollisionWithDeleteHandle(t *testing.T) {
	loser := newTestStore(t)
	paths, err := loser.ensureInstance(controltransport.DefaultInstance)
	if err != nil {
		t.Fatalf("prepare instance: %v", err)
	}
	winner, err := Generate(bytes.NewReader(bytes.Repeat([]byte{0x62}, TokenSize)))
	if err != nil {
		t.Fatalf("generate fixture token: %v", err)
	}
	var before os.FileInfo
	var release func()
	entropyCalls := 0
	loser.random = windowsTokenReadFunc(func(p []byte) (int, error) {
		entropyCalls++
		if entropyCalls != 1 || len(p) != TokenSize {
			t.Fatal("unexpected losing-creator entropy read")
		}
		// The first lookup has found no token. Publish the winner before the
		// loser installs, retaining the publisher's delete-access handle.
		before, release = publishWindowsTokenWithDeleteHandle(t, paths.TokenPath, winner)
		return copy(p, bytes.Repeat([]byte{0x63}, TokenSize)), nil
	})
	loaded, err := loser.LoadOrCreate(controltransport.DefaultInstance)
	if entropyCalls != 1 {
		t.Fatal("losing creator did not reach publication barrier")
	}
	if err != nil || loaded != winner {
		t.Fatalf("winner read failed (code=%q, error=%t, same_token=%t)", CodeOfValue(err), err != nil, loaded == winner)
	}
	release()
	assertWindowsTokenUnchanged(t, loser, paths.TokenPath, before, winner)
}

// Model the documented interval between a rename becoming visible and its
// DELETE-access handle closing. Unlike a read-denying handle, this handle
// allows data reads; it requires readers to permit FILE_SHARE_DELETE.
func publishWindowsTokenWithDeleteHandle(t *testing.T, path string, token Token) (os.FileInfo, func()) {
	t.Helper()
	temporary, err := os.CreateTemp(filepath.Dir(path), ".publication-*")
	if err != nil {
		t.Fatalf("create publication fixture: %v", err)
	}
	temporaryPath := temporary.Name()
	t.Cleanup(func() {
		if err := os.Remove(temporaryPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove publication fixture: %v", err)
		}
	})
	_, writeErr := io.WriteString(temporary, token.String())
	// File.Stat captures identity from the handle. Path-based Stat may defer
	// that lookup until SameFile, when the old name no longer exists.
	before, statErr := temporary.Stat()
	closeErr := temporary.Close()
	if writeErr != nil || statErr != nil || closeErr != nil {
		t.Fatalf("write fixture: %v; stat: %v; close: %v", writeErr, statErr, closeErr)
	}
	name, err := windows.UTF16PtrFromString(temporaryPath)
	if err != nil {
		t.Fatalf("encode publication path: %v", err)
	}
	handle, err := windows.CreateFile(name, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("hold publication handle: %v", err)
	}
	release := func() {
		t.Helper()
		if handle != windows.InvalidHandle {
			err := windows.CloseHandle(handle)
			handle = windows.InvalidHandle
			if err != nil {
				t.Errorf("close publication handle: %v", err)
			}
		}
	}
	t.Cleanup(release)
	if err := installTokenFile(temporaryPath, path); err != nil {
		t.Fatalf("publish fixture with held handle: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("publication did not expose a regular file: %v", err)
	}
	if err := applyPrivateWindowsACL(path); err != nil {
		t.Fatalf("held publication handle blocked ACL preflight: %v", err)
	}
	// Negative control: ordinary os.Open lacks delete sharing. This proves
	// the fixture distinguishes the old reader from a publication-safe one.
	file, err := os.Open(path)
	if err == nil {
		_ = file.Close()
		t.Fatal("ordinary reader unexpectedly allowed the held delete handle")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("ordinary reader did not expose the expected sharing conflict: %v", err)
	}
	return before, release
}
