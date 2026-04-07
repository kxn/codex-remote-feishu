package feishu

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (p *DriveMarkdownPreviewer) CleanupBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	if p == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	if p.api == nil {
		return PreviewDriveCleanupResult{}, fmt.Errorf("preview drive api is not available")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadStateLocked()
	result := PreviewDriveCleanupResult{}
	keys := make([]string, 0, len(state.Files))
	for key := range state.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		record := state.Files[key]
		if record == nil {
			delete(state.Files, key)
			continue
		}
		lastUsedAt, ok := previewRecordLastUsedAt(record)
		if !ok {
			result.SkippedUnknownLastUsedCount++
			continue
		}
		if lastUsedAt.After(cutoff) {
			continue
		}
		if strings.TrimSpace(record.Token) != "" {
			err := p.api.DeleteFile(ctx, record.Token, previewFileType)
			if err != nil && !isPreviewResourceMissingError(err) {
				return PreviewDriveCleanupResult{}, err
			}
		}
		result.DeletedFileCount++
		if record.SizeBytes > 0 {
			result.DeletedEstimatedBytes += record.SizeBytes
		}
		delete(state.Files, key)
	}
	if err := p.saveStateLocked(); err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	result.Summary = summarizePreviewState(state, strings.TrimSpace(p.config.StatePath))
	return result, nil
}

func (p *DriveMarkdownPreviewer) Reconcile(ctx context.Context) (PreviewDriveReconcileResult, error) {
	if p == nil {
		return PreviewDriveReconcileResult{}, nil
	}
	if p.api == nil {
		return PreviewDriveReconcileResult{}, fmt.Errorf("preview drive api is not available")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadStateLocked()
	result := PreviewDriveReconcileResult{
		Summary: summarizePreviewState(state, strings.TrimSpace(p.config.StatePath)),
	}
	if state.Root == nil || strings.TrimSpace(state.Root.Token) == "" {
		return result, nil
	}

	rootChildren, err := p.api.ListFiles(ctx, state.Root.Token)
	switch {
	case err == nil:
	case isPreviewResourceMissingError(err):
		result.RootMissing = true
		result.RemoteMissingScopeCount = len(state.Scopes)
		result.RemoteMissingFileCount = len(state.Files)
		return result, nil
	default:
		return PreviewDriveReconcileResult{}, err
	}

	knownScopeTokens := map[string]string{}
	filesByScope := map[string]map[string]*previewFileRecord{}
	for scopeKey, scope := range state.Scopes {
		if scope == nil || scope.Folder == nil {
			continue
		}
		if token := strings.TrimSpace(scope.Folder.Token); token != "" {
			knownScopeTokens[token] = scopeKey
		}
	}
	for _, record := range state.Files {
		if record == nil {
			continue
		}
		scopeKey := strings.TrimSpace(record.ScopeKey)
		if scopeKey == "" {
			continue
		}
		if filesByScope[scopeKey] == nil {
			filesByScope[scopeKey] = map[string]*previewFileRecord{}
		}
		if token := strings.TrimSpace(record.Token); token != "" {
			filesByScope[scopeKey][token] = record
		}
	}

	rootFolders := map[string]previewRemoteNode{}
	for _, node := range rootChildren {
		switch strings.TrimSpace(node.Type) {
		case previewFolderType:
			rootFolders[node.Token] = node
			if knownScopeTokens[node.Token] == "" {
				result.LocalOnlyScopeCount++
			}
		default:
			result.LocalOnlyFileCount++
		}
	}

	for scopeKey, scope := range state.Scopes {
		if scope == nil || scope.Folder == nil {
			continue
		}
		scopeToken := strings.TrimSpace(scope.Folder.Token)
		if scopeToken == "" {
			result.RemoteMissingScopeCount++
			continue
		}
		if _, ok := rootFolders[scopeToken]; !ok {
			result.RemoteMissingScopeCount++
			continue
		}
		if len(scope.Folder.Shared) > 0 {
			drift, err := previewPermissionDrift(ctx, p.api, scopeToken, previewFolderType, scope.Folder.Shared)
			if err != nil {
				if isPreviewResourceMissingError(err) {
					result.RemoteMissingScopeCount++
					continue
				}
				return PreviewDriveReconcileResult{}, err
			}
			if drift {
				result.PermissionDriftCount++
			}
		}

		scopeChildren, err := p.api.ListFiles(ctx, scopeToken)
		if err != nil {
			if isPreviewResourceMissingError(err) {
				result.RemoteMissingScopeCount++
				continue
			}
			return PreviewDriveReconcileResult{}, err
		}
		expectedFiles := filesByScope[scopeKey]
		remoteFiles := map[string]previewRemoteNode{}
		for _, node := range scopeChildren {
			switch strings.TrimSpace(node.Type) {
			case previewFileType:
				remoteFiles[node.Token] = node
				if expectedFiles[node.Token] == nil {
					result.LocalOnlyFileCount++
				}
			case previewFolderType:
				result.LocalOnlyScopeCount++
			default:
				result.LocalOnlyFileCount++
			}
		}
		for token, record := range expectedFiles {
			if _, ok := remoteFiles[token]; !ok {
				result.RemoteMissingFileCount++
				continue
			}
			if len(record.Shared) == 0 {
				continue
			}
			drift, err := previewPermissionDrift(ctx, p.api, token, previewFileType, record.Shared)
			if err != nil {
				if isPreviewResourceMissingError(err) {
					result.RemoteMissingFileCount++
					continue
				}
				return PreviewDriveReconcileResult{}, err
			}
			if drift {
				result.PermissionDriftCount++
			}
		}
	}

	return result, nil
}
