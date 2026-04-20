package preview

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type webPreviewBlobInfo struct {
	BlobKey    string
	Path       string
	SizeBytes  int64
	LastUsedAt time.Time
}

type webPreviewEvictionCandidate struct {
	ScopePublicID string
	PreviewID     string
	BlobKey       string
	LastUsedAt    time.Time
	CreatedAt     time.Time
}

func (p *DriveMarkdownPreviewer) cleanupWebPreviewCacheLocked(now time.Time) error {
	if p == nil || strings.TrimSpace(p.config.CacheDir) == "" {
		return nil
	}
	if now.IsZero() {
		now = p.nowUTC()
	}

	manifests, dirtyScopes, err := p.loadActiveWebPreviewManifestsLocked(now)
	if err != nil {
		return err
	}
	blobInfos, err := p.listPreviewBlobInfos()
	if err != nil {
		return err
	}

	refCounts := map[string]int{}
	evictionCandidates := make([]webPreviewEvictionCandidate, 0, len(blobInfos))
	for scopePublicID, manifest := range manifests {
		for previewID, record := range manifest.Records {
			if record == nil || strings.TrimSpace(record.BlobKey) == "" {
				continue
			}
			refCounts[record.BlobKey]++
			evictionCandidates = append(evictionCandidates, webPreviewEvictionCandidate{
				ScopePublicID: scopePublicID,
				PreviewID:     previewID,
				BlobKey:       record.BlobKey,
				LastUsedAt:    previewRecordUsageTime(record),
				CreatedAt:     record.CreatedAt.UTC(),
			})
		}
	}

	totalBytes := int64(0)
	unreferenced := make([]webPreviewBlobInfo, 0)
	ttlCutoff := now.Add(-defaultPreviewBlobTTL)
	for _, info := range blobInfos {
		totalBytes += info.SizeBytes
		if refCounts[info.BlobKey] == 0 {
			unreferenced = append(unreferenced, info)
		}
	}

	sort.Slice(unreferenced, func(i, j int) bool {
		if unreferenced[i].LastUsedAt.Equal(unreferenced[j].LastUsedAt) {
			return unreferenced[i].BlobKey < unreferenced[j].BlobKey
		}
		return unreferenced[i].LastUsedAt.Before(unreferenced[j].LastUsedAt)
	})
	for _, info := range unreferenced {
		if !info.LastUsedAt.IsZero() && info.LastUsedAt.After(ttlCutoff) {
			continue
		}
		if err := removePreviewFileIfExists(info.Path); err != nil {
			return err
		}
		totalBytes -= info.SizeBytes
		delete(blobInfos, info.BlobKey)
	}

	if totalBytes > defaultPreviewCacheBudgetBytes {
		for _, info := range unreferenced {
			if totalBytes <= defaultPreviewCacheBudgetBytes {
				break
			}
			current, ok := blobInfos[info.BlobKey]
			if !ok {
				continue
			}
			if err := removePreviewFileIfExists(current.Path); err != nil {
				return err
			}
			totalBytes -= current.SizeBytes
			delete(blobInfos, info.BlobKey)
		}
	}

	if totalBytes > defaultPreviewCacheBudgetBytes {
		sort.Slice(evictionCandidates, func(i, j int) bool {
			if evictionCandidates[i].LastUsedAt.Equal(evictionCandidates[j].LastUsedAt) {
				if evictionCandidates[i].CreatedAt.Equal(evictionCandidates[j].CreatedAt) {
					if evictionCandidates[i].ScopePublicID == evictionCandidates[j].ScopePublicID {
						return evictionCandidates[i].PreviewID < evictionCandidates[j].PreviewID
					}
					return evictionCandidates[i].ScopePublicID < evictionCandidates[j].ScopePublicID
				}
				return evictionCandidates[i].CreatedAt.Before(evictionCandidates[j].CreatedAt)
			}
			return evictionCandidates[i].LastUsedAt.Before(evictionCandidates[j].LastUsedAt)
		})
		for _, candidate := range evictionCandidates {
			if totalBytes <= defaultPreviewCacheBudgetBytes {
				break
			}
			manifest := manifests[candidate.ScopePublicID]
			if manifest == nil {
				continue
			}
			record := manifest.Records[candidate.PreviewID]
			if record == nil {
				continue
			}
			delete(manifest.Records, candidate.PreviewID)
			dirtyScopes[candidate.ScopePublicID] = true
			if len(manifest.Records) == 0 {
				delete(manifests, candidate.ScopePublicID)
			}
			refCounts[candidate.BlobKey]--
			if refCounts[candidate.BlobKey] > 0 {
				continue
			}
			info, ok := blobInfos[candidate.BlobKey]
			if !ok {
				continue
			}
			if err := removePreviewFileIfExists(info.Path); err != nil {
				return err
			}
			totalBytes -= info.SizeBytes
			delete(blobInfos, candidate.BlobKey)
		}
	}

	for scopePublicID := range dirtyScopes {
		manifest := manifests[scopePublicID]
		if manifest == nil || len(manifest.Records) == 0 {
			if err := removePreviewFileIfExists(p.previewScopeManifestPath(scopePublicID)); err != nil {
				return err
			}
			continue
		}
		syncWebPreviewManifestLastUsedAt(manifest)
		if err := p.saveWebPreviewScopeManifest(manifest); err != nil {
			return err
		}
	}
	return nil
}

func (p *DriveMarkdownPreviewer) loadActiveWebPreviewManifestsLocked(now time.Time) (map[string]*webPreviewScopeManifest, map[string]bool, error) {
	paths, err := p.listPreviewScopeManifestPaths()
	if err != nil {
		return nil, nil, err
	}
	manifests := map[string]*webPreviewScopeManifest{}
	dirtyScopes := map[string]bool{}
	for _, manifestPath := range paths {
		scopePublicID := strings.TrimSuffix(filepath.Base(manifestPath), filepath.Ext(manifestPath))
		manifest, err := p.loadWebPreviewScopeManifest(scopePublicID)
		if err != nil {
			return nil, nil, err
		}
		if manifest == nil {
			continue
		}
		changed := false
		for previewID, record := range manifest.Records {
			if record == nil {
				delete(manifest.Records, previewID)
				changed = true
				continue
			}
			if !record.ExpiresAt.IsZero() && record.ExpiresAt.Before(now) {
				delete(manifest.Records, previewID)
				changed = true
			}
		}
		if changed {
			dirtyScopes[scopePublicID] = true
		}
		if len(manifest.Records) == 0 {
			dirtyScopes[scopePublicID] = true
			continue
		}
		manifests[scopePublicID] = manifest
	}
	return manifests, dirtyScopes, nil
}

func (p *DriveMarkdownPreviewer) listPreviewScopeManifestPaths() ([]string, error) {
	scopeDir := filepath.Join(p.config.CacheDir, "scopes")
	entries, err := os.ReadDir(scopeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isPreviewManifestFile(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(scopeDir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

func (p *DriveMarkdownPreviewer) listPreviewBlobInfos() (map[string]webPreviewBlobInfo, error) {
	root := filepath.Join(p.config.CacheDir, "blobs", "sha256")
	infos := map[string]webPreviewBlobInfo{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d == nil || d.IsDir() || filepath.Ext(path) != ".bin" {
			return nil
		}
		blobKey := strings.TrimSuffix(filepath.Base(path), ".bin")
		if strings.TrimSpace(blobKey) == "" {
			return nil
		}
		infos[blobKey] = webPreviewBlobInfo{
			BlobKey:    blobKey,
			Path:       path,
			SizeBytes:  blobSize(path),
			LastUsedAt: blobModTime(path),
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return infos, nil
}

func syncWebPreviewManifestLastUsedAt(manifest *webPreviewScopeManifest) {
	if manifest == nil {
		return
	}
	manifest.LastUsedAt = time.Time{}
	for _, record := range manifest.Records {
		lastUsedAt := previewRecordUsageTime(record)
		if manifest.LastUsedAt.IsZero() || lastUsedAt.After(manifest.LastUsedAt) {
			manifest.LastUsedAt = lastUsedAt
		}
	}
}

func previewRecordUsageTime(record *webPreviewRecord) time.Time {
	if record == nil {
		return time.Time{}
	}
	switch {
	case !record.LastUsedAt.IsZero():
		return record.LastUsedAt.UTC()
	case !record.CreatedAt.IsZero():
		return record.CreatedAt.UTC()
	default:
		return time.Time{}
	}
}

func removePreviewFileIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
