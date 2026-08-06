// Package feishufacts persists the last observed Feishu bot facts per gateway:
// app name, bot open_id and configured scopes, together with fetch/error
// timestamps. It uses the shared JSON state store contract.
package feishufacts

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/statestore"
)

const (
	StateVersion  = 1
	StateFileName = "feishu-bot-facts.json"
)

type ScopeStatus struct {
	ScopeName   string `json:"scopeName"`
	ScopeType   string `json:"scopeType,omitempty"`
	GrantStatus int    `json:"grantStatus"`
}

type Record struct {
	GatewayID       string        `json:"gatewayID"`
	AppID           string        `json:"appID"`
	AppName         string        `json:"appName,omitempty"`
	BotOpenID       string        `json:"botOpenID,omitempty"`
	Scopes          []ScopeStatus `json:"scopes,omitempty"`
	ScopesFetchedAt time.Time     `json:"scopesFetchedAt,omitempty"`
	FetchedAt       time.Time     `json:"fetchedAt,omitempty"`
	BotError        string        `json:"botError,omitempty"`
	ScopesError     string        `json:"scopesError,omitempty"`
	LastError       string        `json:"lastError,omitempty"`
	LastErrorAt     time.Time     `json:"lastErrorAt,omitempty"`
}

func StatePath(stateDir string) string {
	return statestore.StatePath(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{Store: statestore.New[Record](path, options())}
}

func LoadStore(path string) (*Store, error) {
	store, err := statestore.Load[Record](path, options())
	if err != nil {
		return nil, err
	}
	return &Store{Store: store}, nil
}

type Store struct {
	*statestore.Store[Record]
}

func options() statestore.Options[Record] {
	return statestore.Options[Record]{
		Version:       StateVersion,
		Name:          "feishu bot facts",
		Equal:         sameRecord,
		Clone:         cloneRecord,
		LoadNormalize: func(record Record) (Record, bool) { return NormalizeRecord(record) },
		LoadKey:       func(record Record) string { return record.GatewayID },
		LoadEqual:     sameRecord,
	}
}

func NormalizeRecord(record Record) (Record, bool) {
	record.GatewayID = strings.TrimSpace(record.GatewayID)
	record.AppID = strings.TrimSpace(record.AppID)
	record.AppName = strings.TrimSpace(record.AppName)
	record.BotOpenID = strings.TrimSpace(record.BotOpenID)
	record.BotError = strings.TrimSpace(record.BotError)
	record.ScopesError = strings.TrimSpace(record.ScopesError)
	record.LastError = strings.TrimSpace(record.LastError)
	if record.GatewayID == "" || record.AppID == "" {
		return Record{}, false
	}
	if !record.FetchedAt.IsZero() {
		record.FetchedAt = record.FetchedAt.UTC()
	}
	if !record.ScopesFetchedAt.IsZero() {
		record.ScopesFetchedAt = record.ScopesFetchedAt.UTC()
	}
	if !record.LastErrorAt.IsZero() {
		record.LastErrorAt = record.LastErrorAt.UTC()
	}
	scopes := make([]ScopeStatus, 0, len(record.Scopes))
	for _, scope := range record.Scopes {
		scope.ScopeName = strings.TrimSpace(scope.ScopeName)
		scope.ScopeType = strings.TrimSpace(scope.ScopeType)
		if scope.ScopeName == "" {
			continue
		}
		scopes = append(scopes, scope)
	}
	record.Scopes = scopes
	return record, true
}

func (s *Store) Put(record Record) error {
	if s == nil {
		return nil
	}
	normalized, ok := NormalizeRecord(record)
	if !ok {
		return fmt.Errorf("feishu bot facts requires gateway id and app id")
	}
	entries := s.Entries()
	entries[normalized.GatewayID] = normalized
	return s.Replace(entries)
}

func (s *Store) Delete(gatewayID string) error {
	if s == nil {
		return nil
	}
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return nil
	}
	if _, ok := s.Get(gatewayID); !ok {
		return nil
	}
	entries := s.Entries()
	delete(entries, gatewayID)
	return s.Replace(entries)
}

func sameRecord(left, right Record) bool {
	if left.GatewayID != right.GatewayID ||
		left.AppID != right.AppID ||
		left.AppName != right.AppName ||
		left.BotOpenID != right.BotOpenID ||
		left.BotError != right.BotError ||
		left.ScopesError != right.ScopesError ||
		left.LastError != right.LastError ||
		!left.FetchedAt.Equal(right.FetchedAt) ||
		!left.ScopesFetchedAt.Equal(right.ScopesFetchedAt) ||
		!left.LastErrorAt.Equal(right.LastErrorAt) ||
		len(left.Scopes) != len(right.Scopes) {
		return false
	}
	for i := range left.Scopes {
		if left.Scopes[i] != right.Scopes[i] {
			return false
		}
	}
	return true
}

func cloneRecord(record Record) Record {
	record.Scopes = append([]ScopeStatus(nil), record.Scopes...)
	return record
}
