package pathcompare

import "testing"

func TestSameCleanPlatformPathTrimsAndCleans(t *testing.T) {
	t.Parallel()

	if !SameCleanPlatformPath(" /tmp/work/../bin/codex ", "/tmp/bin/codex") {
		t.Fatal("expected clean platform paths to match")
	}
	if SameCleanPlatformPath("", "/tmp/bin/codex") {
		t.Fatal("expected empty path not to match")
	}
}
