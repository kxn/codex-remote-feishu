package daemon

import (
	"fmt"
	"log"
	"strings"
)

type persistedStore interface {
	Dirty() bool
	Save() error
}

type persistedStoreStatus uint8

const (
	persistedStoreStatusWritable persistedStoreStatus = iota
	persistedStoreStatusDegraded
	persistedStoreStatusHistoryOnly
)

type persistedStoreRuntimeState[T persistedStore] struct {
	store         T
	status        persistedStoreStatus
	path          string
	diagnosticErr error
}

func loadPersistedStore[T persistedStore](label, path string, load func(string) (T, error)) persistedStoreRuntimeState[T] {
	path = strings.TrimSpace(path)
	store, err := load(path)
	if err != nil {
		var zero T
		return degradedPersistedStore(label, path, zero, fmt.Errorf("load: %w", err))
	}
	if store.Dirty() {
		if err := store.Save(); err != nil {
			return degradedPersistedStore(label, path, store, fmt.Errorf("persist sanitized state: %w", err))
		}
	}
	return persistedStoreRuntimeState[T]{
		store:  store,
		status: persistedStoreStatusWritable,
		path:   path,
	}
}

func (s persistedStoreRuntimeState[T]) writable() bool {
	return s.status == persistedStoreStatusWritable
}

// History files may need normalization to interpret them, but no migration or
// sanitization is allowed to rewrite the original evidence after cutover.
func loadHistoryStore[T persistedStore](label, path string, load func(string) (T, error)) persistedStoreRuntimeState[T] {
	path = strings.TrimSpace(path)
	store, err := load(path)
	if err != nil {
		var zero T
		return degradedPersistedStore(label, path, zero, fmt.Errorf("load history: %w", err))
	}
	return persistedStoreRuntimeState[T]{store: store, status: persistedStoreStatusHistoryOnly, path: path}
}

func degradedPersistedStore[T persistedStore](label, path string, store T, err error) persistedStoreRuntimeState[T] {
	log.Printf("persisted state entered read-only degraded mode: kind=%s path=%s err=%v", strings.TrimSpace(label), path, err)
	return persistedStoreRuntimeState[T]{
		store:         store,
		status:        persistedStoreStatusDegraded,
		path:          path,
		diagnosticErr: err,
	}
}
