//go:build windows

package inspect

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func createPrivateTemp(directory string) (*os.File, error) {
	descriptor, err := privateSecurityDescriptor()
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	for range 100 {
		var random [16]byte
		if _, err = rand.Read(random[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(directory, ".ca-"+hex.EncodeToString(random[:])+".tmp")
		path, pathErr := windows.UTF16PtrFromString(name)
		if pathErr != nil {
			return nil, pathErr
		}
		handle, createErr := windows.CreateFile(path, windows.GENERIC_WRITE, 0, &attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if createErr == windows.ERROR_FILE_EXISTS || createErr == windows.ERROR_ALREADY_EXISTS {
			continue
		}
		if createErr != nil {
			return nil, createErr
		}
		file := os.NewFile(uintptr(handle), name)
		if file == nil {
			_ = windows.CloseHandle(handle)
			return nil, ErrUnavailable
		}
		return file, nil
	}
	return nil, ErrUnavailable
}

func privateSecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + user.User.Sid.String() + ")")
}

func installFile(source, target string) error {
	err := moveFile(source, target, windows.MOVEFILE_WRITE_THROUGH)
	if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_FILE_EXISTS {
		return os.ErrExist
	}
	return err
}

func moveFile(source, target string, flags uint32) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, flags)
}

func replaceFile(source, target string) error {
	return moveFile(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func verifySecureFile(path string) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND {
			return ErrNotFound
		}
		return ErrUnavailable
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return ErrUnavailable
	}
	actual, _, err := descriptor.DACL()
	if err != nil {
		return ErrUnavailable
	}
	expectedDescriptor, err := privateSecurityDescriptor()
	if err != nil {
		return ErrUnavailable
	}
	expected, _, err := expectedDescriptor.DACL()
	if err != nil {
		return ErrUnavailable
	}
	if control&windows.SE_DACL_PROTECTED == 0 || !sameACL(actual, expected) {
		return ErrInvalid
	}
	return nil
}

func sameACL(actual, expected *windows.ACL) bool {
	if actual == nil || expected == nil || actual.AceCount != expected.AceCount {
		return false
	}
	matched := make([]bool, expected.AceCount)
	for actualIndex := range uint32(actual.AceCount) {
		var actualACE *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(actual, actualIndex, &actualACE) != nil || actualACE.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		actualSID := (*windows.SID)(unsafe.Pointer(&actualACE.SidStart))
		found := false
		for expectedIndex := range uint32(expected.AceCount) {
			if matched[expectedIndex] {
				continue
			}
			var expectedACE *windows.ACCESS_ALLOWED_ACE
			if windows.GetAce(expected, expectedIndex, &expectedACE) != nil {
				return false
			}
			expectedSID := (*windows.SID)(unsafe.Pointer(&expectedACE.SidStart))
			if actualACE.Mask == expectedACE.Mask && actualACE.Header.AceFlags == expectedACE.Header.AceFlags && actualSID.Equals(expectedSID) {
				matched[expectedIndex], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
