package relayruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrLockHeld = errors.New("lock held by another process")

type FileLock struct {
	path  string
	token string
}

type lockRecord struct {
	PID       int       `json:"pid"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"createdAt"`
}

func AcquireLock(ctx context.Context, path string, wait bool) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	record := lockRecord{
		PID:       os.Getpid(),
		Token:     token,
		CreatedAt: time.Now().UTC(),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}

	for {
		err := publishLock(path, func(writer io.Writer) error {
			_, err := writer.Write(raw)
			return err
		})
		if err == nil {
			return &FileLock{path: path, token: token}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		cleared, clearErr := clearStaleLock(path)
		if clearErr != nil {
			return nil, clearErr
		}
		if cleared {
			continue
		}
		if !wait {
			return nil, ErrLockHeld
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// publishLock exposes the owner record only after it is fully written and closed.
// A hard link creates the lock name exclusively; rename could replace an owner.
func publishLock(path string, writeRecord func(io.Writer) error) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if err := writeRecord(file); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Link(file.Name(), path)
}

func (l *FileLock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	raw, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var record lockRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	if record.Token != l.token {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func clearStaleLock(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	var record lockRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return false, err
	}
	if record.PID > 0 && processAlive(record.PID) {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func randomToken() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
