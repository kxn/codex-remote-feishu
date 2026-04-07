package feishu

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type previewRewriteRuntime struct {
	dirty              bool
	lazyCleanupChecked bool
}

func (p *DriveMarkdownPreviewer) nowUTC() time.Time {
	if p == nil || p.nowFn == nil {
		return time.Now().UTC()
	}
	return p.nowFn().UTC()
}

func previewRootMarkerFolderName(gatewayID string) string {
	value := strings.NewReplacer(":", "-", "/", "-", "\\", "-", " ", "-").Replace(normalizeGatewayID(gatewayID))
	value = strings.Trim(value, "-")
	if value == "" {
		value = normalizeGatewayID("")
	}
	return limitNameBytes(previewRootMarkerPrefix+value, 180)
}

func previewManagedFileName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), previewManagedFilePrefix)
}

func previewRemoteCleanupTime(node previewRemoteNode) (time.Time, bool) {
	switch {
	case !node.CreatedTime.IsZero():
		return node.CreatedTime.UTC(), true
	case !node.ModifiedTime.IsZero():
		return node.ModifiedTime.UTC(), true
	default:
		return time.Time{}, false
	}
}

func parsePreviewRemoteTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case value > 1_000_000_000_000:
			return time.UnixMilli(value).UTC()
		case value > 0:
			return time.Unix(value, 0).UTC()
		default:
			return time.Time{}
		}
	}
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return value.UTC()
	}
	return time.Time{}
}

func (p *DriveMarkdownPreviewer) maybeLazyCleanupBeforeUploadLocked(ctx context.Context, state *previewState, runtime *previewRewriteRuntime) error {
	if runtime == nil || runtime.lazyCleanupChecked {
		return nil
	}
	runtime.lazyCleanupChecked = true

	now := p.nowUTC()
	if !state.LastCleanupAt.IsZero() && now.Before(state.LastCleanupAt.Add(defaultPreviewLazyCleanupGap)) {
		return nil
	}

	if _, err := p.cleanupManagedPreviewFilesLocked(ctx, state, now.Add(-defaultPreviewLazyCleanupAge)); err != nil {
		return err
	}
	state.LastCleanupAt = now
	runtime.dirty = true
	return nil
}

func (p *DriveMarkdownPreviewer) cleanupManagedPreviewFilesLocked(ctx context.Context, state *previewState, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	state = normalizePreviewState(state)
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

	if err := p.ensureKnownRootMarkerLocked(ctx, state); err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	if err := p.cleanupRemoteManagedFilesLocked(ctx, state, cutoff, &result); err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	result.Summary = summarizePreviewState(state, strings.TrimSpace(p.config.StatePath))
	return result, nil
}

func (p *DriveMarkdownPreviewer) ensureKnownRootMarkerLocked(ctx context.Context, state *previewState) error {
	if p == nil || p.api == nil || state == nil || state.Root == nil || strings.TrimSpace(state.Root.Token) == "" || state.Root.MarkerReady {
		return nil
	}
	if err := p.ensureRootMarkerLocked(ctx, state.Root.Token); err != nil {
		if isPreviewResourceMissingError(err) {
			state.Root = nil
			return nil
		}
		return fmt.Errorf("ensure markdown preview root marker: %w", err)
	}
	state.Root.MarkerReady = true
	return nil
}

func (p *DriveMarkdownPreviewer) ensureRootMarkerLocked(ctx context.Context, rootToken string) error {
	rootToken = strings.TrimSpace(rootToken)
	if rootToken == "" {
		return fmt.Errorf("missing markdown preview root token")
	}
	children, err := p.api.ListFiles(ctx, rootToken)
	if err != nil {
		return err
	}
	markerName := previewRootMarkerFolderName(p.config.GatewayID)
	for _, child := range children {
		if child.Type == previewFolderType && strings.TrimSpace(child.Name) == markerName {
			return nil
		}
	}
	_, err = p.api.CreateFolder(ctx, markerName, rootToken)
	return err
}

func (p *DriveMarkdownPreviewer) discoverManagedRootLocked(ctx context.Context) (previewRemoteNode, bool, error) {
	rootFolders, err := p.api.ListFiles(ctx, "")
	if err != nil {
		return previewRemoteNode{}, false, err
	}

	markerName := previewRootMarkerFolderName(p.config.GatewayID)
	rootName := strings.TrimSpace(p.config.RootFolderName)
	candidates := []previewRemoteNode{}
	for _, node := range rootFolders {
		if node.Type != previewFolderType || strings.TrimSpace(node.Name) != rootName {
			continue
		}
		children, err := p.api.ListFiles(ctx, node.Token)
		if err != nil {
			if isPreviewResourceMissingError(err) {
				continue
			}
			return previewRemoteNode{}, false, fmt.Errorf("list markdown preview root candidate %s: %w", node.Token, err)
		}
		for _, child := range children {
			if child.Type == previewFolderType && strings.TrimSpace(child.Name) == markerName {
				candidates = append(candidates, node)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return previewRemoteNode{}, false, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		switch {
		case left.CreatedTime.IsZero() && right.CreatedTime.IsZero():
			return left.Token < right.Token
		case left.CreatedTime.IsZero():
			return false
		case right.CreatedTime.IsZero():
			return true
		case left.CreatedTime.Equal(right.CreatedTime):
			return left.Token < right.Token
		default:
			return left.CreatedTime.Before(right.CreatedTime)
		}
	})
	return candidates[0], true, nil
}

func (p *DriveMarkdownPreviewer) cleanupRemoteManagedFilesLocked(ctx context.Context, state *previewState, cutoff time.Time, result *PreviewDriveCleanupResult) error {
	if p == nil || p.api == nil {
		return nil
	}

	var root previewRemoteNode
	switch {
	case state != nil && state.Root != nil && strings.TrimSpace(state.Root.Token) != "":
		root = previewRemoteNode{
			Token: state.Root.Token,
			URL:   state.Root.URL,
			Type:  previewFolderType,
			Name:  p.config.RootFolderName,
		}
	case true:
		discovered, ok, err := p.discoverManagedRootLocked(ctx)
		if err != nil {
			return fmt.Errorf("discover markdown preview root: %w", err)
		}
		if !ok {
			return nil
		}
		root = discovered
		if state != nil {
			if state.Root == nil {
				state.Root = &previewFolderRecord{}
			}
			state.Root.Token = discovered.Token
			state.Root.URL = discovered.URL
			state.Root.MarkerReady = true
		}
	}

	trackedTokens := map[string]bool{}
	if state != nil {
		for _, record := range state.Files {
			if record == nil || strings.TrimSpace(record.Token) == "" {
				continue
			}
			trackedTokens[record.Token] = true
		}
	}

	rootChildren, err := p.api.ListFiles(ctx, root.Token)
	if err != nil {
		if isPreviewResourceMissingError(err) {
			if state != nil {
				state.Root = nil
			}
			return nil
		}
		return fmt.Errorf("list markdown preview root children: %w", err)
	}

	markerName := previewRootMarkerFolderName(p.config.GatewayID)
	for _, child := range rootChildren {
		if child.Type != previewFolderType || strings.TrimSpace(child.Name) == markerName {
			continue
		}

		scopeChildren, err := p.api.ListFiles(ctx, child.Token)
		if err != nil {
			if isPreviewResourceMissingError(err) {
				continue
			}
			return fmt.Errorf("list markdown preview scope children: %w", err)
		}
		for _, node := range scopeChildren {
			if node.Type != previewFileType || trackedTokens[node.Token] || !previewManagedFileName(node.Name) {
				continue
			}
			createdAt, ok := previewRemoteCleanupTime(node)
			if !ok || createdAt.After(cutoff) {
				continue
			}
			err := p.api.DeleteFile(ctx, node.Token, previewFileType)
			if err != nil && !isPreviewResourceMissingError(err) {
				return fmt.Errorf("delete markdown preview file %s: %w", node.Token, err)
			}
			result.DeletedFileCount++
		}
	}

	return nil
}
