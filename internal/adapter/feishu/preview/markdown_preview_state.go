package preview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (p *DriveMarkdownPreviewer) loadStateLocked() *previewState {
	if p.loaded {
		return p.state
	}
	p.loaded = true
	p.state = newPreviewState()
	if strings.TrimSpace(p.config.StatePath) == "" {
		return p.state
	}

	raw, err := os.ReadFile(p.config.StatePath)
	if err != nil {
		return p.state
	}
	var loaded previewState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return p.state
	}
	p.state = normalizePreviewState(&loaded)
	return p.state
}

func (p *DriveMarkdownPreviewer) saveStateLocked() error {
	if strings.TrimSpace(p.config.StatePath) == "" || p.state == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.config.StatePath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p.state, "", "  ")
	if err != nil {
		return err
	}
	tempPath := p.config.StatePath + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, p.config.StatePath)
}

func newPreviewState() *previewState {
	return &previewState{
		Scopes: map[string]*previewScopeRecord{},
		Files:  map[string]*previewFileRecord{},
	}
}

func normalizePreviewState(state *previewState) *previewState {
	if state == nil {
		return newPreviewState()
	}
	if state.Scopes == nil {
		state.Scopes = map[string]*previewScopeRecord{}
	}
	if state.Files == nil {
		state.Files = map[string]*previewFileRecord{}
	}
	if state.Root != nil && state.Root.Shared == nil {
		state.Root.Shared = map[string]bool{}
	}
	for _, scope := range state.Scopes {
		if scope == nil || scope.Folder == nil {
			continue
		}
		if scope.Folder.Shared == nil {
			scope.Folder.Shared = map[string]bool{}
		}
	}
	for key, file := range state.Files {
		if file == nil {
			continue
		}
		if file.Shared == nil {
			file.Shared = map[string]bool{}
		}
		if file.ScopeKey == "" {
			file.ScopeKey = previewRecordScopeKey(key)
		}
	}
	return state
}

func summarizePreviewState(state *previewState, statePath string) PreviewDriveSummary {
	state = normalizePreviewState(state)
	summary := PreviewDriveSummary{
		StatePath:  statePath,
		FileCount:  len(state.Files),
		ScopeCount: len(state.Scopes),
	}
	if state.Root != nil {
		summary.RootToken = state.Root.Token
		summary.RootURL = state.Root.URL
	}
	for _, record := range state.Files {
		if record == nil {
			continue
		}
		if record.SizeBytes > 0 {
			summary.EstimatedBytes += record.SizeBytes
		} else {
			summary.UnknownSizeFileCount++
		}
		if lastUsedAt, ok := previewRecordLastUsedAt(record); ok {
			value := lastUsedAt.UTC()
			if summary.OldestLastUsedAt == nil || value.Before(*summary.OldestLastUsedAt) {
				copyValue := value
				summary.OldestLastUsedAt = &copyValue
			}
			if summary.NewestLastUsedAt == nil || value.After(*summary.NewestLastUsedAt) {
				copyValue := value
				summary.NewestLastUsedAt = &copyValue
			}
		}
	}
	return summary
}

func previewRecordLastUsedAt(record *previewFileRecord) (time.Time, bool) {
	if record == nil {
		return time.Time{}, false
	}
	switch {
	case !record.LastUsedAt.IsZero():
		return record.LastUsedAt.UTC(), true
	case !record.CreatedAt.IsZero():
		return record.CreatedAt.UTC(), true
	default:
		return time.Time{}, false
	}
}
