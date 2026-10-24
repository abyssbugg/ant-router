//go:build windows

package privateacl

import (
	"errors"
	"fmt"
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

// fileAllAccess is FILE_ALL_ACCESS, the mask SDDL writes as "FA".
const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1FF

// Verify reports an error unless path has a protected DACL whose entries all
// grant full access to SYSTEM or the current user, and that grants both.
func Verify(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("current user: %w", err)
	}
	if err := verifyDescriptor(sd, user.User.Sid); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// verifyDescriptor checks the DACL entries themselves, comparing SIDs by
// value, so a process running as LocalSystem (whose user SID is SYSTEM)
// verifies the descriptor this package builds for it.
func verifyDescriptor(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID) error {
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("DACL is not protected from inheritance")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("no DACL: %w", err)
	}
	if dacl == nil {
		// A NULL DACL grants everyone full access.
		return errors.New("DACL is null")
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	var sawSystem, sawUser bool
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("read DACL entry %d: %w", index, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("DACL entry %d is not an access-allowed entry", index)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		isSystem, isUser := sid.Equals(system), sid.Equals(user)
		if !isSystem && !isUser {
			return fmt.Errorf("DACL entry %d grants access to %s", index, sid)
		}
		if ace.Mask != fileAllAccess {
			return fmt.Errorf("DACL entry %d grants mask %#x, want full access", index, ace.Mask)
		}
		sawSystem = sawSystem || isSystem
		sawUser = sawUser || isUser
	}
	if !sawSystem || !sawUser {
		return errors.New("DACL must grant full access to both SYSTEM and the current user")
	}
	return nil
}
