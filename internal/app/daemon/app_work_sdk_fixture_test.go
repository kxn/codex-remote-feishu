package daemon

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// The Feishu SDK's default token cache is process-wide and keyed by App ID,
// independent of the controlled server URL. Use a new App identity even on
// repeated runs of the same test in one process.
func isolatedSDKFixtureAppID(t *testing.T, role string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(t.TempDir() + ":" + role))
	return fmt.Sprintf("cli_%x", digest[:12])
}
