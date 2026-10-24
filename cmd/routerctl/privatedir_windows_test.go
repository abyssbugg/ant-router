//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/abyssbugg/ant-router/internal/privateacl"
)

func TestEnsurePrivateLogDirAppliesOwnerOnlyDACL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	logDir := filepath.Join(root, "instances", "work", "logs")
	if err := ensurePrivateLogDir(root, logDir); err != nil {
		t.Fatalf("ensurePrivateLogDir: %v", err)
	}
	for _, dir := range []string{filepath.Join(root, "instances"), filepath.Join(root, "instances", "work"), logDir} {
		if err := privateacl.Verify(dir); err != nil {
			t.Fatalf("Verify(%s): %v", dir, err)
		}
	}
}

func TestEnsurePrivateLogDirTightensAnExistingPermissiveDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	logDir := filepath.Join(root, "work", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Grant Everyone full access, as an inherited or hand-made ACL might.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	if err := privateacl.ApplyToPath(logDir, sd); err != nil {
		t.Fatal(err)
	}
	if err := privateacl.Verify(logDir); err == nil {
		t.Fatal("fixture DACL must be permissive")
	}
	if err := ensurePrivateLogDir(root, logDir); err != nil {
		t.Fatalf("ensurePrivateLogDir: %v", err)
	}
	if err := privateacl.Verify(logDir); err != nil {
		t.Fatalf("existing log dir was not tightened: %v", err)
	}
}

func TestEnsurePrivateLogDirRefusesNonDirectoriesAndJunctions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(filepath.Join(root, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "work", "logs"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateLogDir(root, filepath.Join(root, "work", "logs")); err == nil {
		t.Fatal("a file at the log path must be refused")
	}

	// A directory symlink needs privileges (or developer mode); skip that
	// case when the host refuses to create one.
	target := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink unavailable, reparse-point case not covered: %v", err)
	}
	if err := ensurePrivateLogDir(root, filepath.Join(link, "logs")); err == nil {
		t.Fatal("a reparse point below the state root must be refused")
	}
	if _, err := os.Stat(filepath.Join(target, "logs")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be created through the reparse point: %v", err)
	}
}

// A component owned by another principal is refused. Changing an owner to
// SYSTEM needs SeRestorePrivilege; hosts without it skip with a reason.
func TestEnsurePrivateLogDirRefusesForeignOwnedComponents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	foreign := filepath.Join(root, "work")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	if user, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil && user.User.Sid.Equals(system) {
		t.Skip("running as SYSTEM: there is no other owner to plant")
	}
	if err := windows.SetNamedSecurityInfo(foreign, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, system, nil, nil, nil); err != nil {
		t.Skipf("cannot assign another owner on this host: %v", err)
	}
	err = ensurePrivateLogDir(root, filepath.Join(foreign, "logs"))
	if err == nil || !strings.Contains(err.Error(), "not the current user") {
		t.Fatalf("ensurePrivateLogDir = %v, want a foreign-owner refusal", err)
	}
}
