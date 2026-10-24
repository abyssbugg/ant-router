//go:build windows

package privateacl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

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
		{"null DACL", "D:PNO_ACCESS_CONTROL", "DACL is null"},
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

// DirectoryDescriptor entries are inheritable, FileDescriptor entries are
// not, and a file created inside a private directory inherits only the
// SYSTEM and current-user entries.
func TestDirectoryDescriptorPropagatesToChildren(t *testing.T) {
	inheritance := func(t *testing.T, sd *windows.SECURITY_DESCRIPTOR) []uint8 {
		t.Helper()
		dacl, _, err := sd.DACL()
		if err != nil || dacl == nil {
			t.Fatalf("DACL: %v", err)
		}
		var flags []uint8
		for i := uint32(0); i < uint32(dacl.AceCount); i++ {
			var ace *windows.ACCESS_ALLOWED_ACE
			if err := windows.GetAce(dacl, i, &ace); err != nil {
				t.Fatal(err)
			}
			flags = append(flags, ace.Header.AceFlags)
		}
		return flags
	}
	dirSD, err := DirectoryDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	fileSD, err := FileDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	const oici = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	for _, f := range inheritance(t, dirSD) {
		if f&oici != oici {
			t.Fatalf("directory ACE flags %#x lack OI|CI", f)
		}
	}
	for _, f := range inheritance(t, fileSD) {
		if f&oici != 0 {
			t.Fatalf("file ACE flags %#x must not be inheritable", f)
		}
	}

	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ApplyToPath(dir, dirSD); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "child")
	if err := os.WriteFile(child, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	childSD, err := windows.GetNamedSecurityInfo(child, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := childSD.DACL()
	if err != nil || dacl == nil || dacl.AceCount == 0 {
		t.Fatalf("child DACL = %v, %v", dacl, err)
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if ace.Header.AceFlags&windows.INHERITED_ACE == 0 || (!sid.Equals(system) && !sid.Equals(user.User.Sid)) {
			t.Fatalf("child ACE %d: flags %#x sid %s, want an inherited SYSTEM or current-user entry", i, ace.Header.AceFlags, sid)
		}
	}
}

func TestCheckOwnerAcceptsObjectsCreatedByThisProcess(t *testing.T) {
	dir := t.TempDir()
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := CheckOwner(handle); err != nil {
		t.Fatalf("CheckOwner(temp dir) = %v", err)
	}
}
