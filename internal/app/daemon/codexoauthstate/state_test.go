package codexoauthstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStoreApplyProbeRollsBackAllMemoryStateWhenSaveFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex-oauth-profile.json")
	store := NewStore(path)
	first, _, err := store.ApplyProbe(codexprofile.OAuthProbeObservation{
		Result: codexprofile.OAuthProbeResult{
			Status:      codexprofile.OAuthProbeStatusDetected,
			AccountHint: "u***@example.com",
		},
		CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
	}, time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC), "lifecycle-1", false)
	if err != nil {
		t.Fatalf("initial ApplyProbe: %v", err)
	}

	failingPath := filepath.Join(t.TempDir(), "existing-directory")
	if err := os.Mkdir(failingPath, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	store.path = failingPath
	_, _, err = store.ApplyProbe(codexprofile.OAuthProbeObservation{
		Result:        codexprofile.OAuthProbeResult{Status: codexprofile.OAuthProbeStatusMissing},
		CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
	}, time.Date(2026, 8, 1, 2, 3, 4, 0, time.UTC), "lifecycle-2", false)
	if err == nil {
		t.Fatal("ApplyProbe unexpectedly succeeded")
	}

	current, ok := store.Current()
	if !ok || current != first {
		t.Fatalf("profile changed after failed save: got=%#v want=%#v", current, first)
	}
	if store.lastKnownStatus != string(codexprofile.OAuthProbeStatusDetected) {
		t.Fatalf("lastKnownStatus = %q, want detected", store.lastKnownStatus)
	}
	if store.lastConfirmedLifecycleID != "lifecycle-1" {
		t.Fatalf("lastConfirmedLifecycleID = %q, want lifecycle-1", store.lastConfirmedLifecycleID)
	}
}

func TestStorePersistsRedactedOAuthDescriptorAndStableGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex-oauth-profile.json")
	store := NewStore(path)
	checkedAt := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)

	first, changed, err := store.ApplyProbe(codexprofile.OAuthProbeObservation{
		Result: codexprofile.OAuthProbeResult{
			Status:      codexprofile.OAuthProbeStatusDetected,
			AccountHint: "u***@example.com",
			PlanType:    "plus",
		},
		CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
	}, checkedAt, "daemon-a", false)
	if err != nil {
		t.Fatalf("ApplyProbe: %v", err)
	}
	if !changed || first.Revision != 1 || first.AuthGeneration != 1 || first.ProfileID != state.OAuthCodexProfileID {
		t.Fatalf("unexpected initial state: %#v changed=%t", first, changed)
	}

	same, changed, err := store.ApplyProbe(codexprofile.OAuthProbeObservation{
		Result: codexprofile.OAuthProbeResult{
			Status:      codexprofile.OAuthProbeStatusDetected,
			AccountHint: "u***@example.com",
			PlanType:    "plus",
		},
		CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
	}, checkedAt.Add(time.Minute), "daemon-a", false)
	if err != nil {
		t.Fatalf("ApplyProbe same: %v", err)
	}
	if changed || same.Revision != 1 || same.AuthGeneration != 1 {
		t.Fatalf("unchanged probe advanced generations: %#v changed=%t", same, changed)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	got, ok := reloaded.Current()
	if !ok || got != same {
		t.Fatalf("reloaded state = %#v ok=%t, want %#v", got, ok, same)
	}
}

func TestStoreUnknownDoesNotDeleteOrAdvanceDetectedOAuth(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	detected, _, err := store.ApplyProbe(detectedObservation("u***@example.com"), time.Now().UTC(), "daemon-a", false)
	if err != nil {
		t.Fatalf("apply detected: %v", err)
	}

	unknown, changed, err := store.ApplyProbe(codexprofile.OAuthProbeObservation{
		Result: codexprofile.OAuthProbeResult{
			Status:             codexprofile.OAuthProbeStatusUnknown,
			LastProbeErrorCode: codexprofile.ErrorOAuthProbeUnknown,
		},
	}, time.Now().UTC(), "daemon-a", false)
	if err != nil {
		t.Fatalf("apply unknown: %v", err)
	}
	if changed || unknown.Status != string(codexprofile.OAuthProbeStatusUnknown) ||
		unknown.Revision != detected.Revision || unknown.AuthGeneration != detected.AuthGeneration || unknown.AccountHint != detected.AccountHint {
		t.Fatalf("unknown probe destroyed detected identity: before=%#v after=%#v changed=%t", detected, unknown, changed)
	}
}

func TestStoreAdvancesAuthGenerationOnlyForIdentityEvidence(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	current, _, _ := store.ApplyProbe(detectedObservation("u***@example.com"), time.Now().UTC(), "daemon-a", false)

	newLifecycle, changed, err := store.ApplyProbe(detectedObservation("u***@example.com"), time.Now().UTC(), "daemon-b", false)
	if err != nil || !changed || newLifecycle.AuthGeneration != current.AuthGeneration+1 || newLifecycle.Revision != current.Revision+1 {
		t.Fatalf("new lifecycle confirmation = %#v changed=%t err=%v", newLifecycle, changed, err)
	}

	missing, changed, err := store.ApplyProbe(codexprofile.OAuthProbeObservation{
		Result:        codexprofile.OAuthProbeResult{Status: codexprofile.OAuthProbeStatusMissing},
		CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
	}, time.Now().UTC(), "daemon-b", false)
	if err != nil || !changed || missing.AuthGeneration != newLifecycle.AuthGeneration+1 || missing.Revision != newLifecycle.Revision+1 {
		t.Fatalf("detected to missing = %#v changed=%t err=%v", missing, changed, err)
	}

	redetected, changed, err := store.ApplyProbe(detectedObservation("v***@example.com"), time.Now().UTC(), "daemon-b", true)
	if err != nil || !changed || redetected.AuthGeneration != missing.AuthGeneration+1 || redetected.Revision != missing.Revision+1 {
		t.Fatalf("auth event re-detection = %#v changed=%t err=%v", redetected, changed, err)
	}
}

func TestStoreAvailabilityAndCapabilityChangeRevisionWithoutChangingAuthGeneration(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	current, _, _ := store.ApplyProbe(detectedObservation("u***@example.com"), time.Now().UTC(), "daemon-a", false)

	custom := detectedObservation("u***@example.com")
	custom.Result.AvailabilityCode = codexprofile.ErrorOAuthDeploymentUnsupported
	next, changed, err := store.ApplyProbe(custom, time.Now().UTC(), "daemon-a", false)
	if err != nil || !changed || next.Revision != current.Revision+1 || next.AuthGeneration != current.AuthGeneration {
		t.Fatalf("availability change = %#v changed=%t err=%v", next, changed, err)
	}
}

func detectedObservation(accountHint string) codexprofile.OAuthProbeObservation {
	return codexprofile.OAuthProbeObservation{
		Result: codexprofile.OAuthProbeResult{
			Status:      codexprofile.OAuthProbeStatusDetected,
			AccountHint: accountHint,
			PlanType:    "plus",
		},
		CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
	}
}
