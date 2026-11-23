// Package privatestaging owns private SDK download files. Its manifest is a
// local ownership receipt, never a replacement for the durable ingress record.
package privatestaging

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const DirectoryName = "private-ingress-staging"

var ErrUntrustedSource = errors.New("untrusted private staging source")

type Owner struct{ Tenant, AppID, Gateway, EventID, MessageID string }
type Entry struct {
	Name, Identity, SHA256 string
	Size                   int64
}
type Manifest struct {
	Version           int
	Owner             Owner
	DirectoryIdentity string
	Files             []Entry
}

type Store struct {
	dir      string
	manifest Manifest
}

func Open(base string, owner Owner) (*Store, error) {
	if !filepath.IsAbs(base) || owner.Tenant == "" || owner.AppID == "" || owner.Gateway == "" || owner.EventID == "" || owner.MessageID == "" {
		return nil, fmt.Errorf("invalid private staging identity")
	}
	root := filepath.Join(base, DirectoryName)
	if e := os.Mkdir(root, 0700); e != nil && !os.IsExist(e) {
		return nil, e
	}
	if e := checkDir(root); e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(owner)
	h := sha256.Sum256(raw)
	dir := filepath.Join(root, hex.EncodeToString(h[:]))
	if e := os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
		return nil, e
	}
	if e := checkDir(dir); e != nil {
		return nil, e
	}
	before, e := os.Lstat(dir)
	if e != nil {
		return nil, e
	}
	d, e := os.Open(dir)
	if e != nil {
		return nil, e
	}
	defer d.Close()
	opened, e := d.Stat()
	if e != nil || !os.SameFile(before, opened) || !privateDirectory(opened) {
		return nil, fmt.Errorf("private staging directory changed: %w", ErrUntrustedSource)
	}
	id, e := identity(d)
	if e != nil {
		return nil, e
	}
	s := &Store{dir: dir, manifest: Manifest{Version: 1, Owner: owner, DirectoryIdentity: id}}
	data, e := readManifest(filepath.Join(dir, "manifest.json"))
	if e == nil {
		if json.Unmarshal(data, &s.manifest) != nil || s.manifest.Version != 1 || s.manifest.Owner != owner || s.manifest.DirectoryIdentity != id {
			return nil, fmt.Errorf("private staging manifest mismatch: %w", ErrUntrustedSource)
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	} else if e = s.save(); e != nil {
		return nil, e
	}
	if e = s.recoverIncomplete(); e != nil {
		return nil, e
	}
	return s, nil
}
func readManifest(path string) ([]byte, error) {
	before, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest is not regular: %w", ErrUntrustedSource)
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		return nil, fmt.Errorf("manifest changed: %w", ErrUntrustedSource)
	}
	return io.ReadAll(io.LimitReader(f, 8<<20))
}

func checkDir(path string) error {
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !privateDirectory(i) {
		return fmt.Errorf("private staging directory is not owned and private: %w", ErrUntrustedSource)
	}
	return nil
}
func (s *Store) save() error {
	b, e := json.Marshal(s.manifest)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(s.dir, ".manifest-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(s.dir, "manifest.json")); e != nil {
		return e
	}
	d, e := os.Open(s.dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// Create syncs the actual download and its inode/hash manifest before returning.
func (s *Store) Create(r io.Reader, suffix string) (string, error) {
	if suffix != "" && (!strings.HasPrefix(suffix, ".") || strings.ContainsAny(suffix, "/\\")) {
		return "", fmt.Errorf("invalid suffix")
	}
	f, e := os.CreateTemp(s.dir, "download-*"+suffix)
	if e != nil {
		return "", e
	}
	defer f.Close()
	path := f.Name()
	if e = f.Chmod(0600); e != nil {
		return "", e
	}
	id, e := identity(f)
	if e != nil {
		return "", e
	}
	// Record the owned inode before writing so restart can reconcile a partial
	// download without inferring ownership from its name.
	s.manifest.Files = append(s.manifest.Files, Entry{Name: filepath.Base(path), Identity: id})
	index := len(s.manifest.Files) - 1
	if e = s.save(); e != nil {
		return "", e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), r)
	if e != nil {
		return "", e
	}
	if e = f.Sync(); e != nil {
		return "", e
	}
	s.manifest.Files[index].SHA256 = hex.EncodeToString(h.Sum(nil))
	s.manifest.Files[index].Size = n
	if e = s.save(); e != nil {
		return "", e
	}
	return path, nil
}

func (s *Store) recoverIncomplete() error {
	kept := s.manifest.Files[:0]
	changed := false
	for _, entry := range s.manifest.Files {
		if entry.SHA256 != "" {
			kept = append(kept, entry)
			continue
		}
		path := filepath.Join(s.dir, entry.Name)
		if filepath.Base(entry.Name) != entry.Name {
			return fmt.Errorf("invalid staged file name: %w", ErrUntrustedSource)
		}
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			changed = true
			continue
		}
		if e != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("incomplete source changed: %w", ErrUntrustedSource)
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		opened, e := f.Stat()
		if e != nil || !os.SameFile(info, opened) {
			f.Close()
			return fmt.Errorf("incomplete source changed: %w", ErrUntrustedSource)
		}
		id, e := identity(f)
		f.Close()
		if e != nil || id != entry.Identity {
			return fmt.Errorf("incomplete source identity changed: %w", ErrUntrustedSource)
		}
		current, e := os.Lstat(path)
		if e != nil || !os.SameFile(info, current) {
			return fmt.Errorf("incomplete source changed: %w", ErrUntrustedSource)
		}
		if e = os.Remove(path); e != nil {
			return e
		}
		changed = true
	}
	s.manifest.Files = kept
	if changed {
		return s.save()
	}
	return nil
}

// Cleanup removes only the exact file created by this store, after its caller
// has independently established that the durable target no longer needs it.
// A historical path without a matching manifest is retained.
func Cleanup(base string, owner Owner, path, expectedHash string) error {
	s, e := Open(base, owner)
	if e != nil {
		return e
	}
	if filepath.Clean(filepath.Dir(path)) != s.dir || filepath.Base(path) == "manifest.json" {
		return fmt.Errorf("source is outside private staging: %w", ErrUntrustedSource)
	}
	index := -1
	for i, v := range s.manifest.Files {
		if v.Name == filepath.Base(path) {
			index = i
			break
		}
	}
	if index < 0 {
		// A previous cleanup may have removed both the file and manifest
		// entry before its journal completion was committed.
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("source is not in manifest: %w", ErrUntrustedSource)
	}
	entry := s.manifest.Files[index]
	if expectedHash == "" || entry.SHA256 != expectedHash {
		return fmt.Errorf("source hash mismatch: %w", ErrUntrustedSource)
	}
	before, e := os.Lstat(path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if e == nil {
		if !before.Mode().IsRegular() {
			return fmt.Errorf("source is not a regular file: %w", ErrUntrustedSource)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		after, err := f.Stat()
		if err != nil || !os.SameFile(before, after) {
			f.Close()
			return fmt.Errorf("source changed: %w", ErrUntrustedSource)
		}
		id, err := identity(f)
		if err != nil || id != entry.Identity {
			f.Close()
			return fmt.Errorf("source identity changed: %w", ErrUntrustedSource)
		}
		h := sha256.New()
		n, err := io.Copy(h, f)
		f.Close()
		if err != nil || n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
			return fmt.Errorf("source content changed: %w", ErrUntrustedSource)
		}
		current, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, current) {
			return fmt.Errorf("source changed: %w", ErrUntrustedSource)
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	}
	s.manifest.Files = append(s.manifest.Files[:index], s.manifest.Files[index+1:]...)
	return s.save()
}

// IsManagedPath checks the exact event directory before any cleanup attempt.
func IsManagedPath(base string, owner Owner, path string) bool {
	if !filepath.IsAbs(base) || !filepath.IsAbs(path) {
		return false
	}
	raw, _ := json.Marshal(owner)
	h := sha256.Sum256(raw)
	dir := filepath.Join(base, DirectoryName, hex.EncodeToString(h[:]))
	return filepath.Clean(filepath.Dir(path)) == dir && filepath.Base(path) != "manifest.json"
}

// ExpectedHash returns the recorded download hash for this exact SDK event.
func ExpectedHash(base string, owner Owner, path string) (string, error) {
	if !IsManagedPath(base, owner, path) {
		return "", fmt.Errorf("source is outside private staging: %w", ErrUntrustedSource)
	}
	s, e := Open(base, owner)
	if e != nil {
		return "", e
	}
	for _, entry := range s.manifest.Files {
		if entry.Name == filepath.Base(path) && entry.SHA256 != "" {
			return entry.SHA256, nil
		}
	}
	if _, e = os.Lstat(path); os.IsNotExist(e) {
		return "", nil
	}
	return "", fmt.Errorf("source is not in manifest: %w", ErrUntrustedSource)
}
