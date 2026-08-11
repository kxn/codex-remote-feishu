// Package statestore provides a generic JSON-backed state store used by the
// daemon's per-domain state packages (bot capability settings, Feishu room
// state, surface resume, ...).
//
// The packages previously each carried their own copy of the same
// StateFile + Store + Save / replaceEntries / sameEntries template. This
// package converges that template into a single implementation; each domain
// package keeps only its record type, key normalization, validation and
// migration callbacks.
//
// The on-disk JSON layout (file name, version field, entries structure) is a
// persistence contract and must not change. LoadStore must keep accepting the
// exact same files it accepted before.
package statestore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
)

// StateFile is the persisted JSON shape shared by all domain state stores.
type StateFile[T any] struct {
	Version int          `json:"version"`
	Entries map[string]T `json:"entries,omitempty"`
}

// Options carries the per-domain behavior that used to live in each package's
// copied template.
type Options[T any] struct {
	// Version is the current on-disk version written by Save.
	Version int
	// Name is used in version error messages (e.g. "bot capability settings").
	Name string
	// Equal compares two records for content equality. It is required for
	// Replace's change detection. Use reflect-free per-record comparison.
	Equal func(T, T) bool
	// Clone deep-copies a record before it is handed out by Entries/Get.
	// Optional; leave nil to hand out values as-is.
	Clone func(T) T

	// LoadNormalize normalizes a record while loading; returning ok=false
	// drops the record and marks the store dirty. Optional.
	LoadNormalize func(T) (T, bool)
	// LoadKey derives the canonical key for a normalized record; returning
	// "" drops the record and marks the store dirty. Optional.
	LoadKey func(T) string
	// LoadEqual compares a raw persisted record against its normalized form;
	// returning false marks the store dirty. Optional.
	LoadEqual func(T, T) bool
	// LoadPost post-processes the whole entries map after per-record loading;
	// returning changed=true marks the store dirty. Optional.
	LoadPost func(map[string]T) (map[string]T, bool)
	// DefaultVersion is the version assumed when the file has version 0
	// (older files written before the version field existed). Defaults to
	// Version when zero.
	DefaultVersion int
	// LegacyVersions lists additional versions that LoadStore accepts and
	// transparently migrates by rewriting on the next Save.
	LegacyVersions []int
	// GetKey normalizes a lookup key in Get. Defaults to strings.TrimSpace.
	GetKey func(string) string
}

// Store is a generic JSON-backed entries store with atomic Save semantics.
type Store[T any] struct {
	path    string
	entries map[string]T
	dirty   bool
	opts    Options[T]
}

// StatePath joins stateDir with fileName, mirroring the old per-package
// StatePath helpers.
func StatePath(stateDir, fileName string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, fileName)
}

// New creates an empty store bound to path.
func New[T any](path string, opts Options[T]) *Store[T] {
	if opts.DefaultVersion == 0 {
		opts.DefaultVersion = opts.Version
	}
	if opts.GetKey == nil {
		opts.GetKey = strings.TrimSpace
	}
	return &Store[T]{
		path:    strings.TrimSpace(path),
		entries: map[string]T{},
		opts:    opts,
	}
}

// Path returns the store's file path.
func (s *Store[T]) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Version returns the on-disk version this store writes.
func (s *Store[T]) Version() int {
	if s == nil {
		return 0
	}
	return s.opts.Version
}

// Load reads the store from disk. A missing file yields an empty, clean
// store. Version mismatches are an error unless the version is listed in
// LegacyVersions (in which case the store is marked dirty for migration).
func Load[T any](path string, opts Options[T]) (*Store[T], error) {
	store := New[T](path, opts)
	if store.path == "" {
		return store, nil
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	var persisted StateFile[T]
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return nil, err
	}
	version := persisted.Version
	if version == 0 {
		version = store.opts.DefaultVersion
	}
	if version != store.opts.Version && !containsVersion(store.opts.LegacyVersions, version) {
		return nil, fmt.Errorf("unsupported %s state version: %d", store.opts.Name, version)
	}
	if version != store.opts.Version {
		store.dirty = true
	}
	for key, record := range persisted.Entries {
		rawRecord := record
		if store.opts.LoadNormalize != nil {
			normalized, ok := store.opts.LoadNormalize(record)
			if !ok {
				store.dirty = true
				continue
			}
			record = normalized
		}
		canonicalKey := store.opts.GetKey(key)
		if store.opts.LoadKey != nil {
			canonicalKey = store.opts.LoadKey(record)
			if canonicalKey == "" {
				store.dirty = true
				continue
			}
		}
		if strings.TrimSpace(key) != canonicalKey {
			store.dirty = true
		}
		if store.opts.LoadEqual != nil && !store.opts.LoadEqual(rawRecord, record) {
			store.dirty = true
		}
		store.entries[canonicalKey] = record
	}
	if store.opts.LoadPost != nil {
		if canonical, changed := store.opts.LoadPost(store.entries); changed {
			store.entries = canonical
			store.dirty = true
		}
	}
	return store, nil
}

// SetEntries replaces the in-memory entries without touching the disk. Used
// by LoadStore implementations with custom parsing.
func (s *Store[T]) SetEntries(entries map[string]T) {
	if s == nil {
		return
	}
	s.entries = entries
}

// MarkDirty flags the store as requiring a rewrite.
func (s *Store[T]) MarkDirty() {
	if s == nil {
		return
	}
	s.dirty = true
}

// Entries returns a copy of the entries map.
func (s *Store[T]) Entries() map[string]T {
	if s == nil || len(s.entries) == 0 {
		return map[string]T{}
	}
	values := make(map[string]T, len(s.entries))
	for key, record := range s.entries {
		if s.opts.Clone != nil {
			record = s.opts.Clone(record)
		}
		values[key] = record
	}
	return values
}

// Get returns the record stored under key.
func (s *Store[T]) Get(key string) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}
	record, ok := s.entries[s.opts.GetKey(key)]
	if !ok {
		return zero, false
	}
	if s.opts.Clone != nil {
		record = s.opts.Clone(record)
	}
	return record, true
}

// Replace swaps the entries and persists them, skipping the write when the
// content is unchanged (mirrors the old replaceEntries semantics).
func (s *Store[T]) Replace(entries map[string]T) error {
	if s == nil {
		return nil
	}
	if sameEntries(s.entries, entries, s.opts.Equal) {
		return nil
	}
	previous := s.entries
	s.entries = entries
	if err := s.Save(); err != nil {
		s.entries = previous
		return err
	}
	return nil
}

// Save atomically writes the entries to disk with the current version.
func (s *Store[T]) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	persisted := StateFile[T]{
		Version: s.opts.Version,
		Entries: s.Entries(),
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := atomicfile.Write(s.path, raw, 0o600); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// Dirty reports whether the store has pending changes that require a rewrite.
func (s *Store[T]) Dirty() bool {
	if s == nil {
		return false
	}
	return s.dirty
}

func sameEntries[T any](left, right map[string]T, equal func(T, T) bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftRecord := range left {
		rightRecord, ok := right[key]
		if !ok || !equal(leftRecord, rightRecord) {
			return false
		}
	}
	return true
}

func containsVersion(versions []int, target int) bool {
	for _, version := range versions {
		if version == target {
			return true
		}
	}
	return false
}
