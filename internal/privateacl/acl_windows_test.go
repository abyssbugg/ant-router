//go:build windows

package privateacl

import (
	"os"
	"path/filepath"
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
