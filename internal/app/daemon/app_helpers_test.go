package daemon

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/buildinfo"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestFormatStatusSnapshotBinaryIncludesFlavor(t *testing.T) {
	previous := buildinfo.FlavorValue
	buildinfo.FlavorValue = string(buildinfo.FlavorShipping)
	t.Cleanup(func() {
		buildinfo.FlavorValue = previous
	})

	got := formatStatusSnapshotBinary(agentproto.ServerIdentity{
		BinaryIdentity: agentproto.BinaryIdentity{
			Branch:           "master",
			Version:          "dev-9ae95f7175a5",
			BuildFingerprint: "sha256:200123456789",
		},
	})
	if !strings.Contains(got, "master / dev-9ae95f7175a5 / sha256:200") {
		t.Fatalf("formatted binary = %q, want branch/version/fingerprint", got)
	}
	if !strings.Contains(got, "flavor:shipping") {
		t.Fatalf("formatted binary = %q, want flavor suffix", got)
	}
}
