package install

import (
	"github.com/kxn/codex-remote-feishu/internal/buildinfo"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func CurrentBuildFlavor() buildinfo.Flavor {
	return buildinfo.CurrentFlavor()
}

func CurrentBuildAllowsReleaseTrack(track ReleaseTrack) bool {
	return buildinfo.CurrentCapabilityPolicy().AllowsReleaseTrack(string(track))
}

func CurrentBuildAllowedReleaseTracks() []ReleaseTrack {
	policy := buildinfo.CurrentCapabilityPolicy()
	values := make([]ReleaseTrack, 0, len(policy.AllowedReleaseTracks))
	for _, track := range policy.AllowedReleaseTracks {
		if parsed := ParseReleaseTrack(track); parsed != "" {
			values = append(values, parsed)
		}
	}
	return values
}

func CurrentBuildAllowsLocalUpgrade() bool {
	return buildinfo.CurrentCapabilityPolicy().AllowLocalUpgrade
}

func DefaultTrackForInstallSource(source InstallSource) ReleaseTrack {
	source = normalizeInstallSource(source)
	if source != InstallSourceRelease && CurrentBuildAllowsReleaseTrack(ReleaseTrackAlpha) {
		return ReleaseTrackAlpha
	}
	for _, track := range []ReleaseTrack{ReleaseTrackProduction, ReleaseTrackBeta, ReleaseTrackAlpha} {
		if CurrentBuildAllowsReleaseTrack(track) {
			return track
		}
	}
	return ReleaseTrackProduction
}

func applyBuildFlavorDebugDefaults(cfg *config.AppConfig) {
	if cfg == nil {
		return
	}
	policy := buildinfo.CurrentCapabilityPolicy()
	if cfg.Debug.Pprof == nil {
		cfg.Debug.Pprof = &config.PprofSettings{}
	}
	if policy.DefaultPprofEnabled {
		cfg.Debug.Pprof.Enabled = true
	}
	if policy.DefaultRelayFlow {
		cfg.Debug.RelayFlow = true
	}
	if policy.DefaultRelayRaw {
		cfg.Debug.RelayRaw = true
	}
}
