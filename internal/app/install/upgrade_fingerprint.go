package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// UpgradeStateFingerprint identifies the install authority relevant to an
// upgrade confirmation; it does not confer permission to start an upgrade.
func UpgradeStateFingerprint(state InstallState) string {
	raw, _ := json.Marshal([]any{state.CurrentVersion, state.CurrentBinaryPath, state.CurrentTrack, state.PendingUpgrade})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
