package feishu

import "strings"

const (
	PlatformFeishu         = "feishu"
	ScopeKindUser          = "user"
	ScopeKindChat          = "chat"
	LegacyDefaultGatewayID = "legacy-default"
)

type SurfaceRef struct {
	Platform  string
	GatewayID string
	ScopeKind string
	ScopeID   string
}

func ParseSurfaceRef(surfaceID string) (SurfaceRef, bool) {
	parts := strings.Split(strings.TrimSpace(surfaceID), ":")
	switch len(parts) {
	case 4:
		if parts[0] != PlatformFeishu {
			return SurfaceRef{}, false
		}
		ref := SurfaceRef{
			Platform:  parts[0],
			GatewayID: normalizeGatewayID(parts[1]),
			ScopeKind: strings.TrimSpace(parts[2]),
			ScopeID:   strings.TrimSpace(parts[3]),
		}
		if !ref.valid() {
			return SurfaceRef{}, false
		}
		return ref, true
	case 3:
		if parts[0] != PlatformFeishu {
			return SurfaceRef{}, false
		}
		ref := SurfaceRef{
			Platform:  parts[0],
			GatewayID: LegacyDefaultGatewayID,
			ScopeKind: strings.TrimSpace(parts[1]),
			ScopeID:   strings.TrimSpace(parts[2]),
		}
		if !ref.valid() {
			return SurfaceRef{}, false
		}
		return ref, true
	default:
		return SurfaceRef{}, false
	}
}

func (r SurfaceRef) SurfaceID() string {
	if !r.valid() {
		return ""
	}
	return strings.Join([]string{
		PlatformFeishu,
		normalizeGatewayID(r.GatewayID),
		r.ScopeKind,
		r.ScopeID,
	}, ":")
}

func (r SurfaceRef) valid() bool {
	if strings.TrimSpace(r.Platform) != PlatformFeishu {
		return false
	}
	if strings.TrimSpace(r.ScopeID) == "" {
		return false
	}
	switch strings.TrimSpace(r.ScopeKind) {
	case ScopeKindUser, ScopeKindChat:
		return true
	default:
		return false
	}
}

func normalizeGatewayID(gatewayID string) string {
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return LegacyDefaultGatewayID
	}
	return gatewayID
}
