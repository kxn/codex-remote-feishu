package goalinterlockstore

import (
	"reflect"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/statestore"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

const (
	StateVersion  = 1
	StateFileName = "goal-interlock-state.json"
)

type Entry struct {
	Key   string                          `json:"key"`
	Lease orchestrator.GoalInterlockLease `json:"lease"`
}

func StatePath(stateDir string) string {
	return statestore.StatePath(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{Store: statestore.New[Entry](path, statestore.Options[Entry]{
		Version: StateVersion,
		Name:    "goal queue interlock state",
		Equal:   sameEntry,
		LoadKey: func(entry Entry) string { return entry.Key },
	})}
}

func LoadStore(path string) (*Store, error) {
	store, err := statestore.Load[Entry](path, statestore.Options[Entry]{
		Version: StateVersion,
		Name:    "goal queue interlock state",
		Equal:   sameEntry,
		LoadKey: func(entry Entry) string { return entry.Key },
	})
	if err != nil {
		return nil, err
	}
	return &Store{Store: store}, nil
}

func sameEntry(left, right Entry) bool {
	return left.Key == right.Key && reflect.DeepEqual(left.Lease, right.Lease)
}

type Store struct {
	*statestore.Store[Entry]
}

func (s *Store) ReplaceAll(leases []orchestrator.GoalInterlockLease) error {
	if s == nil {
		return nil
	}
	entries := map[string]Entry{}
	for _, lease := range leases {
		key := strings.TrimSpace(lease.InstanceID) + "\x00" + strings.TrimSpace(lease.ThreadID)
		if key == "\x00" {
			continue
		}
		entries[key] = Entry{Key: key, Lease: lease}
	}
	return s.Replace(entries)
}

func (s *Store) Leases() []orchestrator.GoalInterlockLease {
	if s == nil {
		return nil
	}
	leases := make([]orchestrator.GoalInterlockLease, 0, len(s.Entries()))
	for _, entry := range s.Entries() {
		leases = append(leases, entry.Lease)
	}
	return leases
}
