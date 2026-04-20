package preview

import (
	"strings"
	"time"
)

func (p *DriveMarkdownPreviewer) doPreviewOp(key string, fn func() (any, error)) (any, error) {
	if p == nil {
		return nil, nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fn()
	}

	p.inflightMu.Lock()
	if call := p.inflightOps[key]; call != nil {
		p.inflightMu.Unlock()
		<-call.done
		return call.value, call.err
	}
	if p.inflightOps == nil {
		p.inflightOps = map[string]*previewOpCall{}
	}
	call := &previewOpCall{done: make(chan struct{})}
	p.inflightOps[key] = call
	p.inflightMu.Unlock()

	call.value, call.err = fn()
	close(call.done)

	p.inflightMu.Lock()
	delete(p.inflightOps, key)
	p.inflightMu.Unlock()
	return call.value, call.err
}

func markPreviewRuntimeDirty(runtime *previewRewriteRuntime) {
	if runtime != nil {
		runtime.dirty = true
	}
}

func clonePreviewShared(shared map[string]bool) map[string]bool {
	if len(shared) == 0 {
		return map[string]bool{}
	}
	cloned := make(map[string]bool, len(shared))
	for key, value := range shared {
		cloned[key] = value
	}
	return cloned
}

func clonePreviewFolderRecord(record *previewFolderRecord) *previewFolderRecord {
	if record == nil {
		return nil
	}
	return &previewFolderRecord{
		Token:            record.Token,
		URL:              record.URL,
		Shared:           clonePreviewShared(record.Shared),
		LastReconciledAt: record.LastReconciledAt,
	}
}

func clonePreviewFileRecord(record *previewFileRecord) *previewFileRecord {
	if record == nil {
		return nil
	}
	return &previewFileRecord{
		Path:       record.Path,
		SHA256:     record.SHA256,
		Token:      record.Token,
		URL:        record.URL,
		Shared:     clonePreviewShared(record.Shared),
		ScopeKey:   record.ScopeKey,
		SizeBytes:  record.SizeBytes,
		CreatedAt:  record.CreatedAt,
		LastUsedAt: record.LastUsedAt,
	}
}

func previewSharedCoversPrincipals(shared map[string]bool, principals []previewPrincipal) bool {
	if len(principals) == 0 {
		return true
	}
	for _, principal := range principals {
		if strings.TrimSpace(principal.Key) == "" {
			continue
		}
		if !shared[principal.Key] {
			return false
		}
	}
	return true
}

func ensurePreviewScopeRecordLocked(state *previewState, scopeKey string) *previewScopeRecord {
	if state.Scopes == nil {
		state.Scopes = map[string]*previewScopeRecord{}
	}
	scope := state.Scopes[scopeKey]
	if scope == nil {
		scope = &previewScopeRecord{}
		state.Scopes[scopeKey] = scope
	}
	return scope
}

func (p *DriveMarkdownPreviewer) snapshotPreviewRoot() *previewFolderRecord {
	if p == nil {
		return nil
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.loadStateLocked()
	return clonePreviewFolderRecord(state.Root)
}

func (p *DriveMarkdownPreviewer) storePreviewRoot(record *previewFolderRecord, runtime *previewRewriteRuntime) {
	if p == nil || record == nil {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.loadStateLocked()
	state.Root = clonePreviewFolderRecord(record)
	markPreviewRuntimeDirty(runtime)
}

func (p *DriveMarkdownPreviewer) clearPreviewRoot(runtime *previewRewriteRuntime) {
	if p == nil {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.loadStateLocked()
	if state.Root == nil {
		return
	}
	state.Root = nil
	markPreviewRuntimeDirty(runtime)
}

func (p *DriveMarkdownPreviewer) snapshotPreviewScopeFolder(scopeKey string) *previewFolderRecord {
	if p == nil {
		return nil
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.loadStateLocked()
	scope := state.Scopes[scopeKey]
	if scope == nil {
		return nil
	}
	return clonePreviewFolderRecord(scope.Folder)
}

func (p *DriveMarkdownPreviewer) storePreviewScopeFolder(scopeKey string, record *previewFolderRecord, runtime *previewRewriteRuntime) {
	if p == nil || strings.TrimSpace(scopeKey) == "" || record == nil {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.loadStateLocked()
	scope := ensurePreviewScopeRecordLocked(state, scopeKey)
	scope.Folder = clonePreviewFolderRecord(record)
	markPreviewRuntimeDirty(runtime)
}

func (p *DriveMarkdownPreviewer) clearPreviewScopeState(scopeKey string, runtime *previewRewriteRuntime) {
	if p == nil || strings.TrimSpace(scopeKey) == "" {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	state := p.loadStateLocked()
	clearPreviewScope(state, scopeKey)
	markPreviewRuntimeDirty(runtime)
}

func (p *DriveMarkdownPreviewer) markDriveFileUsedIfReady(fileKey, scopeKey string, sizeBytes int64, principals []previewPrincipal, now time.Time, runtime *previewRewriteRuntime) (*previewFileRecord, bool) {
	if p == nil {
		return nil, false
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	state := p.loadStateLocked()
	record := state.Files[fileKey]
	if record == nil ||
		strings.TrimSpace(record.Token) == "" ||
		strings.TrimSpace(record.URL) == "" ||
		!previewSharedCoversPrincipals(record.Shared, principals) {
		return nil, false
	}

	if record.ScopeKey == "" {
		record.ScopeKey = scopeKey
		markPreviewRuntimeDirty(runtime)
	}
	if sizeBytes > 0 && record.SizeBytes <= 0 {
		record.SizeBytes = sizeBytes
		markPreviewRuntimeDirty(runtime)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
		markPreviewRuntimeDirty(runtime)
	}
	record.LastUsedAt = now
	scope := ensurePreviewScopeRecordLocked(state, scopeKey)
	scope.LastUsedAt = now
	markPreviewRuntimeDirty(runtime)
	return clonePreviewFileRecord(record), true
}

func (p *DriveMarkdownPreviewer) snapshotDriveFileRecord(fileKey, scopeKey string, artifact PreparedPreviewArtifact, now time.Time) *previewFileRecord {
	if p == nil {
		return nil
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	state := p.loadStateLocked()
	record := clonePreviewFileRecord(state.Files[fileKey])
	if record == nil {
		record = &previewFileRecord{}
	}
	if strings.TrimSpace(record.Path) == "" {
		record.Path = strings.TrimSpace(artifact.SourcePath)
	}
	if strings.TrimSpace(record.SHA256) == "" {
		record.SHA256 = strings.TrimSpace(artifact.ContentHash)
	}
	if strings.TrimSpace(record.ScopeKey) == "" {
		record.ScopeKey = strings.TrimSpace(scopeKey)
	}
	if record.SizeBytes <= 0 && len(artifact.Bytes) > 0 {
		record.SizeBytes = int64(len(artifact.Bytes))
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.Shared == nil {
		record.Shared = map[string]bool{}
	}
	return record
}

func (p *DriveMarkdownPreviewer) commitDriveFileRecord(fileKey, scopeKey string, record *previewFileRecord, now time.Time, runtime *previewRewriteRuntime) {
	if p == nil || strings.TrimSpace(fileKey) == "" || record == nil {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	state := p.loadStateLocked()
	current := state.Files[fileKey]
	merged := clonePreviewFileRecord(record)
	if merged.Shared == nil {
		merged.Shared = map[string]bool{}
	}
	if current != nil {
		if strings.TrimSpace(merged.Path) == "" {
			merged.Path = current.Path
		}
		if strings.TrimSpace(merged.SHA256) == "" {
			merged.SHA256 = current.SHA256
		}
		if strings.TrimSpace(merged.ScopeKey) == "" {
			merged.ScopeKey = current.ScopeKey
		}
		if merged.SizeBytes <= 0 {
			merged.SizeBytes = current.SizeBytes
		}
		if merged.CreatedAt.IsZero() {
			merged.CreatedAt = current.CreatedAt
		}
		for key, value := range current.Shared {
			if value {
				merged.Shared[key] = true
			}
		}
	}
	if merged.CreatedAt.IsZero() {
		merged.CreatedAt = now
	}
	merged.LastUsedAt = now
	state.Files[fileKey] = merged
	scope := ensurePreviewScopeRecordLocked(state, scopeKey)
	scope.LastUsedAt = now
	markPreviewRuntimeDirty(runtime)
}

func (p *DriveMarkdownPreviewer) resetDriveFileRecord(fileKey string, runtime *previewRewriteRuntime) {
	if p == nil || strings.TrimSpace(fileKey) == "" {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	state := p.loadStateLocked()
	record := state.Files[fileKey]
	if record == nil {
		return
	}
	record.Token = ""
	record.URL = ""
	record.Shared = map[string]bool{}
	markPreviewRuntimeDirty(runtime)
}
