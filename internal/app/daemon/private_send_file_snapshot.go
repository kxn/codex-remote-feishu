package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

// snapshotPrivateSendFile freezes the selected bytes before the asynchronous
// upload effect is committed. The file name is retained for the SDK upload.
func snapshotPrivateSendFile(ctx context.Context, root, effect, source string) (string, int64, string, error) {
	if root == "" || effect == "" {
		return "", 0, "", fmt.Errorf("private file staging unavailable")
	}
	f, err := os.Open(source)
	if err != nil {
		return "", 0, "", err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", 0, "", err
	}
	if !before.Mode().IsRegular() {
		return "", 0, "", fmt.Errorf("selected file is not regular")
	}
	key := sha256.Sum256([]byte(effect))
	dir := filepath.Join(root, "private-sendfile", hex.EncodeToString(key[:]))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", 0, "", err
	}
	tmp, err := os.CreateTemp(dir, ".snapshot-")
	if err != nil {
		return "", 0, "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err = tmp.Chmod(0600); err != nil {
		return "", 0, "", err
	}
	if err = ctx.Err(); err != nil {
		return "", 0, "", err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), f)
	if err != nil {
		return "", 0, "", err
	}
	after, err := f.Stat()
	if err != nil {
		return "", 0, "", err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || n != before.Size() {
		return "", 0, "", fmt.Errorf("selected file changed during preparation")
	}
	if err = ctx.Err(); err != nil {
		return "", 0, "", err
	}
	if err = tmp.Sync(); err != nil {
		return "", 0, "", err
	}
	if err = tmp.Close(); err != nil {
		return "", 0, "", err
	}
	name := filepath.Base(source)
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		return "", 0, "", fmt.Errorf("invalid selected file name")
	}
	target := filepath.Join(dir, name)
	if err = os.Rename(tmp.Name(), target); err != nil {
		return "", 0, "", err
	}
	d, err := os.Open(dir)
	if err != nil {
		return "", 0, "", err
	}
	err = d.Sync()
	_ = d.Close()
	if err != nil {
		return "", 0, "", err
	}
	return target, n, hex.EncodeToString(hash.Sum(nil)), nil
}

// A confirmed upload receipt no longer needs the staged bytes. Removing the
// file does not follow a symlink; paths outside the private staging tree are
// never cleanup candidates.
func cleanupPrivateFileSnapshot(root, path string) {
	if root == "" || path == "" {
		return
	}
	base := filepath.Join(root, "private-sendfile")
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || len(parts[0]) != 64 || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return
	}
	if _, err = hex.DecodeString(parts[0]); err != nil {
		return
	}
	_ = os.Remove(path)
	_ = os.Remove(filepath.Dir(path))
}

// Uses the existing staging cleanup loop. Only an applied, non-unknown upload
// receipt with no unresolved reference authorizes deletion.
func cleanupCompletedPrivateFileSnapshots(ctx context.Context, root string, j *privatechat.Journal) error {
	if root == "" {
		return nil
	}
	base := filepath.Join(root, "private-sendfile")
	dirs, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var paths []string
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !dir.IsDir() || len(dir.Name()) != 64 {
			continue
		}
		if _, err := hex.DecodeString(dir.Name()); err != nil {
			continue
		}
		files, err := os.ReadDir(filepath.Join(base, dir.Name()))
		if err != nil {
			continue
		}
		for _, file := range files {
			if !file.Type().IsRegular() || strings.HasPrefix(file.Name(), ".snapshot-") {
				continue
			}
			path := filepath.Join(base, dir.Name(), file.Name())
			paths = append(paths, path)
		}
	}
	eligibility, err := j.FileSnapshotsCleanupEligibility(ctx, paths)
	if err != nil {
		return err
	}
	removed := 0
	for _, path := range paths {
		if eligibility[path] {
			cleanupPrivateFileSnapshot(root, path)
			removed++
			if removed >= 128 {
				break
			}
		}
	}

	return nil
}
