package install

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type InstallSource string

const (
	InstallSourceRelease InstallSource = "release"
	InstallSourceRepo    InstallSource = "repo"
)

type ReleaseTrack string

const (
	ReleaseTrackProduction ReleaseTrack = "production"
	ReleaseTrackBeta       ReleaseTrack = "beta"
	ReleaseTrackAlpha      ReleaseTrack = "alpha"
)

type RollbackCandidate struct {
	Version     string        `json:"version,omitempty"`
	BinaryPath  string        `json:"binaryPath,omitempty"`
	Source      InstallSource `json:"source,omitempty"`
	Fingerprint string        `json:"fingerprint,omitempty"`
}

type PendingUpgrade struct {
	Phase            string       `json:"phase,omitempty"`
	TargetTrack      ReleaseTrack `json:"targetTrack,omitempty"`
	TargetVersion    string       `json:"targetVersion,omitempty"`
	GatewayID        string       `json:"gatewayID,omitempty"`
	SurfaceSessionID string       `json:"surfaceSessionID,omitempty"`
	ChatID           string       `json:"chatID,omitempty"`
	ActorUserID      string       `json:"actorUserID,omitempty"`
	SourceMessageID  string       `json:"sourceMessageID,omitempty"`
	RequestedAt      *time.Time   `json:"requestedAt,omitempty"`
}

const (
	PendingUpgradePhaseAvailable  = "available"
	PendingUpgradePhasePrompted   = "prompted"
	PendingUpgradePhasePrepared   = "prepared"
	PendingUpgradePhaseSwitching  = "switching"
	PendingUpgradePhaseObserving  = "observing"
	PendingUpgradePhaseCommitted  = "committed"
	PendingUpgradePhaseRolledBack = "rolled_back"
	PendingUpgradePhaseFailed     = "failed"
)

type StateMetadataOptions struct {
	StatePath       string
	SourceBinary    string
	InstalledBinary string
	CurrentVersion  string
	InstallSource   InstallSource
	CurrentTrack    ReleaseTrack
	VersionsRoot    string
	CurrentSlot     string
}

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-(?:alpha|beta)\.[0-9]+)?$`)

func ApplyStateMetadata(state *InstallState, opts StateMetadataOptions) {
	if state == nil {
		return
	}

	if strings.TrimSpace(state.StatePath) == "" {
		state.StatePath = strings.TrimSpace(opts.StatePath)
	}

	if strings.TrimSpace(state.CurrentBinaryPath) == "" {
		state.CurrentBinaryPath = firstNonEmpty(
			strings.TrimSpace(opts.InstalledBinary),
			strings.TrimSpace(state.InstalledBinary),
			strings.TrimSpace(opts.SourceBinary),
		)
	}

	if strings.TrimSpace(state.VersionsRoot) == "" {
		state.VersionsRoot = firstNonEmpty(
			strings.TrimSpace(opts.VersionsRoot),
			defaultVersionsRootForStatePath(state.StatePath),
		)
	}

	currentVersion := firstNonEmpty(strings.TrimSpace(opts.CurrentVersion), strings.TrimSpace(state.CurrentVersion))
	source := normalizeInstallSource(opts.InstallSource)
	if source == "" {
		source = inferInstallSource(strings.TrimSpace(opts.SourceBinary), strings.TrimSpace(state.VersionsRoot), currentVersion)
	}
	if state.InstallSource == "" {
		state.InstallSource = source
	}

	slot := strings.TrimSpace(opts.CurrentSlot)
	if slot == "" {
		slot = inferCurrentSlot(strings.TrimSpace(opts.SourceBinary), strings.TrimSpace(state.VersionsRoot))
	}
	if strings.TrimSpace(state.CurrentSlot) == "" {
		state.CurrentSlot = slot
	}

	if strings.TrimSpace(state.CurrentVersion) == "" {
		state.CurrentVersion = firstNonEmpty(currentVersion, slot)
	}

	track := normalizeReleaseTrack(opts.CurrentTrack)
	if track == "" {
		track = inferReleaseTrack(firstNonEmpty(strings.TrimSpace(state.CurrentVersion), currentVersion), firstNonEmpty(string(state.InstallSource), string(source)))
	}
	if state.CurrentTrack == "" {
		state.CurrentTrack = track
	}
}

func defaultVersionsRootForStatePath(statePath string) string {
	dir := strings.TrimSpace(filepath.Dir(statePath))
	if dir == "" || dir == "." {
		return ""
	}
	return filepath.Join(dir, "releases")
}

func inferInstallSource(sourceBinary, versionsRoot, currentVersion string) InstallSource {
	if binaryWithinVersionsRoot(sourceBinary, versionsRoot) {
		return InstallSourceRelease
	}
	if looksLikeReleaseVersion(currentVersion) {
		return InstallSourceRelease
	}
	return InstallSourceRepo
}

func inferReleaseTrack(currentVersion, installSource string) ReleaseTrack {
	switch trackFromVersion(currentVersion) {
	case string(ReleaseTrackProduction):
		return ReleaseTrackProduction
	case string(ReleaseTrackBeta):
		return ReleaseTrackBeta
	case string(ReleaseTrackAlpha):
		return ReleaseTrackAlpha
	}
	if normalizeInstallSource(InstallSource(installSource)) == InstallSourceRelease {
		return ReleaseTrackProduction
	}
	return ReleaseTrackAlpha
}

func inferCurrentSlot(sourceBinary, versionsRoot string) string {
	sourceBinary = strings.TrimSpace(sourceBinary)
	versionsRoot = strings.TrimSpace(versionsRoot)
	if sourceBinary == "" || versionsRoot == "" {
		return ""
	}

	sourceBinary = filepath.Clean(sourceBinary)
	versionsRoot = filepath.Clean(versionsRoot)
	rel, err := filepath.Rel(versionsRoot, sourceBinary)
	if err != nil {
		return ""
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func binaryWithinVersionsRoot(sourceBinary, versionsRoot string) bool {
	if strings.TrimSpace(sourceBinary) == "" || strings.TrimSpace(versionsRoot) == "" {
		return false
	}
	sourceBinary = filepath.Clean(sourceBinary)
	versionsRoot = filepath.Clean(versionsRoot)
	rel, err := filepath.Rel(versionsRoot, sourceBinary)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func looksLikeReleaseVersion(version string) bool {
	return releaseVersionPattern.MatchString(strings.TrimSpace(version))
}

func trackFromVersion(version string) string {
	version = strings.TrimSpace(version)
	switch {
	case strings.Contains(version, "-alpha."):
		return string(ReleaseTrackAlpha)
	case strings.Contains(version, "-beta."):
		return string(ReleaseTrackBeta)
	case looksLikeReleaseVersion(version):
		return string(ReleaseTrackProduction)
	default:
		return ""
	}
}

func normalizeInstallSource(source InstallSource) InstallSource {
	switch strings.ToLower(strings.TrimSpace(string(source))) {
	case string(InstallSourceRelease):
		return InstallSourceRelease
	case string(InstallSourceRepo):
		return InstallSourceRepo
	default:
		return ""
	}
}

func normalizeReleaseTrack(track ReleaseTrack) ReleaseTrack {
	switch strings.ToLower(strings.TrimSpace(string(track))) {
	case string(ReleaseTrackProduction):
		return ReleaseTrackProduction
	case string(ReleaseTrackBeta):
		return ReleaseTrackBeta
	case string(ReleaseTrackAlpha):
		return ReleaseTrackAlpha
	default:
		return ""
	}
}

func ParseReleaseTrack(value string) ReleaseTrack {
	return normalizeReleaseTrack(ReleaseTrack(value))
}
