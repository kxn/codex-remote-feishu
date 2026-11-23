package orchestrator

import (
	"fmt"
	"time"
)

// PrivateUpgradeFlow is private UI intent, not the authority for the shared
// install transaction. Its candidate fingerprint must be rechecked by the host.
type PrivateUpgradeFlow struct {
	ID, OwnerUserID, MessageID, Phase                              string
	Source, Track, CurrentVersion, TargetVersion, StateFingerprint string
	CodexChecked, CodexCanUpgrade                                  bool
	SharedDisplayHash                                              string
	SharedRequestID                                                string
	SourceSHA256                                                   string
	CreatedAt, ExpiresAt                                           time.Time
}

func (s *Service) SetPrivateUpgradeFlow(surfaceID string, flow *PrivateUpgradeFlow) error {
	surface := s.root.Surfaces[surfaceID]
	if !s.config.PrivateApplication || surface == nil {
		return fmt.Errorf("private surface unavailable")
	}
	runtime := s.ensureSurfaceUIRuntime(surface)
	if flow == nil {
		runtime.PrivateUpgradeFlow = nil
		return nil
	}
	if flow.ID == "" || flow.OwnerUserID != surface.ActorUserID || flow.ExpiresAt.IsZero() {
		return fmt.Errorf("invalid private upgrade owner")
	}
	copy := *flow
	runtime.PrivateUpgradeFlow = &copy
	return nil
}
func (s *Service) PrivateUpgradeFlow(surfaceID string) *PrivateUpgradeFlow {
	if !s.config.PrivateApplication {
		return nil
	}
	runtime := s.surfaceUIRuntimeByID(surfaceID)
	if runtime == nil || runtime.PrivateUpgradeFlow == nil {
		return nil
	}
	copy := *runtime.PrivateUpgradeFlow
	return &copy
}
