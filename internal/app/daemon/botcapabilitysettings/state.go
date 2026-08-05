package botcapabilitysettings

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/statestore"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	StateVersion  = 1
	StateFileName = "bot-capability-settings.json"
)

type Record = state.BotCapabilitySettingsRecord

func StatePath(stateDir string) string {
	return statestore.StatePath(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{Store: statestore.New[Record](path, statestore.Options[Record]{
		Version:       StateVersion,
		Name:          "bot capability settings",
		Equal:         func(left, right Record) bool { return left == right },
		LoadNormalize: state.NormalizeBotCapabilitySettingsRecord,
		LoadKey:       func(record Record) string { return state.BotCapabilitySettingsKey(record.GatewayID) },
	})}
}

func LoadStore(path string) (*Store, error) {
	store, err := statestore.Load[Record](path, statestore.Options[Record]{
		Version:       StateVersion,
		Name:          "bot capability settings",
		Equal:         func(left, right Record) bool { return left == right },
		LoadNormalize: state.NormalizeBotCapabilitySettingsRecord,
		LoadKey:       func(record Record) string { return state.BotCapabilitySettingsKey(record.GatewayID) },
	})
	if err != nil {
		return nil, err
	}
	return &Store{Store: store}, nil
}

type Store struct {
	*statestore.Store[Record]
}

func (s *Store) Put(record Record) error {
	if s == nil {
		return nil
	}
	record = state.CanonicalizeBotCapabilityProfileSelection(record)
	normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record)
	if !ok {
		return fmt.Errorf("bot capability settings requires gateway id")
	}
	key := state.BotCapabilitySettingsKey(normalized.GatewayID)
	if key == "" {
		return fmt.Errorf("bot capability settings requires gateway id")
	}
	entries := s.Entries()
	entries[key] = normalized
	return s.Replace(entries)
}

func (s *Store) ReplaceAll(records []Record) error {
	if s == nil {
		return nil
	}
	entries := make(map[string]Record, len(records))
	for _, record := range records {
		record = state.CanonicalizeBotCapabilityProfileSelection(record)
		normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record)
		if !ok {
			return fmt.Errorf("bot capability settings requires gateway id")
		}
		entries[state.BotCapabilitySettingsKey(normalized.GatewayID)] = normalized
	}
	return s.Replace(entries)
}
