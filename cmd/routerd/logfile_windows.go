//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// securePrivateFile gives the open log file a protected DACL granting access only to
// SYSTEM and the current user, the Windows equivalent of mode 0600 (the
// permission bits passed to OpenFile do not restrict access on Windows).
func securePrivateFile(file *os.File) error {
	path := file.Name()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;FA;;;SY)(A;;FA;;;%s)", user.User.Sid.String()))
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
