//go:build !windows

package catalogfile

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Catalogs are bounded to 4 MiB. Resolve symlinks, then open nonblocking and
// check the opened descriptor (not a raced pre-open stat). FIFOs, devices,
// sockets and directories are refused without consuming them.

func ReadRegular(path string, maxCatalogBytes int64) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open(resolved, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), resolved)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxCatalogBytes {
		return nil, os.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCatalogBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxCatalogBytes {
		return nil, os.ErrInvalid
	}
	return data, nil
}
