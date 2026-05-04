package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/displaypath"
)

func inspectWorkspaceDisplayInfo(workspaceKey string) gitmeta.WorkspaceInfo {
	info, err := gitmeta.InspectWorkspace(workspaceKey, gitmeta.InspectOptions{})
	if err != nil {
		return gitmeta.WorkspaceInfo{}
	}
	return info
}

func targetPickerWorkspaceMetaByKey(entries []workspaceSelectionEntry) map[string]string {
	metaByKey := map[string]string{}
	if len(entries) == 0 {
		return metaByKey
	}
	families := map[string][]workspaceSelectionEntry{}
	for _, entry := range entries {
		if entry.recoverableOnly {
			continue
		}
		familyKey := strings.TrimSpace(entry.gitInfo.RepoFamilyKey())
		if familyKey == "" {
			continue
		}
		families[familyKey] = append(families[familyKey], entry)
	}
	for _, familyEntries := range families {
		if len(familyEntries) < 2 {
			continue
		}
		branchGroups := map[string][]workspaceSelectionEntry{}
		for _, entry := range familyEntries {
			branch := strings.TrimSpace(entry.gitInfo.Branch)
			if branch == "" {
				continue
			}
			key := normalizeWorkspaceClaimKey(entry.workspaceKey)
			if key == "" {
				continue
			}
			metaByKey[key] = branch
			branchGroups[branch] = append(branchGroups[branch], entry)
		}
		for branch, branchEntries := range branchGroups {
			if len(branchEntries) < 2 {
				continue
			}
			suffixes := targetPickerWorkspaceShortTails(branchEntries)
			for _, entry := range branchEntries {
				key := normalizeWorkspaceClaimKey(entry.workspaceKey)
				if key == "" {
					continue
				}
				tail := strings.TrimSpace(suffixes[key])
				if tail == "" {
					continue
				}
				metaByKey[key] = branch + "@" + tail
			}
		}
	}
	return metaByKey
}

func targetPickerWorkspaceShortTails(entries []workspaceSelectionEntry) map[string]string {
	if len(entries) == 0 {
		return map[string]string{}
	}
	paths := make([]string, 0, len(entries))
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		workspaceKey := normalizeWorkspaceClaimKey(entry.workspaceKey)
		if workspaceKey == "" {
			continue
		}
		keys = append(keys, workspaceKey)
		paths = append(paths, workspaceKey)
	}
	raw := displaypath.PathLabels(paths)
	resolved := make(map[string]string, len(keys))
	for _, key := range keys {
		resolved[key] = strings.TrimSpace(raw[displaypath.Normalize(key)])
	}
	return resolved
}
