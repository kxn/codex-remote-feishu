package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/pathscope"
)

func TestLoadStateCollapsesLegacyConfigPaths(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	legacyWrapperPath := filepath.Join(baseDir, ".config", "codex-remote", "wrapper.env")
	legacyServicesPath := filepath.Join(baseDir, ".config", "codex-remote", "services.env")
	rawBytes, err := json.Marshal(map[string]string{
		"statePath":          statePath,
		"wrapperConfigPath":  legacyWrapperPath,
		"servicesConfigPath": legacyServicesPath,
	})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, rawBytes, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	wantConfigPath := filepath.Join(baseDir, ".config", "codex-remote", "config.json")
	if loaded.ConfigPath != wantConfigPath {
		t.Fatalf("ConfigPath = %q, want %q", loaded.ConfigPath, wantConfigPath)
	}
	if loaded.StatePath != statePath {
		t.Fatalf("StatePath = %q, want %q", loaded.StatePath, statePath)
	}
}

func TestWriteStateOmitsLegacyConfigPathFields(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	state := InstallState{
		BaseDir:    baseDir,
		ConfigPath: filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:  statePath,
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	for _, field := range []string{"wrapperConfigPath", "servicesConfigPath"} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("did not expect %s in written state: %s", field, raw)
		}
	}
}

func TestWriteStateRespectsStrictFSPrefix(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "sandbox")
	t.Setenv(pathscope.EnvFSPrefix, prefix)
	t.Setenv(pathscope.EnvFSStrict, "1")

	state := InstallState{
		BaseDir:    prefix,
		ConfigPath: filepath.Join(prefix, ".config", "codex-remote", "config.json"),
		StatePath:  filepath.Join(prefix, ".local", "share", "codex-remote", "install-state.json"),
	}
	outsidePath := filepath.Join(t.TempDir(), "install-state.json")
	if err := WriteState(outsidePath, state); err == nil {
		t.Fatal("WriteState(outside) expected strict-prefix error")
	}

	insidePath := filepath.Join(prefix, ".local", "share", "codex-remote", "install-state.json")
	if err := WriteState(insidePath, state); err != nil {
		t.Fatalf("WriteState(inside): %v", err)
	}
}

// TestLoadStatePreservesUpgradeTransactionWithLegacyFields is the #808-E
// compatibility guard: a state file written by an older version (legacy
// config path fields, legacy installed-binary fields, unknown future fields)
// must still load, keep the upgrade transaction (PendingUpgrade) and the
// rollback candidate intact, and ignore unknown fields.
func TestLoadStatePreservesUpgradeTransactionWithLegacyFields(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	raw := map[string]interface{}{
		"instanceId":             "stable",
		"baseDir":                baseDir,
		"statePath":              statePath,
		"wrapperConfigPath":      filepath.Join(baseDir, ".config", "codex-remote", "wrapper.env"),
		"servicesConfigPath":     filepath.Join(baseDir, ".config", "codex-remote", "services.env"),
		"installedBinary":        filepath.Join(baseDir, "bin", "codex-remote.exe"),
		"installedWrapperBinary": filepath.Join(baseDir, "bin", "codex-remote-wrapper.exe"),
		"installedRelaydBinary":  filepath.Join(baseDir, "bin", "codex-remote-relayd.exe"),
		"unknownFutureField":     map[string]interface{}{"foo": "bar"},
		"pendingUpgrade": map[string]interface{}{
			"phase":                       "prepared",
			"source":                      "release",
			"targetTrack":                 "production",
			"targetVersion":               "v1.2.3",
			"targetSlot":                  "v1.2.3",
			"targetBinaryPath":            filepath.Join(baseDir, "versions", "v1.2.3", "codex-remote.exe"),
			"gatewayID":                   "gw-1",
			"surfaceSessionID":            "sess-1",
			"chatID":                      "chat-1",
			"actorUserID":                 "u-1",
			"sourceMessageID":             "msg-1",
			"requestedAt":                 "2026-08-05T12:00:00Z",
			"resultDeliveryAttempts":      2,
			"resultDeliveryLastError":     "transient",
			"resultDeliveryNextAttemptAt": "2026-08-05T12:05:00Z",
		},
		"rollbackCandidate": map[string]interface{}{
			"version":     "v1.1.0",
			"binaryPath":  filepath.Join(baseDir, "versions", "v1.1.0", "codex-remote.exe"),
			"source":      "release",
			"fingerprint": "sha256:abc",
			"configSnapshots": []map[string]interface{}{
				{"path": filepath.Join(baseDir, ".config", "codex-remote", "config.json"), "backupPath": filepath.Join(baseDir, "backup", "config.json"), "existed": true},
			},
		},
	}
	rawBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, rawBytes, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	// Legacy config path fields still collapse to config.json.
	wantConfigPath := filepath.Join(baseDir, ".config", "codex-remote", "config.json")
	if loaded.ConfigPath != wantConfigPath {
		t.Fatalf("ConfigPath = %q, want %q", loaded.ConfigPath, wantConfigPath)
	}

	// Upgrade transaction must survive intact.
	if loaded.PendingUpgrade == nil {
		t.Fatal("PendingUpgrade lost after loading legacy state")
	}
	pending := loaded.PendingUpgrade
	if pending.Phase != PendingUpgradePhasePrepared {
		t.Fatalf("PendingUpgrade.Phase = %q, want %q", pending.Phase, PendingUpgradePhasePrepared)
	}
	if pending.TargetVersion != "v1.2.3" {
		t.Fatalf("PendingUpgrade.TargetVersion = %q, want v1.2.3", pending.TargetVersion)
	}
	wantRequestedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	if pending.RequestedAt == nil || !pending.RequestedAt.Equal(wantRequestedAt) {
		t.Fatalf("PendingUpgrade.RequestedAt = %v, want %v", pending.RequestedAt, wantRequestedAt)
	}
	if pending.ResultDeliveryAttempts != 2 {
		t.Fatalf("PendingUpgrade.ResultDeliveryAttempts = %d, want 2", pending.ResultDeliveryAttempts)
	}
	if pending.GatewayID != "gw-1" || pending.SurfaceSessionID != "sess-1" {
		t.Fatalf("PendingUpgrade delivery routing lost: gateway=%q surface=%q", pending.GatewayID, pending.SurfaceSessionID)
	}

	// Rollback candidate must survive intact.
	if loaded.RollbackCandidate == nil {
		t.Fatal("RollbackCandidate lost after loading legacy state")
	}
	rb := loaded.RollbackCandidate
	if rb.Version != "v1.1.0" {
		t.Fatalf("RollbackCandidate.Version = %q, want v1.1.0", rb.Version)
	}
	if len(rb.ConfigSnapshots) != 1 || !rb.ConfigSnapshots[0].Existed {
		t.Fatalf("RollbackCandidate.ConfigSnapshots = %#v, want 1 existed snapshot", rb.ConfigSnapshots)
	}
}

// TestUpgradeTransactionSurvivesWriteReload is the #808-E restart guard:
// after the upgrade reaches "prepared", a process restart (write + reload)
// must fully restore the transaction and rollback candidate.
func TestUpgradeTransactionSurvivesWriteReload(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	state := InstallState{
		InstanceID:    "stable",
		BaseDir:       baseDir,
		ConfigPath:    filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:     statePath,
		InstallSource: InstallSourceRelease,
		CurrentTrack:  ReleaseTrackProduction,
		PendingUpgrade: &PendingUpgrade{
			Phase:                  PendingUpgradePhasePrepared,
			Source:                 UpgradeSourceRelease,
			TargetTrack:            ReleaseTrackProduction,
			TargetVersion:          "v1.2.3",
			TargetSlot:             "v1.2.3",
			TargetBinaryPath:       filepath.Join(baseDir, "versions", "v1.2.3", "codex-remote.exe"),
			GatewayID:              "gw-1",
			SurfaceSessionID:       "sess-1",
			ChatID:                 "chat-1",
			ActorUserID:            "u-1",
			SourceMessageID:        "msg-1",
			RequestedAt:            &now,
			ResultDeliveryAttempts: 2,
		},
		RollbackCandidate: &RollbackCandidate{
			Version:     "v1.1.0",
			BinaryPath:  filepath.Join(baseDir, "versions", "v1.1.0", "codex-remote.exe"),
			Source:      InstallSourceRelease,
			Fingerprint: "sha256:abc",
			ConfigSnapshots: []ConfigSnapshot{
				{Path: filepath.Join(baseDir, ".config", "codex-remote", "config.json"), BackupPath: filepath.Join(baseDir, "backup", "config.json"), Existed: true},
			},
		},
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState after restart: %v", err)
	}

	if loaded.PendingUpgrade == nil {
		t.Fatal("PendingUpgrade lost after write+reload")
	}
	pending := loaded.PendingUpgrade
	if pending.Phase != PendingUpgradePhasePrepared {
		t.Fatalf("PendingUpgrade.Phase = %q, want %q", pending.Phase, PendingUpgradePhasePrepared)
	}
	if pending.TargetVersion != "v1.2.3" || pending.TargetSlot != "v1.2.3" {
		t.Fatalf("PendingUpgrade target lost: version=%q slot=%q", pending.TargetVersion, pending.TargetSlot)
	}
	if pending.RequestedAt == nil || !pending.RequestedAt.Equal(now) {
		t.Fatalf("PendingUpgrade.RequestedAt = %v, want %v", pending.RequestedAt, now)
	}
	if pending.ResultDeliveryAttempts != 2 {
		t.Fatalf("PendingUpgrade.ResultDeliveryAttempts = %d, want 2", pending.ResultDeliveryAttempts)
	}
	if pending.GatewayID != "gw-1" || pending.SurfaceSessionID != "sess-1" || pending.ChatID != "chat-1" || pending.ActorUserID != "u-1" || pending.SourceMessageID != "msg-1" {
		t.Fatalf("PendingUpgrade delivery routing lost: %#v", pending)
	}

	if loaded.RollbackCandidate == nil {
		t.Fatal("RollbackCandidate lost after write+reload")
	}
	rb := loaded.RollbackCandidate
	if rb.Version != "v1.1.0" || rb.Fingerprint != "sha256:abc" {
		t.Fatalf("RollbackCandidate identity lost: %#v", rb)
	}
	if len(rb.ConfigSnapshots) != 1 || rb.ConfigSnapshots[0].Path == "" || !rb.ConfigSnapshots[0].Existed {
		t.Fatalf("RollbackCandidate.ConfigSnapshots lost: %#v", rb.ConfigSnapshots)
	}

	// User preferences that cannot be probed must survive too.
	if loaded.InstallSource != InstallSourceRelease {
		t.Fatalf("InstallSource = %q, want %q", loaded.InstallSource, InstallSourceRelease)
	}
	if loaded.CurrentTrack != ReleaseTrackProduction {
		t.Fatalf("CurrentTrack = %q, want %q", loaded.CurrentTrack, ReleaseTrackProduction)
	}
}

// TestWriteStateOmitsDerivedLayoutFields is the #808-C write-path guard:
// layout facts that are derived from the state file location at load time
// (baseDir / configPath / statePath / versionsRoot) must not be persisted, so
// a moved install never inherits stale snapshots. currentSlot stays because
// it identifies the active version slot and is not derivable from the path.
func TestWriteStateOmitsDerivedLayoutFields(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	state := InstallState{
		InstanceID:        "stable",
		BaseDir:           baseDir,
		ConfigPath:        filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:         statePath,
		VersionsRoot:      filepath.Join(baseDir, ".local", "share", "codex-remote", "releases"),
		CurrentSlot:       "v1.2.3",
		CurrentBinaryPath: filepath.Join(baseDir, ".local", "share", "codex-remote", "releases", "v1.2.3", "codex-remote"),
		CurrentTrack:      ReleaseTrackProduction,
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	for _, field := range []string{"baseDir", "configPath", "statePath", "versionsRoot"} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("did not expect %q in written state: %s", field, raw)
		}
	}
	for _, field := range []string{"instanceId", "currentBinaryPath", "currentTrack", "currentSlot"} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("expected %q in written state: %s", field, raw)
		}
	}

	// A reload must restore the derived layout from the path alone.
	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState after slim write: %v", err)
	}
	if loaded.BaseDir != baseDir {
		t.Fatalf("BaseDir after reload = %q, want %q", loaded.BaseDir, baseDir)
	}
	if loaded.ConfigPath != state.ConfigPath {
		t.Fatalf("ConfigPath after reload = %q, want %q", loaded.ConfigPath, state.ConfigPath)
	}
	if loaded.VersionsRoot != state.VersionsRoot {
		t.Fatalf("VersionsRoot after reload = %q, want %q", loaded.VersionsRoot, state.VersionsRoot)
	}
	if loaded.CurrentSlot != state.CurrentSlot {
		t.Fatalf("CurrentSlot after reload = %q, want %q", loaded.CurrentSlot, state.CurrentSlot)
	}
	if loaded.StatePath != statePath {
		t.Fatalf("StatePath after reload = %q, want %q", loaded.StatePath, statePath)
	}
}

// TestWriteStateKeepsCustomConfigPath guards the user-customized config path:
// only the default derived config path is omitted on write; a non-default path
// is a user preference that must be persisted.
func TestWriteStateKeepsCustomConfigPath(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	customConfig := filepath.Join(t.TempDir(), "my-config.json")
	state := InstallState{
		InstanceID: "stable",
		BaseDir:    baseDir,
		ConfigPath: customConfig,
		StatePath:  statePath,
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var disk struct {
		ConfigPath string `json:"configPath"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if disk.ConfigPath != customConfig {
		t.Fatalf("ConfigPath in written state = %q, want %q", disk.ConfigPath, customConfig)
	}
}

// TestLoadStateDerivesLayoutFromPathOnly is the #808-C read-path guard: a
// state file that contains none of the layout fields (the new slim format)
// must load with every layout fact derived from the file location.
func TestLoadStateDerivesLayoutFromPathOnly(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte(`{"instanceId":"stable","currentBinaryPath":"/x/codex-remote"}`), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.StatePath != statePath {
		t.Fatalf("StatePath = %q, want %q", loaded.StatePath, statePath)
	}
	if loaded.BaseDir != baseDir {
		t.Fatalf("BaseDir = %q, want %q", loaded.BaseDir, baseDir)
	}
	wantConfig := filepath.Join(baseDir, ".config", "codex-remote", "config.json")
	if loaded.ConfigPath != wantConfig {
		t.Fatalf("ConfigPath = %q, want %q", loaded.ConfigPath, wantConfig)
	}
	wantVersions := filepath.Join(baseDir, ".local", "share", "codex-remote", "releases")
	if loaded.VersionsRoot != wantVersions {
		t.Fatalf("VersionsRoot = %q, want %q", loaded.VersionsRoot, wantVersions)
	}
}
