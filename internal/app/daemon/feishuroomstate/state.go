package feishuroomstate

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/statestore"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	StateVersion       = 2
	legacyStateVersion = 1
	// Keep the original path so version 1 upgrades in place without a second store.
	StateFileName = "feishu-room-primary.json"
)

type Record = state.FeishuRoomStateRecord

func StatePath(stateDir string) string {
	return statestore.StatePath(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{Store: statestore.New[Record](path, statestore.Options[Record]{
		Version:         StateVersion,
		Name:            "feishu room state",
		Equal:           sameRecord,
		LoadNormalize:   func(record Record) (Record, bool) { return state.NormalizeFeishuRoomStateRecord(record) },
		LoadKey:         func(record Record) string { return state.FeishuRoomKey(record.RoomID) },
		LoadEqual:       sameRecord,
		DefaultVersion:  legacyStateVersion,
		LegacyVersions:  []int{legacyStateVersion},
	})}
}

func LoadStore(path string) (*Store, error) {
	store, err := statestore.Load[Record](path, statestore.Options[Record]{
		Version:         StateVersion,
		Name:            "feishu room state",
		Equal:           sameRecord,
		LoadNormalize:   func(record Record) (Record, bool) { return state.NormalizeFeishuRoomStateRecord(record) },
		LoadKey:         func(record Record) string { return state.FeishuRoomKey(record.RoomID) },
		LoadEqual:       sameRecord,
		DefaultVersion:  legacyStateVersion,
		LegacyVersions:  []int{legacyStateVersion},
	})
	if err != nil {
		return nil, err
	}
	return &Store{Store: store}, nil
}

type Store struct {
	*statestore.Store[Record]
}

func (s *Store) Get(key string) (Record, bool) {
	return s.Store.Get(state.FeishuRoomKey(key))
}

func (s *Store) Put(record Record) error {
	if s == nil {
		return nil
	}
	normalized, ok := state.NormalizeFeishuRoomStateRecord(record)
	if !ok {
		return fmt.Errorf("feishu room state requires room identity")
	}
	key := state.FeishuRoomKey(normalized.RoomID)
	if key == "" {
		return fmt.Errorf("feishu room state requires room identity")
	}
	entries := s.Entries()
	entries[key] = normalized
	return s.Replace(entries)
}

func (s *Store) Delete(key string) error {
	if s == nil {
		return nil
	}
	key = state.FeishuRoomKey(key)
	if key == "" {
		return nil
	}
	if _, ok := s.Get(key); !ok {
		return nil
	}
	entries := s.Entries()
	delete(entries, key)
	return s.Replace(entries)
}

func (s *Store) ReplaceAll(records []Record) error {
	if s == nil {
		return nil
	}
	entries := make(map[string]Record, len(records))
	for _, record := range records {
		normalized, ok := state.NormalizeFeishuRoomStateRecord(record)
		if !ok {
			return fmt.Errorf("feishu room state requires room identity")
		}
		entries[state.FeishuRoomKey(normalized.RoomID)] = normalized
	}
	return s.Replace(entries)
}

func sameRecord(left, right Record) bool {
	return left.RoomID == right.RoomID &&
		left.ChatID == right.ChatID &&
		left.WorkspaceKey == right.WorkspaceKey &&
		left.WorkspaceUpdatedBy == right.WorkspaceUpdatedBy &&
		left.WorkspaceUpdatedAt.Equal(right.WorkspaceUpdatedAt) &&
		left.WorkspaceResetGeneration == right.WorkspaceResetGeneration &&
		left.PrimaryGatewayID == right.PrimaryGatewayID &&
		left.PrimaryUpdatedBy == right.PrimaryUpdatedBy &&
		left.PrimaryUpdatedAt.Equal(right.PrimaryUpdatedAt) &&
		intPointerEqual(left.ConcurrencyLimit, right.ConcurrencyLimit)
}

func intPointerEqual(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
