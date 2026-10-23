//go:build windows

package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/daemon"
	"github.com/abyssbugg/ant-router/internal/privateacl"
	"golang.org/x/sys/windows"
)

// On Windows the log file must be owner-only through its ACL: a protected
// DACL (no inherited entries) granting only SYSTEM and the current user.
func TestRunLogFileIsPrivateOnWindows(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "logs", "routerd.log")
	var stderr strings.Builder
	factory := func(daemon.Options) (daemonRunner, error) { return fakeRunner{}, nil }
	if code := run(context.Background(), []string{"--log-file", logPath}, io.Discard, &stderr, factory); code != 0 {
		t.Fatalf("run() = %d stderr %q, want 0", code, stderr.String())
	}
	requirePrivateDACL(t, logPath)
}

// A log created in a directory whose inheritable ACL grants Everyone read
// access is private from the moment it exists: the protected DACL is part
// of creation, not applied afterwards.
func TestCreatePrivateLogFileDoesNotInheritAccess(t *testing.T) {
	dir := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	open, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := open.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "routerd.log")
	file, err := createPrivateLogFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("line\n"); err != nil {
		t.Fatalf("write log: %v", err)
	}
	requirePrivateDACL(t, logPath)
}

func requirePrivateDACL(t *testing.T, path string) {
	t.Helper()
	if err := privateacl.Verify(path); err != nil {
		t.Fatal(err)
	}
}
