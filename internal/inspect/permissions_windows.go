//go:build windows

package inspect

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func createPrivateTemp(directory string) (*os.File, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + user.User.Sid.String() + ")")
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
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return ErrUnavailable
	}
	sddl := descriptor.String()
	if !strings.HasPrefix(sddl, "D:P") || strings.Count(sddl, "(A;") != 2 || !strings.Contains(sddl, ";;;SY)") || !strings.Contains(sddl, ";;;"+user.User.Sid.String()+")") {
		return ErrInvalid
	}
	return nil
}
