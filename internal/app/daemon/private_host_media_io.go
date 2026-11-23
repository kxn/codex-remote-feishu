package daemon

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func privateStageDir(root *os.Root, relative string) (*os.Root, error) {
	path := ""
	var last os.FileInfo
	for _, part := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("invalid attachment target")
		}
		path = filepath.Join(path, part)
		info, err := root.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if err = root.Mkdir(path, 0755); err != nil && !errors.Is(err, os.ErrExist) {
				return nil, err
			}
			info, err = root.Lstat(path)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("attachment directory changed")
		}
		last = info
	}
	dir, err := root.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := dir.Open(".")
	if err != nil {
		dir.Close()
		return nil, err
	}
	info, err := opened.Stat()
	opened.Close()
	if err != nil || !os.SameFile(last, info) {
		dir.Close()
		return nil, fmt.Errorf("attachment directory replaced")
	}
	return dir, nil
}

func privateVerifyStageDirectory(root *os.Root, relative string, want os.FileInfo) error {
	path := ""
	for _, part := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		path = filepath.Join(path, part)
		info, err := root.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("attachment directory replaced")
		}
		if path == filepath.Clean(relative) && !os.SameFile(info, want) {
			return fmt.Errorf("attachment directory replaced")
		}
	}
	return nil
}

func privateHashOpened(file *os.File) (string, int64, error) {
	if _, err := file.Seek(0, 0); err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(h, file)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func privateStageOne(root *os.Root, workspace string, item privateStagedFile) error {
	dirPath, name := filepath.Dir(item.Target), filepath.Base(item.Target)
	dir, err := privateStageDir(root, dirPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	parent, err := dir.Open(".")
	if err != nil {
		return err
	}
	defer parent.Close()
	parentID, err := privateDirectoryIdentity(parent)
	if err != nil {
		return err
	}
	parentInfo, err := parent.Stat()
	if err != nil {
		return err
	}
	if err = privateVerifyStageDirectory(root, dirPath, parentInfo); err != nil {
		return err
	}
	verify := func() error {
		info, e := dir.Lstat(name)
		if e != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("attachment target changed")
		}
		f, e := dir.Open(name)
		if e != nil {
			return e
		}
		defer f.Close()
		opened, e := f.Stat()
		if e != nil || !os.SameFile(info, opened) {
			return fmt.Errorf("attachment target replaced")
		}
		hash, size, e := privateHashOpened(f)
		if e != nil || hash != item.SHA256 || size != item.Size {
			return fmt.Errorf("attachment target content differs")
		}
		return nil
	}
	if _, err = dir.Lstat(name); err == nil {
		return verify()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := ".private-stage-" + hex.EncodeToString(nonce[:])
	dst, err := dir.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	src, err := os.Open(item.Source)
	if err != nil {
		dst.Close()
		return err
	}
	h := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(dst, h), src)
	src.Close()
	if copyErr == nil {
		copyErr = dst.Sync()
	}
	closeErr := dst.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil || size != item.Size || hex.EncodeToString(h.Sum(nil)) != item.SHA256 {
		return fmt.Errorf("attachment source changed during staging")
	}
	if err = privateStageLink(parent, filepath.Join(workspace, dirPath), temp, name); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err = verify(); err != nil {
		return err
	}
	current, err := os.Open(filepath.Join(workspace, dirPath))
	if err != nil {
		return err
	}
	currentID, err := privateDirectoryIdentity(current)
	current.Close()
	if err != nil || currentID != parentID {
		return fmt.Errorf("attachment parent replaced")
	}
	if err = privateVerifyStageDirectory(root, dirPath, parentInfo); err != nil {
		return err
	}
	tempInfo, tempErr := dir.Lstat(temp)
	targetInfo, targetErr := dir.Lstat(name)
	if tempErr == nil && targetErr == nil && os.SameFile(tempInfo, targetInfo) {
		_ = dir.Remove(temp)
	}
	return nil
}
