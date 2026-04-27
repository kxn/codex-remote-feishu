package buildinfo

import "testing"

func TestParseFlavorDefaultsToDev(t *testing.T) {
	if got := ParseFlavor(""); got != FlavorDev {
		t.Fatalf("ParseFlavor(\"\") = %q, want %q", got, FlavorDev)
	}
	if got := ParseFlavor("unknown"); got != FlavorDev {
		t.Fatalf("ParseFlavor(\"unknown\") = %q, want %q", got, FlavorDev)
	}
}

func TestCapabilityPolicyForShipping(t *testing.T) {
	policy := CapabilityPolicyForFlavor(FlavorShipping)
	if policy.Flavor != FlavorShipping {
		t.Fatalf("Flavor = %q, want %q", policy.Flavor, FlavorShipping)
	}
	if policy.AllowDevUpgrade {
		t.Fatal("shipping policy should not allow dev upgrade")
	}
	if policy.AllowLocalUpgrade {
		t.Fatal("shipping policy should not allow local upgrade")
	}
	if policy.DefaultPprofEnabled {
		t.Fatal("shipping policy should keep pprof disabled by default")
	}
	if policy.AllowsReleaseTrack("alpha") {
		t.Fatal("shipping policy should not allow alpha track")
	}
	if !policy.AllowsReleaseTrack("beta") || !policy.AllowsReleaseTrack("production") {
		t.Fatalf("shipping policy allowed tracks = %#v", policy.AllowedReleaseTracks)
	}
}

func TestCapabilityPolicyForAlpha(t *testing.T) {
	policy := CapabilityPolicyForFlavor(FlavorAlpha)
	if policy.Flavor != FlavorAlpha {
		t.Fatalf("Flavor = %q, want %q", policy.Flavor, FlavorAlpha)
	}
	if !policy.AllowDevUpgrade {
		t.Fatal("alpha policy should allow dev upgrade")
	}
	if policy.AllowLocalUpgrade {
		t.Fatal("alpha policy should not allow local upgrade")
	}
	if policy.DefaultPprofEnabled {
		t.Fatal("alpha policy should keep pprof disabled by default")
	}
	for _, track := range []string{"alpha", "beta", "production"} {
		if !policy.AllowsReleaseTrack(track) {
			t.Fatalf("alpha policy should allow %q, got %#v", track, policy.AllowedReleaseTracks)
		}
	}
}

func TestCapabilityPolicyForDev(t *testing.T) {
	policy := CapabilityPolicyForFlavor(FlavorDev)
	if policy.Flavor != FlavorDev {
		t.Fatalf("Flavor = %q, want %q", policy.Flavor, FlavorDev)
	}
	if !policy.AllowDevUpgrade {
		t.Fatal("dev policy should allow dev upgrade")
	}
	if !policy.AllowLocalUpgrade {
		t.Fatal("dev policy should allow local upgrade")
	}
	if !policy.DefaultPprofEnabled {
		t.Fatal("dev policy should enable pprof by default")
	}
	for _, track := range []string{"alpha", "beta", "production"} {
		if !policy.AllowsReleaseTrack(track) {
			t.Fatalf("dev policy should allow %q, got %#v", track, policy.AllowedReleaseTracks)
		}
	}
}
