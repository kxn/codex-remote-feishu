package feishu

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
)

type previewRewriteRuntime struct {
	dirty bool
}

func (p *DriveMarkdownPreviewer) nowUTC() time.Time {
	if p == nil || p.nowFn == nil {
		return time.Now().UTC()
	}
	return p.nowFn().UTC()
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
	if err := p.cleanupRemoteManagedFilesLocked(ctx, state, cutoff, &result); err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	result.Summary = summarizePreviewState(state, strings.TrimSpace(p.config.StatePath))
	return result, nil
}

func (p *DriveMarkdownPreviewer) RunBackgroundMaintenance(ctx context.Context) {
	if p == nil || p.api == nil || strings.TrimSpace(p.config.StatePath) == "" {
		return
	}

	ticker := time.NewTicker(p.config.BackgroundCleanupEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.runBackgroundCleanup(ctx); err != nil && ctx.Err() == nil {
				log.Printf("markdown preview background cleanup failed: gateway=%s err=%v", strings.TrimSpace(p.config.GatewayID), err)
			}
		}
	}
}

func (p *DriveMarkdownPreviewer) runBackgroundCleanup(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadStateLocked()
	now := p.nowUTC()
	if !state.LastCleanupAt.IsZero() && now.Before(state.LastCleanupAt.Add(p.config.BackgroundCleanupEvery)) {
		return nil
	}

	result, err := p.cleanupManagedPreviewFilesLocked(ctx, state, now.Add(-p.config.BackgroundCleanupMaxAge))
	if err != nil {
		return err
	}
	state.LastCleanupAt = now
	if err := p.saveStateLocked(); err != nil {
		return err
	}
	if result.DeletedFileCount > 0 {
		log.Printf(
			"markdown preview background cleanup: gateway=%s deleted=%d bytes=%d",
			strings.TrimSpace(p.config.GatewayID),
			result.DeletedFileCount,
			result.DeletedEstimatedBytes,
		)
	}
	return nil
}

func (p *DriveMarkdownPreviewer) discoverManagedRootLocked(ctx context.Context) (previewRemoteNode, bool, error) {
	rootFolders, err := p.api.ListFiles(ctx, "")
	if err != nil {
		return previewRemoteNode{}, false, err
	}

	candidates := []previewRemoteNode{}
	for _, node := range rootFolders {
		if node.Type == previewFolderType && strings.TrimSpace(node.Name) == defaultPreviewRootFolderName {
			candidates = append(candidates, node)
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

type previewInventorySnapshot struct {
	root    previewRemoteNode
	folders []previewRemoteNode
	files   []previewRemoteNode
}

func (p *DriveMarkdownPreviewer) loadManagedInventoryLocked(ctx context.Context, state *previewState) (previewInventorySnapshot, bool, error) {
	if p == nil || p.api == nil {
		return previewInventorySnapshot{}, false, nil
	}

	var root previewRemoteNode
	for attempt := 0; attempt < 2; attempt++ {
		root = previewRemoteNode{}
		if state != nil && state.Root != nil && strings.TrimSpace(state.Root.Token) != "" {
			root = previewRemoteNode{
				Token: strings.TrimSpace(state.Root.Token),
				URL:   strings.TrimSpace(state.Root.URL),
				Type:  previewFolderType,
				Name:  defaultPreviewRootFolderName,
			}
		} else {
			discovered, ok, err := p.discoverManagedRootLocked(ctx)
			if err != nil {
				return previewInventorySnapshot{}, false, fmt.Errorf("discover markdown preview root: %w", err)
			}
			if !ok {
				return previewInventorySnapshot{}, false, nil
			}
			root = discovered
			if state != nil {
				if state.Root == nil {
					state.Root = &previewFolderRecord{}
				}
				state.Root.Token = discovered.Token
				state.Root.URL = discovered.URL
			}
		}

		snapshot, err := p.walkManagedInventoryLocked(ctx, root)
		if err == nil {
			return snapshot, true, nil
		}
		if !isPreviewResourceMissingError(err) {
			return previewInventorySnapshot{}, false, err
		}
		if state != nil {
			state.Root = nil
		}
	}

	return previewInventorySnapshot{}, false, nil
}

func (p *DriveMarkdownPreviewer) walkManagedInventoryLocked(ctx context.Context, root previewRemoteNode) (previewInventorySnapshot, error) {
	snapshot := previewInventorySnapshot{root: root}
	queue := []previewRemoteNode{root}
	visited := map[string]bool{}
	if strings.TrimSpace(root.Token) != "" {
		visited[root.Token] = true
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		children, err := p.api.ListFiles(ctx, current.Token)
		if err != nil {
			return previewInventorySnapshot{}, fmt.Errorf("list markdown preview folder %s: %w", current.Token, err)
		}
		for _, child := range children {
			switch child.Type {
			case previewFolderType:
				if strings.TrimSpace(child.Token) == "" || visited[child.Token] {
					continue
				}
				visited[child.Token] = true
				snapshot.folders = append(snapshot.folders, child)
				queue = append(queue, child)
			case previewFileType:
				snapshot.files = append(snapshot.files, child)
			}
		}
	}

	return snapshot, nil
}

func (p *DriveMarkdownPreviewer) cleanupRemoteManagedFilesLocked(ctx context.Context, state *previewState, cutoff time.Time, result *PreviewDriveCleanupResult) error {
	if p == nil || p.api == nil || result == nil {
		return nil
	}

	snapshot, ok, err := p.loadManagedInventoryLocked(ctx, state)
	if err != nil {
		return err
	}
	if !ok {
		return nil
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

	if state != nil && state.Root != nil {
		state.Root.Token = snapshot.root.Token
		state.Root.URL = snapshot.root.URL
	}

	for _, node := range snapshot.files {
		if trackedTokens[node.Token] {
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

	return nil
}
