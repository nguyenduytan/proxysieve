//go:build !windows

package inspect

import "os"

func createPrivateTemp(directory string) (*os.File, error) {
	file, err := os.CreateTemp(directory, ".ca-*.tmp")
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(0600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

func installFile(source, target string) error { return os.Link(source, target) }

func replaceFile(source, target string) error { return os.Rename(source, target) }

func verifySecureFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return ErrUnavailable
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return ErrInvalid
	}
	return nil
}
