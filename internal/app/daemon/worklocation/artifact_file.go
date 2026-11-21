package worklocation

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const MaxDeliveryFileBytes = 30_000_000

// ReadDeliveryFile reads one declared file, with OS-enforced confinement even
// when a path component is replaced between validation and opening.
func ReadDeliveryFile(directory, path string) ([]byte, error) {
	if !filepath.IsAbs(directory) || !filepath.IsLocal(path) || filepath.Clean(path) == "." {
		return nil, fmt.Errorf("use a file path relative to the current run directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	before, err := root.Stat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxDeliveryFileBytes {
		return nil, fmt.Errorf("delivery requires a nonempty regular file up to 30 MB")
	}
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("file changed before it could be saved")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxDeliveryFileBytes+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if after.Size() != int64(len(data)) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || len(data) > MaxDeliveryFileBytes {
		return nil, fmt.Errorf("file changed while being saved; finish writing it before retrying")
	}
	return data, nil
}

func DeliveryFileName(path string) (string, error) {
	name := filepath.Base(path)
	if strings.TrimSpace(name) == "" || len(name) > 255 || strings.ContainsAny(name, "\r\n\x00/\\") {
		return "", fmt.Errorf("invalid delivery file name")
	}
	return name, nil
}
