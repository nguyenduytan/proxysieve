//go:build !windows

package inspect

import "os"

func secureFile(path string) error {
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	return verifySecureFile(path)
}

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
