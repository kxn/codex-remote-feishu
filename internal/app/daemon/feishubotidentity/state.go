package feishubotidentity

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/statestore"
)

const (
	StateVersion  = 1
	StateFileName = "feishu-bot-identities.json"
)

type Record struct {
	GatewayID  string             `json:"gatewayID"`
	AppID      string             `json:"appID"`
	Generation uint64             `json:"generation"`
	UpdatedAt  time.Time          `json:"updatedAt,omitempty"`
	Pending    *PendingTransition `json:"pending,omitempty"`
}

type PendingTransition struct {
	DesiredAppID string    `json:"desiredAppID,omitempty"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
}

func StatePath(stateDir string) string {
	return statestore.StatePath(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{Store: statestore.New[Record](path, statestore.Options[Record]{
		Version:       StateVersion,
		Name:          "feishu bot identity",
		Equal:         sameRecord,
		Clone:         cloneRecord,
		LoadNormalize: func(record Record) (Record, bool) { return NormalizeRecord(record) },
		LoadKey:       func(record Record) string { return record.GatewayID },
		LoadEqual:     sameRecord,
	})}
}

func LoadStore(path string) (*Store, error) {
	store, err := statestore.Load[Record](path, statestore.Options[Record]{
		Version:       StateVersion,
		Name:          "feishu bot identity",
		Equal:         sameRecord,
		Clone:         cloneRecord,
		LoadNormalize: func(record Record) (Record, bool) { return NormalizeRecord(record) },
		LoadKey:       func(record Record) string { return record.GatewayID },
		LoadEqual:     sameRecord,
	})
	if err != nil {
		return nil, err
	}
	return &Store{Store: store}, nil
}

type Store struct {
	*statestore.Store[Record]
}

func NormalizeRecord(record Record) (Record, bool) {
	record.GatewayID = strings.TrimSpace(record.GatewayID)
	record.AppID = strings.TrimSpace(record.AppID)
	if record.GatewayID == "" || record.AppID == "" {
		return Record{}, false
	}
	if record.Generation == 0 {
		record.Generation = 1
	}
	if !record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	if record.Pending != nil {
		pending := PendingTransition{
			DesiredAppID: strings.TrimSpace(record.Pending.DesiredAppID),
			StartedAt:    record.Pending.StartedAt,
		}
		if !pending.StartedAt.IsZero() {
			pending.StartedAt = pending.StartedAt.UTC()
		}
		record.Pending = &pending
	}
	return record, true
}

func (s *Store) Put(record Record) error {
	if s == nil {
		return nil
	}
	normalized, ok := NormalizeRecord(record)
	if !ok {
		return fmt.Errorf("feishu bot identity requires gateway id and app id")
	}
	entries := s.Entries()
	entries[normalized.GatewayID] = normalized
	return s.Replace(entries)
}

func sameRecord(left, right Record) bool {
	if strings.TrimSpace(left.GatewayID) == strings.TrimSpace(right.GatewayID) &&
		strings.TrimSpace(left.AppID) == strings.TrimSpace(right.AppID) &&
		left.Generation == right.Generation &&
		left.UpdatedAt.Equal(right.UpdatedAt) {
		return samePending(left.Pending, right.Pending)
	}
	return false
}

func cloneRecord(record Record) Record {
	if record.Pending == nil {
		return record
	}
	pending := *record.Pending
	record.Pending = &pending
	return record
}

func samePending(left, right *PendingTransition) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return strings.TrimSpace(left.DesiredAppID) == strings.TrimSpace(right.DesiredAppID) &&
		left.StartedAt.Equal(right.StartedAt)
}
