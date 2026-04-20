package preview

import (
	"context"
	"fmt"
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
	ctx, cancel := newFeishuTimeoutContext(ctx, previewDriveCleanupTimeout)
	defer cancel()

	result, err := p.cleanupManagedPreviewFiles(ctx, cutoff)
	if err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	summary, root, err := p.summarizeManagedInventory(ctx)
	if err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	p.stateMu.Lock()
	state := p.loadStateLocked()
	state.LastCleanupAt = p.nowUTC()
	if root != nil {
		if state.Root == nil {
			state.Root = &previewFolderRecord{}
		}
		state.Root.Token = root.Token
		state.Root.URL = root.URL
	} else {
		state.Root = nil
	}
	if err := p.saveStateLocked(); err != nil {
		p.stateMu.Unlock()
		return PreviewDriveCleanupResult{}, err
	}
	p.stateMu.Unlock()
	result.Summary = summary
	return result, nil
}

func (p *DriveMarkdownPreviewer) Summary(ctx context.Context) (PreviewDriveSummary, error) {
	if p == nil {
		return PreviewDriveSummary{}, nil
	}
	ctx, cancel := newFeishuTimeoutContext(ctx, previewDriveSummaryTimeout)
	defer cancel()

	p.stateMu.Lock()
	state := p.loadStateLocked()
	if p.api == nil {
		fallback := previewAdminFallbackSummary(state, strings.TrimSpace(p.config.StatePath), "api_unavailable", "当前还没有可用的飞书云盘预览配置。")
		p.stateMu.Unlock()
		return fallback, nil
	}
	beforeToken, beforeURL := previewRootSnapshot(state)
	p.stateMu.Unlock()

	summary, root, err := p.summarizeManagedInventory(ctx)
	if err != nil {
		if isPreviewDriveAccessDeniedError(err) {
			p.stateMu.Lock()
			fallback := previewAdminFallbackSummary(p.loadStateLocked(), strings.TrimSpace(p.config.StatePath), "permission_required", "当前机器人还没有开通飞书云盘权限。如需 Markdown 预览，请为应用开通 `drive:drive` 权限。")
			p.stateMu.Unlock()
			return fallback, nil
		}
		return PreviewDriveSummary{}, err
	}
	afterToken, afterURL := "", ""
	if root != nil {
		afterToken, afterURL = strings.TrimSpace(root.Token), strings.TrimSpace(root.URL)
	}
	if beforeToken != afterToken || beforeURL != afterURL {
		p.stateMu.Lock()
		state = p.loadStateLocked()
		if root != nil {
			if state.Root == nil {
				state.Root = &previewFolderRecord{}
			}
			state.Root.Token = root.Token
			state.Root.URL = root.URL
		} else {
			state.Root = nil
		}
		if err := p.saveStateLocked(); err != nil {
			p.stateMu.Unlock()
			return PreviewDriveSummary{}, err
		}
		p.stateMu.Unlock()
	}
	return summary, nil
}

func (p *DriveMarkdownPreviewer) summarizeManagedInventory(ctx context.Context) (PreviewDriveSummary, *previewFolderRecord, error) {
	summary := PreviewDriveSummary{
		StatePath: strings.TrimSpace(p.config.StatePath),
	}

	p.stateMu.Lock()
	state := p.loadStateLocked()
	rootHint := clonePreviewFolderRecord(state.Root)
	recordsByToken := map[string]*previewFileRecord{}
	for _, record := range state.Files {
		if record == nil {
			continue
		}
		token := strings.TrimSpace(record.Token)
		if token == "" {
			continue
		}
		recordsByToken[token] = clonePreviewFileRecord(record)
	}
	p.stateMu.Unlock()

	snapshot, ok, err := p.loadManagedInventory(ctx, rootHint)
	if err != nil {
		return PreviewDriveSummary{}, nil, err
	}
	if !ok {
		return summary, nil, nil
	}

	summary.RootToken = snapshot.root.Token
	summary.RootURL = snapshot.root.URL
	summary.FileCount = len(snapshot.files)
	summary.ScopeCount = len(snapshot.folders)

	for _, node := range snapshot.files {
		record := recordsByToken[node.Token]
		if record != nil && record.SizeBytes > 0 {
			summary.EstimatedBytes += record.SizeBytes
		} else {
			summary.UnknownSizeFileCount++
		}
		if value, ok := previewInventorySummaryTime(record, node); ok {
			updatePreviewSummaryWindow(&summary, value)
		}
	}

	return summary, &previewFolderRecord{
		Token: snapshot.root.Token,
		URL:   snapshot.root.URL,
	}, nil
}

func previewRootSnapshot(state *previewState) (string, string) {
	if state == nil || state.Root == nil {
		return "", ""
	}
	return strings.TrimSpace(state.Root.Token), strings.TrimSpace(state.Root.URL)
}

func previewInventorySummaryTime(record *previewFileRecord, node previewRemoteNode) (time.Time, bool) {
	if value, ok := previewRecordLastUsedAt(record); ok {
		return value, true
	}
	return previewRemoteCleanupTime(node)
}

func updatePreviewSummaryWindow(summary *PreviewDriveSummary, value time.Time) {
	if summary == nil || value.IsZero() {
		return
	}
	value = value.UTC()
	if summary.OldestLastUsedAt == nil || value.Before(*summary.OldestLastUsedAt) {
		copyValue := value
		summary.OldestLastUsedAt = &copyValue
	}
	if summary.NewestLastUsedAt == nil || value.After(*summary.NewestLastUsedAt) {
		copyValue := value
		summary.NewestLastUsedAt = &copyValue
	}
}

func previewAdminFallbackSummary(state *previewState, statePath, status, message string) PreviewDriveSummary {
	summary := summarizePreviewState(state, statePath)
	summary.Status = strings.TrimSpace(status)
	summary.StatusMessage = strings.TrimSpace(message)
	return summary
}
