//go:build windows

package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/daemon"
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
	descriptor, err := windows.GetNamedSecurityInfo(logPath, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sddl := descriptor.String()
	dacl := sddl[strings.Index(sddl, "D:"):]
	aces := strings.Count(dacl, "(")
	if !strings.HasPrefix(dacl, "D:P") || aces != 2 || !strings.Contains(dacl, "(A;;FA;;;SY)") || !strings.Contains(dacl, "(A;;FA;;;"+user.User.Sid.String()+")") {
		t.Fatalf("log file DACL = %q, want protected with only SYSTEM and the current user", dacl)
	}
}
