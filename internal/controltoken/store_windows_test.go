//go:build windows

package controltoken

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/controltransport"
	"golang.org/x/sys/windows"
)

// These controlled sharing conflicts characterize failure safety, not the
// unidentified handle owner behind the intermittent Windows CI failure.
func TestWindowsSharedTokenReadFailurePreservesToken(t *testing.T) {
	for _, operation := range []string{"Load", "LoadOrCreate"} {
		t.Run(operation, func(t *testing.T) {
			store := newTestStore(t)
			store.random = bytes.NewReader(bytes.Repeat([]byte{0x41}, TokenSize))
			original, err := store.LoadOrCreate(controltransport.DefaultInstance)
			if err != nil {
				t.Fatalf("create initial token: %v", err)
			}
			paths, err := controltransport.ResolveInstancePaths(store.stateRoot, controltransport.DefaultInstance)
			if err != nil {
				t.Fatalf("resolve token path: %v", err)
			}
			before, err := os.Stat(paths.TokenPath)
			if err != nil {
				t.Fatalf("stat original token: %v", err)
			}
			entropyCalls := 0
			store.random = windowsTokenReadFunc(func([]byte) (int, error) {
				entropyCalls++
				return 0, errors.New("unexpected token generation")
			})
			release := holdWindowsTokenReadConflict(t, paths.TokenPath)
			var token Token
			if operation == "Load" {
				token, err = store.Load(controltransport.DefaultInstance)
			} else {
				token, err = store.LoadOrCreate(controltransport.DefaultInstance)
			}
			assertWindowsTokenOpenConflict(t, err, paths.TokenPath, original)
			if token != (Token{}) {
				t.Fatal("failed read returned token material")
			}
			if entropyCalls != 0 {
				t.Fatal("sharing failure triggered token generation")
			}
			release()
			assertWindowsTokenUnchanged(t, store, paths.TokenPath, before, original)
		})
	}
}

func TestWindowsCreationCollisionPreservesWinner(t *testing.T) {
	for _, denyWinnerRead := range []bool{false, true} {
		name := "readable_winner"
		if denyWinnerRead {
			name = "read_denied_winner"
		}
		t.Run(name, func(t *testing.T) {
			loser := newTestStore(t)
			winner, err := NewStore(loser.stateRoot)
			if err != nil {
				t.Fatalf("create winner store: %v", err)
			}
			paths, err := controltransport.ResolveInstancePaths(loser.stateRoot, controltransport.DefaultInstance)
			if err != nil {
				t.Fatalf("resolve token path: %v", err)
			}
			winner.random = bytes.NewReader(bytes.Repeat([]byte{0x51}, TokenSize))
			loserBytes := bytes.Repeat([]byte{0x52}, TokenSize)
			var winningToken Token
			var before os.FileInfo
			var release func()
			entropyCalls := 0
			// Generate runs only after the loser's initial lookup found no token.
			// Publish a distinct winner here, before the loser attempts installation.
			// This is a synchronous barrier using an existing seam, not a sleep.
			loser.random = windowsTokenReadFunc(func(p []byte) (int, error) {
				entropyCalls++
				if entropyCalls != 1 || len(p) != TokenSize {
					t.Fatal("unexpected entropy read in losing creator")
				}
				winningToken, err = winner.LoadOrCreate(controltransport.DefaultInstance)
				if err != nil {
					t.Fatalf("publish competing winner: %v", err)
				}
				before, err = os.Stat(paths.TokenPath)
				if err != nil {
					t.Fatalf("stat competing winner: %v", err)
				}
				if denyWinnerRead {
					release = holdWindowsTokenReadConflict(t, paths.TokenPath)
				}
				return copy(p, loserBytes), nil
			})
			returned, err := loser.LoadOrCreate(controltransport.DefaultInstance)
			if entropyCalls != 1 {
				t.Fatal("losing creator did not reach the controlled publication barrier")
			}
			if denyWinnerRead {
				assertWindowsTokenOpenConflict(t, err, paths.TokenPath, winningToken)
				if returned != (Token{}) {
					t.Fatal("failed winner read returned token material")
				}
				release()
			} else if err != nil || returned != winningToken {
				t.Fatalf("losing creator failed (code=%q, error=%t, same_token=%t)", CodeOfValue(err), err != nil, returned == winningToken)
			}
			assertWindowsTokenUnchanged(t, loser, paths.TokenPath, before, winningToken)
		})
	}
}

type windowsTokenReadFunc func([]byte) (int, error)

func (f windowsTokenReadFunc) Read(p []byte) (int, error) { return f(p) }

// Hold a data-read handle without read sharing. Write/delete sharing remains
// allowed; no token bytes are read through this handle.
func holdWindowsTokenReadConflict(t *testing.T, path string) func() {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("encode handle path: %v", err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("open controlled sharing handle: %v", err)
	}
	release := func() {
		t.Helper()
		if handle != windows.InvalidHandle {
			err := windows.CloseHandle(handle)
			handle = windows.InvalidHandle
			if err != nil {
				t.Errorf("close controlled sharing handle: %v", err)
			}
		}
	}
	t.Cleanup(release)
	// Keep the metadata and ACL checks ahead of os.Open observable. If this
	// preflight fails on Windows, the fixture has not isolated a data-open conflict.
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("held handle blocked regular-file metadata preflight: %v", err)
	}
	if err := applyPrivateWindowsACL(path); err != nil {
		t.Fatalf("held handle blocked private-ACL preflight: %v", err)
	}
	// Verify the harness really denies reads before testing the Store path.
	file, err := os.Open(path)
	if err == nil {
		_ = file.Close()
		t.Fatal("controlled handle unexpectedly permitted a data read")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("controlled handle did not cause a sharing violation: %v", err)
	}
	return release
}

func assertWindowsTokenOpenConflict(t *testing.T, err error, path string, original Token) {
	t.Helper()
	if err == nil {
		t.Fatal("held sharing conflict unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), original.String()) {
		t.Fatal("sharing error exposed token material")
	}
	var tokenErr *Error
	if !errors.As(err, &tokenErr) || tokenErr.Code != CodeStoreUnavailable || tokenErr.Path != "token_file" {
		t.Fatalf("sharing error did not retain the token-file unavailable category: %v", err)
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("sharing error lost the Windows cause: %v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "open" || pathErr.Path != path {
		t.Fatalf("sharing error did not originate at published-token open: %v", err)
	}
}

func assertWindowsTokenUnchanged(t *testing.T, store *Store, path string, before os.FileInfo, original Token) {
	t.Helper()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token after conflict: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("sharing conflict replaced the published token file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token after releasing handle: %v", err)
	}
	if string(data) != original.String() {
		t.Fatal("sharing conflict changed the published token bytes")
	}
	loaded, err := store.Load(controltransport.DefaultInstance)
	if err != nil || loaded != original {
		t.Fatalf("Load after release failed (code=%q, error=%t, same_token=%t)", CodeOfValue(err), err != nil, loaded == original)
	}
	reused, err := store.LoadOrCreate(controltransport.DefaultInstance)
	if err != nil || reused != original {
		t.Fatalf("LoadOrCreate after release failed (code=%q, error=%t, same_token=%t)", CodeOfValue(err), err != nil, reused == original)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("inspect token directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("token operation left an unexpected file or creation temporary")
	}
}
