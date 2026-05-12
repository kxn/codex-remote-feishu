package upgradecontract

import (
	"strings"
	"testing"
)

func TestParseCommandTextRecognizesCanonicalSubcommands(t *testing.T) {
	tests := []struct {
		text      string
		wantMode  CommandMode
		wantTrack ReleaseTrack
	}{
		{text: "/upgrade", wantMode: CommandShowStatus},
		{text: "/upgrade track", wantMode: CommandShowTrack},
		{text: "/upgrade track beta", wantMode: CommandSetTrack, wantTrack: ReleaseTrackBeta},
		{text: "/upgrade latest", wantMode: CommandLatest},
		{text: "/upgrade codex", wantMode: CommandCodex},
		{text: "/upgrade dev", wantMode: CommandDev},
		{text: "/upgrade local", wantMode: CommandLocal},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			parsed, err := ParseCommandText(tt.text)
			if err != nil {
				t.Fatalf("ParseCommandText(%q): %v", tt.text, err)
			}
			if parsed.Mode != tt.wantMode {
				t.Fatalf("mode = %q, want %q", parsed.Mode, tt.wantMode)
			}
			if parsed.Track != tt.wantTrack {
				t.Fatalf("track = %q, want %q", parsed.Track, tt.wantTrack)
			}
		})
	}
}

func TestParseCommandTextReportsInvalidTrackSeparately(t *testing.T) {
	_, err := ParseCommandText("/upgrade track nightly")
	if !IsInvalidTrackError(err) {
		t.Fatalf("expected invalid track error, got %v", err)
	}
}

func TestNormalizeMenuArgumentRecognizesDevAndTrackVariants(t *testing.T) {
	tests := map[string]string{
		"dev":              "dev",
		"track_beta":       "track beta",
		"track-production": "track production",
	}
	for input, want := range tests {
		got, ok := NormalizeMenuArgument(input)
		if !ok {
			t.Fatalf("NormalizeMenuArgument(%q) rejected", input)
		}
		if got != want {
			t.Fatalf("NormalizeMenuArgument(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildDefinitionRespectsCapabilityPolicy(t *testing.T) {
	shipping := BuildDefinition(CapabilityPolicy{
		AllowedReleaseTracks: []ReleaseTrack{ReleaseTrackBeta, ReleaseTrackProduction},
	})
	if strings.Contains(strings.Join(shipping.Examples, " "), "/upgrade dev") {
		t.Fatalf("shipping examples should hide dev: %#v", shipping.Examples)
	}
	if strings.Contains(strings.Join(shipping.Examples, " "), "/upgrade local") {
		t.Fatalf("shipping examples should hide local: %#v", shipping.Examples)
	}
	if strings.Contains(shipping.ArgumentFormNote, "dev") || strings.Contains(shipping.ArgumentFormNote, "local") {
		t.Fatalf("shipping form note should hide dev/local: %q", shipping.ArgumentFormNote)
	}
	alpha := BuildDefinition(CapabilityPolicy{
		AllowedReleaseTracks: []ReleaseTrack{ReleaseTrackAlpha, ReleaseTrackBeta, ReleaseTrackProduction},
		AllowDevUpgrade:      true,
	})
	if !strings.Contains(strings.Join(alpha.Examples, " "), "/upgrade dev") {
		t.Fatalf("alpha examples should include dev: %#v", alpha.Examples)
	}
	for _, option := range alpha.Options {
		if option.CommandText == "/upgrade dev" {
			return
		}
	}
	t.Fatalf("alpha options should include dev: %#v", alpha.Options)
}

func TestUsageHelpersRespectCapabilityPolicy(t *testing.T) {
	summary := UsageSummary(CapabilityPolicy{
		AllowedReleaseTracks: []ReleaseTrack{ReleaseTrackBeta, ReleaseTrackProduction},
	})
	if strings.Contains(summary, "`dev`") || strings.Contains(summary, "`local`") {
		t.Fatalf("summary should hide dev/local: %q", summary)
	}
	syntax := UsageSyntax(CapabilityPolicy{
		AllowedReleaseTracks: []ReleaseTrack{ReleaseTrackAlpha, ReleaseTrackBeta, ReleaseTrackProduction},
		AllowDevUpgrade:      true,
		AllowLocalUpgrade:    true,
	})
	for _, want := range []string{"`/upgrade track [alpha|beta|production]`", "`/upgrade dev`", "`/upgrade local`"} {
		if !strings.Contains(syntax, want) {
			t.Fatalf("syntax %q missing %q", syntax, want)
		}
	}
}
