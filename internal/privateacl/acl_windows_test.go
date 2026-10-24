//go:build windows

package privateacl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestApplyMakesFileAndDirectoryPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Freshly created objects inherit the temp directory's ACL.
	if err := Verify(file); err == nil {
		t.Fatal("Verify accepted an inherited ACL")
	}
	dirSD, err := DirectoryDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	fileSD, err := FileDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToPath(dir, dirSD); err != nil {
		t.Fatal(err)
	}
	if err := ApplyToPath(file, fileSD); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, file} {
		if err := Verify(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyRejectsExtraAccess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	open, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + sid + ")(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToPath(file, open); err != nil {
		t.Fatal(err)
	}
	if err := Verify(file); err == nil {
		t.Fatal("Verify accepted an Everyone read entry")
	}
}

func TestVerifyDescriptorChecksEveryEntry(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, tc := range []struct {
		name, sddl, want string
	}{
		{"private", "D:P(A;;FA;;;SY)(A;;FA;;;" + sid + ")", ""},
		{"private inheritable", "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid + ")", ""},
		{"empty protected DACL", "D:P", "both SYSTEM and the current user"},
		{"single entry", "D:P(A;;FA;;;SY)", "both SYSTEM and the current user"},
		{"wrong mask for user", "D:P(A;;FA;;;SY)(A;;FR;;;" + sid + ")", "want full access"},
		{"everyone instead of user", "D:P(A;;FA;;;SY)(A;;FA;;;WD)", "grants access to"},
		{"deny entry", "D:P(A;;FA;;;SY)(D;;FA;;;" + sid + ")", "not an access-allowed entry"},
		{"extra everyone read", "D:P(A;;FA;;;SY)(A;;FA;;;" + sid + ")(A;;FR;;;WD)", "grants access to"},
		{"unprotected", "D:(A;;FA;;;SY)(A;;FA;;;" + sid + ")", "not protected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tc.sddl)
			if err != nil {
				t.Fatal(err)
			}
			err = verifyDescriptor(sd, user.User.Sid)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("verifyDescriptor(%s) = %v", tc.sddl, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("verifyDescriptor(%s) = %v, want %q", tc.sddl, err, tc.want)
			}
		})
	}
}

// The descriptor built for a LocalSystem process names SYSTEM twice; it
// must still verify, because SIDs are compared by value.
func TestVerifyDescriptorAcceptsSystemAsCurrentUser(t *testing.T) {
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + system.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyDescriptor(sd, system); err != nil {
		t.Fatalf("verifyDescriptor as SYSTEM = %v", err)
	}
}

func TestApplyToHandleReplacesTheDACL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(file); err == nil {
		t.Fatal("fixture must start with an inherited ACL")
	}
	name, err := windows.UTF16PtrFromString(file)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	sd, err := FileDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyToHandle(handle, sd); err != nil {
		t.Fatalf("ApplyToHandle: %v", err)
	}
	if err := Verify(file); err != nil {
		t.Fatalf("Verify after ApplyToHandle: %v", err)
	}
}
