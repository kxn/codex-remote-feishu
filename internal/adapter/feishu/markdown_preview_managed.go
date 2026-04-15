package feishu

import (
	"context"
	"errors"
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

type previewManagedCleanupCandidate struct {
	Key       string
	Token     string
	SizeBytes int64
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

func (p *DriveMarkdownPreviewer) cleanupManagedPreviewFiles(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	candidates, trackedTokens, skippedUnknown, rootHint := p.snapshotManagedCleanupState(cutoff)
	result := PreviewDriveCleanupResult{
		SkippedUnknownLastUsedCount: skippedUnknown,
	}

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Token) != "" {
			err := p.api.DeleteFile(ctx, candidate.Token, previewFileType)
			if err != nil && !isPreviewResourceMissingError(err) {
				return PreviewDriveCleanupResult{}, err
			}
		}
		result.DeletedFileCount++
		if candidate.SizeBytes > 0 {
			result.DeletedEstimatedBytes += candidate.SizeBytes
		}
	}

	snapshot, ok, err := p.loadManagedInventory(ctx, rootHint)
	if err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	if ok {
		if err := p.cleanupRemoteManagedFiles(ctx, trackedTokens, cutoff, snapshot, &result); err != nil {
			return PreviewDriveCleanupResult{}, err
		}
	}

	p.stateMu.Lock()
	state := p.loadStateLocked()
	for _, candidate := range candidates {
		record := state.Files[candidate.Key]
		if record == nil {
			delete(state.Files, candidate.Key)
			continue
		}
		if strings.TrimSpace(candidate.Token) == "" || strings.TrimSpace(record.Token) == strings.TrimSpace(candidate.Token) {
			delete(state.Files, candidate.Key)
		}
	}
	if ok {
		if state.Root == nil {
			state.Root = &previewFolderRecord{}
		}
		state.Root.Token = snapshot.root.Token
		state.Root.URL = snapshot.root.URL
	}
	result.Summary = summarizePreviewState(state, strings.TrimSpace(p.config.StatePath))
	p.stateMu.Unlock()

	return result, nil
}

func (p *DriveMarkdownPreviewer) snapshotManagedCleanupState(cutoff time.Time) ([]previewManagedCleanupCandidate, map[string]bool, int, *previewFolderRecord) {
	if p == nil {
		return nil, nil, 0, nil
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	state := p.loadStateLocked()
	rootHint := clonePreviewFolderRecord(state.Root)
	keys := make([]string, 0, len(state.Files))
	for key := range state.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	candidates := make([]previewManagedCleanupCandidate, 0)
	trackedTokens := map[string]bool{}
	skippedUnknown := 0
	for _, key := range keys {
		record := state.Files[key]
		if record == nil {
			delete(state.Files, key)
			continue
		}
		lastUsedAt, ok := previewRecordLastUsedAt(record)
		switch {
		case !ok:
			skippedUnknown++
			if strings.TrimSpace(record.Token) != "" {
				trackedTokens[record.Token] = true
			}
		case lastUsedAt.After(cutoff):
			if strings.TrimSpace(record.Token) != "" {
				trackedTokens[record.Token] = true
			}
		default:
			candidates = append(candidates, previewManagedCleanupCandidate{
				Key:       key,
				Token:     strings.TrimSpace(record.Token),
				SizeBytes: record.SizeBytes,
			})
		}
	}
	return candidates, trackedTokens, skippedUnknown, rootHint
}

func (p *DriveMarkdownPreviewer) RunBackgroundMaintenance(ctx context.Context) {
	if p == nil {
		return
	}
	hasDriveCleanup := p.api != nil && strings.TrimSpace(p.config.StatePath) != ""
	hasWebPreviewCleanup := strings.TrimSpace(p.config.CacheDir) != ""
	if !hasDriveCleanup && !hasWebPreviewCleanup {
		return
	}

	ticker := time.NewTicker(p.config.BackgroundCleanupEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupCtx, cancel := newFeishuTimeoutContext(ctx, previewDriveBackgroundCleanupTimeout)
			err := p.runBackgroundCleanup(cleanupCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Printf("markdown preview background cleanup failed: gateway=%s err=%v", strings.TrimSpace(p.config.GatewayID), err)
			}
		}
	}
}

func (p *DriveMarkdownPreviewer) runBackgroundCleanup(ctx context.Context) error {
	p.maintenanceMu.Lock()
	defer p.maintenanceMu.Unlock()

	now := p.nowUTC()
	var combinedErr error
	if p.api != nil && strings.TrimSpace(p.config.StatePath) != "" {
		p.stateMu.Lock()
		state := p.loadStateLocked()
		shouldRunDriveCleanup := state.LastCleanupAt.IsZero() || now.After(state.LastCleanupAt.Add(p.config.BackgroundCleanupEvery))
		p.stateMu.Unlock()

		if shouldRunDriveCleanup {
			result, err := p.cleanupManagedPreviewFiles(ctx, now.Add(-p.config.BackgroundCleanupMaxAge))
			if err == nil {
				p.stateMu.Lock()
				state = p.loadStateLocked()
				state.LastCleanupAt = now
				if saveErr := p.saveStateLocked(); saveErr != nil {
					err = saveErr
				}
				p.stateMu.Unlock()
			}
			if err == nil && result.DeletedFileCount > 0 {
				log.Printf(
					"markdown preview background cleanup: gateway=%s deleted=%d bytes=%d",
					strings.TrimSpace(p.config.GatewayID),
					result.DeletedFileCount,
					result.DeletedEstimatedBytes,
				)
			}
			if err != nil {
				combinedErr = joinPreviewMaintenanceError(combinedErr, "drive cleanup", err)
			}
		}
	}
	if strings.TrimSpace(p.config.CacheDir) != "" {
		p.webPreviewMu.Lock()
		err := p.cleanupWebPreviewCacheLocked(now)
		p.webPreviewMu.Unlock()
		if err != nil {
			combinedErr = joinPreviewMaintenanceError(combinedErr, "web preview cleanup", err)
		}
	}
	return combinedErr
}

func joinPreviewMaintenanceError(current error, label string, err error) error {
	if err == nil {
		return current
	}
	wrapped := fmt.Errorf("%s failed: %w", strings.TrimSpace(label), err)
	if current == nil {
		return wrapped
	}
	return errors.Join(current, wrapped)
}

func (p *DriveMarkdownPreviewer) discoverManagedRoot(ctx context.Context) (previewRemoteNode, bool, error) {
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

func (p *DriveMarkdownPreviewer) loadManagedInventory(ctx context.Context, rootHint *previewFolderRecord) (previewInventorySnapshot, bool, error) {
	if p == nil || p.api == nil {
		return previewInventorySnapshot{}, false, nil
	}

	currentRoot := clonePreviewFolderRecord(rootHint)
	for attempt := 0; attempt < 2; attempt++ {
		root := previewRemoteNode{}
		if currentRoot != nil && strings.TrimSpace(currentRoot.Token) != "" {
			root = previewRemoteNode{
				Token: strings.TrimSpace(currentRoot.Token),
				URL:   strings.TrimSpace(currentRoot.URL),
				Type:  previewFolderType,
				Name:  defaultPreviewRootFolderName,
			}
		} else {
			discovered, ok, err := p.discoverManagedRoot(ctx)
			if err != nil {
				return previewInventorySnapshot{}, false, fmt.Errorf("discover markdown preview root: %w", err)
			}
			if !ok {
				return previewInventorySnapshot{}, false, nil
			}
			root = discovered
			currentRoot = &previewFolderRecord{
				Token: discovered.Token,
				URL:   discovered.URL,
			}
		}

		snapshot, err := p.walkManagedInventory(ctx, root)
		if err == nil {
			return snapshot, true, nil
		}
		if !isPreviewResourceMissingError(err) {
			return previewInventorySnapshot{}, false, err
		}
		currentRoot = nil
	}

	return previewInventorySnapshot{}, false, nil
}

func (p *DriveMarkdownPreviewer) walkManagedInventory(ctx context.Context, root previewRemoteNode) (previewInventorySnapshot, error) {
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

func (p *DriveMarkdownPreviewer) cleanupRemoteManagedFiles(ctx context.Context, trackedTokens map[string]bool, cutoff time.Time, snapshot previewInventorySnapshot, result *PreviewDriveCleanupResult) error {
	if p == nil || p.api == nil || result == nil {
		return nil
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
