//go:build windows

package codexownership

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func secureDirectory(path string) error { return applyPrivateWindowsACL(path) }
func secureFile(path string) error      { return applyPrivateWindowsACL(path) }

func applyPrivateWindowsACL(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sddl := fmt.Sprintf("D:P(A;;FA;;;SY)(A;;FA;;;%s)", user.User.Sid.String())
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
