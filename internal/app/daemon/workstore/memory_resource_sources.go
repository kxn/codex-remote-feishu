package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"strconv"
	"strings"
)

// A later capture supersedes only the same observed source and range. Saved
// historical versions remain readable; unrelated partial ranges never collide.
func (t *Tx) invalidateSupersededResourceMemory(current work.ResourceVersion) error {
	if current.Kind == work.VersionCurrentReference || !current.Saved {
		return nil
	}
	aliases, err := t.projectFeishuResourceSources(current.ScopeID)
	if err != nil {
		return err
	}
	var previous []work.ResourceVersion
	if aliases[current.ResourceID] != "" {
		previous, err = listRecords[work.ResourceVersion](t, "resource_versions",
			" WHERE scope_id=? AND json_extract(payload,'$.source.kind')=? AND json_extract(payload,'$.source.id')=?",
			current.ScopeID, current.Source.Kind, current.Source.ID)
	} else {
		previous, err = t.ListResourceVersions(current.ScopeID, current.ResourceID)
	}
	if err != nil {
		return err
	}
	for _, old := range previous {
		if !resourceObservationSupersedes(current, old, aliases) {
			continue
		}
		memories, err := listRecords[work.MemoryEntry](t, "memories", " WHERE scope_id=? AND state='valid'", current.ScopeID)
		if err != nil {
			return err
		}
		for _, entry := range memories {
			matched := false
			for _, source := range entry.Sources {
				if reflect.DeepEqual(source, old.Source) {
					matched = true
					break
				}
				if source.Kind == "resource_version" && source.ID == old.ID && source.EvidenceRef == old.ContentRef && (source.Version == old.Source.Version || source.Version == old.ContentHash || source.Version == old.ID) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			expected := entry.Revision
			entry.Revision++
			entry.State = work.MemoryInvalidated
			entry.UpdatedAt = current.CapturedAt
			if err = t.PutMemory(entry, expected); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Tx) projectFeishuResourceSources(scopeID string) (map[string]string, error) {
	scope, err := t.GetScope(scopeID)
	if err != nil || scope.Kind != work.ScopeProject {
		return nil, err
	}
	resources, err := t.ListResources(scopeID)
	if err != nil {
		return nil, err
	}
	sources := make(map[string]string, len(resources))
	for _, resource := range resources {
		if work.IsFeishuResource(resource) {
			sources[resource.ID] = resource.SourceIdentity
		}
	}
	return sources, nil
}

func normalizedMemoryObservationRange(version work.ResourceVersion) string {
	// Docx's capture range labels its revision, while the observed range is the
	// same document. The native numeric revision independently orders changes.
	if version.Source.Kind == "feishu_docx" && version.ObservationRange == "document revision "+version.Source.Version {
		return "document"
	}
	return strings.TrimSpace(version.ObservationRange)
}

func resourceObservationSupersedes(current, old work.ResourceVersion, aliases map[string]string) bool {
	source := aliases[current.ResourceID]
	sameResource := current.ResourceID == old.ResourceID || source != "" && source == aliases[old.ResourceID]
	if current.ID == old.ID || !sameResource || current.ScopeID != old.ScopeID || current.Kind == work.VersionCurrentReference || !current.Saved || old.Source.Kind != current.Source.Kind || old.Source.ID != current.Source.ID || old.Source.Version == current.Source.Version || normalizedMemoryObservationRange(old) != normalizedMemoryObservationRange(current) {
		return false
	}
	oldNumber, oldErr := strconv.ParseInt(old.Source.Version, 10, 64)
	newNumber, newErr := strconv.ParseInt(current.Source.Version, 10, 64)
	if oldErr == nil && newErr == nil {
		return newNumber > oldNumber
	}
	return !old.CapturedAt.IsZero() && !current.StartedAt.IsZero() && current.StartedAt.After(old.CapturedAt)
}
func (t *Tx) validateMemoryResourceSources(entry work.MemoryEntry) error {
	versions, err := listRecords[work.ResourceVersion](t, "resource_versions", " WHERE scope_id=?", entry.StorageScopeID)
	if err != nil {
		return err
	}
	if len(versions) < 2 {
		return nil
	}
	aliases, err := t.projectFeishuResourceSources(entry.StorageScopeID)
	if err != nil {
		return err
	}
	for _, source := range entry.Sources {
		for _, old := range versions {
			matches := reflect.DeepEqual(source, old.Source) || (source.Kind == "resource_version" && source.ID == old.ID && source.EvidenceRef == old.ContentRef)
			if !matches {
				continue
			}
			for _, newer := range versions {
				if !resourceObservationSupersedes(newer, old, aliases) {
					continue
				}
				adopted := false
				for _, currentSource := range entry.Sources {
					if reflect.DeepEqual(currentSource, newer.Source) || (currentSource.Kind == "resource_version" && currentSource.ID == newer.ID && currentSource.EvidenceRef == newer.ContentRef) {
						adopted = true
						break
					}
				}
				if !adopted {
					return fmt.Errorf("memory source is superseded; read and explicitly adopt the newer resource observation")
				}
			}
		}
	}
	return nil
}
