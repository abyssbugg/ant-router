//go:build windows

package privateacl

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// currentUserSID returns the SID of the user the process runs as.
func currentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("current user: %w", err)
	}
	return user.User.Sid.String(), nil
}

// FileDescriptor is the private descriptor for a file.
func FileDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	return descriptor("")
}

// DirectoryDescriptor is the private descriptor for a directory; its
// entries are inherited by files and directories created inside it.
func DirectoryDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	return descriptor("OICI")
}

func descriptor(inheritance string) (*windows.SECURITY_DESCRIPTOR, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;%[1]s;FA;;;SY)(A;%[1]s;FA;;;%[2]s)", inheritance, sid))
}

// SecurityAttributes wraps a descriptor for CreateFile/CreateDirectory, so
// an object is private from the moment it exists.
func SecurityAttributes(sd *windows.SECURITY_DESCRIPTOR) *windows.SecurityAttributes {
	attributes := &windows.SecurityAttributes{SecurityDescriptor: sd}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	return attributes
}

const protectedDACL = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION

// ApplyToPath replaces the DACL of path with the descriptor's.
func ApplyToPath(path string, sd *windows.SECURITY_DESCRIPTOR) error {
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, protectedDACL, nil, nil, dacl, nil)
}

// ApplyToHandle replaces the DACL of an open object. The handle must have
// been opened with WRITE_DAC. Unlike ApplyToPath it cannot be redirected by
// a path swap after the handle was opened.
func ApplyToHandle(handle windows.Handle, sd *windows.SECURITY_DESCRIPTOR) error {
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, protectedDACL, nil, nil, dacl, nil)
}

// Verify reports an error unless path has a protected DACL whose only
// entries grant full access to SYSTEM and the current user.
func Verify(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	sddl := sd.String()
	start := strings.Index(sddl, "D:")
	if start < 0 {
		return fmt.Errorf("%s has no DACL", path)
	}
	dacl := sddl[start:]
	if !strings.HasPrefix(dacl, "D:P") {
		return fmt.Errorf("%s DACL %q is not protected", path, dacl)
	}
	entries := strings.Split(strings.TrimSuffix(strings.TrimPrefix(dacl[strings.Index(dacl, "("):], "("), ")"), ")(")
	if len(entries) != 2 {
		return fmt.Errorf("%s DACL %q has %d entries, want SYSTEM and the current user only", path, dacl, len(entries))
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		fields := strings.Split(entry, ";")
		if len(fields) != 6 || fields[0] != "A" || fields[2] != "FA" || (fields[5] != "SY" && fields[5] != sid) {
			return fmt.Errorf("%s DACL %q grants access beyond SYSTEM and the current user", path, dacl)
		}
		seen[fields[5]] = true
	}
	if !seen["SY"] || !seen[sid] {
		return fmt.Errorf("%s DACL %q must grant both SYSTEM and the current user", path, dacl)
	}
	return nil
}
