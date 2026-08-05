package daemon

import (
	"os"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
)

func detectManagedShimStatuses(entrypoints []string, currentBinary string) (map[string]editor.ManagedShimStatus, error) {
	statuses := make(map[string]editor.ManagedShimStatus, len(entrypoints))
	for _, entrypoint := range entrypoints {
		entrypoint = strings.TrimSpace(entrypoint)
		if entrypoint == "" {
			continue
		}
		status, err := editor.DetectManagedShim(entrypoint, currentBinary)
		if err != nil {
			return nil, err
		}
		statuses[entrypoint] = status
	}
	return statuses, nil
}

func lookupManagedShimStatus(statuses map[string]editor.ManagedShimStatus, entrypoint, currentBinary string) (editor.ManagedShimStatus, error) {
	entrypoint = strings.TrimSpace(entrypoint)
	if entrypoint == "" {
		return editor.ManagedShimStatus{}, nil
	}
	if status, ok := statuses[entrypoint]; ok {
		return status, nil
	}
	status, err := editor.DetectManagedShim(entrypoint, currentBinary)
	if err != nil {
		return editor.ManagedShimStatus{}, err
	}
	return status, nil
}

func computeShimReinstallNeed(currentMode, recordedEntrypoint, latestEntrypoint string, latestShim editor.ManagedShimStatus, statuses map[string]editor.ManagedShimStatus, currentConfigPath, currentStatePath string) bool {
	managedActive := modeIncludes(currentMode, install.IntegrationManagedShim)
	if !managedActive {
		return false
	}
	if strings.TrimSpace(latestEntrypoint) != "" && shimEntryNeedsRepair(latestShim, true) {
		return true
	}

	recordedEntrypoint = strings.TrimSpace(recordedEntrypoint)
	for _, entrypoint := range historicalManagedShimTargets(recordedEntrypoint, statuses, currentConfigPath, currentStatePath) {
		if samePlatformPath(entrypoint, latestEntrypoint) {
			continue
		}
		status := statuses[entrypoint]
		if shimEntryNeedsRepair(status, false) {
			return true
		}
	}
	return false
}

func historicalManagedShimTargets(recordedEntrypoint string, statuses map[string]editor.ManagedShimStatus, currentConfigPath, currentStatePath string) []string {
	targets := map[string]bool{}
	recordedEntrypoint = strings.TrimSpace(recordedEntrypoint)
	if recordedEntrypoint != "" {
		if status, ok := statuses[recordedEntrypoint]; ok && status.RepoManaged && status.Exists {
			targets[recordedEntrypoint] = true
		}
	}
	for entrypoint, status := range statuses {
		if !status.Exists {
			continue
		}
		switch status.Kind {
		case editor.ManagedShimKindTiny:
			if !status.SidecarValid {
				continue
			}
			if samePlatformPath(status.SidecarConfigPath, currentConfigPath) || samePlatformPath(status.SidecarInstallStatePath, currentStatePath) {
				targets[entrypoint] = true
			}
		case editor.ManagedShimKindLegacy:
			// A legacy copied-binary shim has no sidecar to confirm
			// ownership; require a content match against the current
			// wrapper binary so unrelated bundle binaries are left alone.
			if status.RepoManaged && status.MatchesBinary {
				targets[entrypoint] = true
			}
		}
	}
	ordered := make([]string, 0, len(targets))
	for entrypoint := range targets {
		ordered = append(ordered, entrypoint)
	}
	sort.Strings(ordered)
	return ordered
}

func managedShimMigrationTargets(primaryEntrypoint, recordedEntrypoint string, statuses map[string]editor.ManagedShimStatus, currentConfigPath, currentStatePath string) []string {
	targets := []string{}
	seen := map[string]bool{}
	add := func(entrypoint string) {
		entrypoint = strings.TrimSpace(entrypoint)
		if entrypoint == "" || seen[entrypoint] {
			return
		}
		if info, err := os.Stat(entrypoint); err != nil || !info.Mode().IsRegular() {
			return
		}
		seen[entrypoint] = true
		targets = append(targets, entrypoint)
	}

	add(primaryEntrypoint)

	recordedEntrypoint = strings.TrimSpace(recordedEntrypoint)
	if recordedEntrypoint != "" {
		if status, ok := statuses[recordedEntrypoint]; ok && status.RepoManaged && status.Exists {
			add(recordedEntrypoint)
		}
	}
	for _, entrypoint := range historicalManagedShimTargets(recordedEntrypoint, statuses, currentConfigPath, currentStatePath) {
		add(entrypoint)
	}
	return targets
}

func shimEntryNeedsRepair(status editor.ManagedShimStatus, requireTiny bool) bool {
	if !status.Exists {
		return false
	}
	if requireTiny && status.Kind != editor.ManagedShimKindTiny {
		return true
	}
	if status.Kind == editor.ManagedShimKindTiny {
		return !status.Installed || !status.SidecarValid
	}
	return status.RepoManaged || requireTiny
}
