package workauth

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// LoadOrCreate keeps credentials valid across daemon restarts. Publication is
// exclusive and happens after a complete, synced write; concurrent startup
// cannot replace a key or observe a half-written one. Corrupt keys fail closed.
func LoadOrCreate(path string, now func() time.Time) (*Signer, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("credential key path must be absolute")
	}
	key, err := readKey(path)
	if err == nil {
		return New(key, now)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".work-key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return nil, err
	}
	if _, err = file.Write(key); err != nil {
		return nil, err
	}
	if err = file.Sync(); err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if err = os.Link(file.Name(), path); err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
		key, err = readKey(path)
		if err != nil {
			return nil, err
		}
	}
	return New(key, now)
}

func readKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != 32 {
		return nil, fmt.Errorf("invalid credential key file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("credential key must be owner-only")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("credential key changed during read")
	}
	data, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil {
		return nil, err
	}
	if len(data) != 32 {
		return nil, fmt.Errorf("invalid credential key length")
	}
	return data, nil
}
