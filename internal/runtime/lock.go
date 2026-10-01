package relayruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, writeErr := file.Write(raw); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return nil, writeErr
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, closeErr
			}
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

// lockStartTimeTolerance 吸收时钟抖动：只有当占用该 PID 的进程启动时间明显晚于
// 锁的创建时间时，才认定这个 PID 已经被系统复用。
const lockStartTimeTolerance = time.Second

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
		// 锁文件被截断或损坏时同样无法恢复，必须清理，否则 daemon 会永久卡死。
		return removeLockFile(path)
	}
	if lockHolderActive(record) {
		return false, nil
	}
	return removeLockFile(path)
}

// lockHolderActive 判断当初写锁的进程是否仍然是当前占用该 PID 的进程。
//
// 仅凭 processAlive 不足以判断锁仍然有效：持有者被强杀（SIGKILL、断电、launchd
// 退出超时）后锁文件会残留，一旦该 PID 被系统复用给无关进程，processAlive 就恒
// 为真，daemon 会永久认为自己拿不到锁，只能人工删锁文件才能恢复。
//
// 锁的创建者其进程启动时间必然不晚于锁的创建时间；若当前占用该 PID 的进程启动
// 得更晚，它就不可能是当初写锁的那个进程。
func lockHolderActive(record lockRecord) bool {
	if record.PID <= 0 || !processAlive(record.PID) {
		return false
	}
	if record.CreatedAt.IsZero() {
		return true
	}
	start, ok := processStartTime(record.PID)
	if !ok {
		// 取不到启动时间时保持旧行为，避免误清有效锁。
		return true
	}
	return !start.After(record.CreatedAt.Add(lockStartTimeTolerance))
}

func removeLockFile(path string) (bool, error) {
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
