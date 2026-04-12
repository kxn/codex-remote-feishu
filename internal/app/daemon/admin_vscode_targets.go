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

func computeShimReinstallNeed(currentMode string, installState *install.InstallState, latestEntrypoint string, latestShim editor.ManagedShimStatus, statuses map[string]editor.ManagedShimStatus, currentConfigPath, currentStatePath string) bool {
	managedActive := modeIncludes(currentMode, install.IntegrationManagedShim)
	if !managedActive && installState != nil {
		for _, integration := range installState.Integrations {
			if integration == install.IntegrationManagedShim {
				managedActive = true
				break
			}
		}
	}
	if !managedActive {
		return false
	}
	if strings.TrimSpace(latestEntrypoint) != "" && shimEntryNeedsRepair(latestShim, true) {
		return true
	}
	if installState != nil && strings.TrimSpace(latestEntrypoint) != "" && !samePlatformPath(latestEntrypoint, installState.BundleEntrypoint) {
		return true
	}

	recordedEntrypoint := ""
	if installState != nil {
		recordedEntrypoint = strings.TrimSpace(installState.BundleEntrypoint)
	}
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
		if !status.Exists || status.Kind != editor.ManagedShimKindTiny || !status.SidecarValid {
			continue
		}
		if samePlatformPath(status.SidecarConfigPath, currentConfigPath) || samePlatformPath(status.SidecarInstallStatePath, currentStatePath) {
			targets[entrypoint] = true
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
		return !status.Installed || !status.SidecarValid || !status.MatchesBinary
	}
	return status.RepoManaged || requireTiny
}
