package buildinfo

import "strings"

type Flavor string

const (
	FlavorDev      Flavor = "dev"
	FlavorAlpha    Flavor = "alpha"
	FlavorShipping Flavor = "shipping"
)

// FlavorValue is injected by release builds. Source builds default to dev.
var FlavorValue = string(FlavorDev)

type CapabilityPolicy struct {
	Flavor               Flavor
	AllowedReleaseTracks []string
	AllowDevUpgrade      bool
	AllowLocalUpgrade    bool
	DefaultPprofEnabled  bool
	DefaultRelayFlow     bool
	DefaultRelayRaw      bool
}

func ParseFlavor(value string) Flavor {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(FlavorShipping):
		return FlavorShipping
	case string(FlavorAlpha):
		return FlavorAlpha
	case string(FlavorDev):
		return FlavorDev
	default:
		return FlavorDev
	}
}

func CurrentFlavor() Flavor {
	return ParseFlavor(FlavorValue)
}

func CurrentCapabilityPolicy() CapabilityPolicy {
	return CapabilityPolicyForFlavor(CurrentFlavor())
}

func CapabilityPolicyForFlavor(flavor Flavor) CapabilityPolicy {
	switch ParseFlavor(string(flavor)) {
	case FlavorShipping:
		return CapabilityPolicy{
			Flavor:               FlavorShipping,
			AllowedReleaseTracks: []string{"beta", "production"},
			AllowDevUpgrade:      false,
			AllowLocalUpgrade:    false,
		}
	case FlavorAlpha:
		return CapabilityPolicy{
			Flavor:               FlavorAlpha,
			AllowedReleaseTracks: []string{"alpha", "beta", "production"},
			AllowDevUpgrade:      true,
			AllowLocalUpgrade:    false,
		}
	default:
		return CapabilityPolicy{
			Flavor:               FlavorDev,
			AllowedReleaseTracks: []string{"alpha", "beta", "production"},
			AllowDevUpgrade:      true,
			AllowLocalUpgrade:    true,
			DefaultPprofEnabled:  true,
		}
	}
}

func (p CapabilityPolicy) AllowsReleaseTrack(track string) bool {
	track = strings.ToLower(strings.TrimSpace(track))
	for _, allowed := range p.AllowedReleaseTracks {
		if strings.EqualFold(strings.TrimSpace(allowed), track) {
			return true
		}
	}
	return false
}
