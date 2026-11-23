package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PersonalMigrationAsset struct {
	ContentID       string `json:"content_id"`
	SourceMessageID string `json:"source_message_id"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	MediaType       string `json:"media_type"`
	FileName        string `json:"file_name,omitempty"`
	Kind            string `json:"kind"`
	Path            string `json:"path"`
}

// materializePersonalMigrationAssets copies verified source blobs into a
// caller-owned directory. It never modifies the source or claims OS sandboxing.
func materializePersonalMigrationAssets(source PersonalMigrationSource, rootPath string) ([]PersonalMigrationAsset, error) {
	if err := source.Verify(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(rootPath) {
		return nil, fmt.Errorf("absolute asset root required")
	}
	canonical, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return nil, err
	}
	rootPath = canonical
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir := "migration-" + source.SHA256
	if err = root.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := root.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("invalid migration asset directory")
	}
	assets, err := root.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer assets.Close()
	content := map[string][]byte{}
	for _, r := range source.Source.Records {
		if r.Table == "content" {
			content[r.Key] = r.Data
		}
	}
	var out []PersonalMigrationAsset
	for _, r := range source.Source.Records {
		if r.Table != "messages" {
			continue
		}
		var msg work.Message
		if err = json.Unmarshal(r.Payload, &msg); err != nil {
			return nil, err
		}
		for _, attachment := range msg.Attachments {
			data, ok := content[attachment.ContentRef]
			if !ok {
				return nil, fmt.Errorf("attachment content missing")
			}
			sum := sha256.Sum256(data)
			digest := hex.EncodeToString(sum[:])
			if digest != attachment.SHA256 || int64(len(data)) != attachment.Size {
				return nil, fmt.Errorf("attachment integrity mismatch")
			}
			key := sha256.Sum256([]byte(attachment.ContentRef))
			name := hex.EncodeToString(key[:])
			file, e := assets.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if errors.Is(e, os.ErrExist) {
				if e = verifyPersonalAsset(assets, name, digest, attachment.Size); e != nil {
					return nil, e
				}
			} else if e != nil {
				return nil, e
			} else {
				_, writeErr := file.Write(data)
				if writeErr == nil {
					writeErr = file.Sync()
				}
				closeErr := file.Close()
				if writeErr != nil || closeErr != nil {
					_ = assets.Remove(name)
					return nil, errors.Join(writeErr, closeErr)
				}
			}
			out = append(out, PersonalMigrationAsset{ContentID: attachment.ContentRef, SourceMessageID: attachment.SourceMessageID, SHA256: digest, Size: attachment.Size, MediaType: attachment.MediaType, FileName: attachment.FileName, Kind: attachment.Kind, Path: filepath.Join(rootPath, dir, name)})
		}
	}
	dirFile, err := assets.Open(".")
	if err != nil {
		return nil, err
	}
	syncErr := dirFile.Sync()
	closeErr := dirFile.Close()
	if syncErr != nil || closeErr != nil {
		return nil, errors.Join(syncErr, closeErr)
	}
	return out, nil
}
func verifyPersonalAsset(root *os.Root, name, digest string, size int64) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != size {
		return fmt.Errorf("existing asset metadata mismatch")
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return fmt.Errorf("existing asset hash mismatch")
	}
	return nil
}
