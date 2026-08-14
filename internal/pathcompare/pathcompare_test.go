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

func TestSameCleanPlatformPathWindowsExtendedForms(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{`\\?\C:\repo`, `C:\repo`},
		{`//?/C:/repo`, `C:\repo`},
		{`\\?\UNC\server\share`, `\\server\share`},
		{`//?/UNC/server/share`, `\\server\share`},
		{`C:\Repo`, `c:/repo`},
	}
	for _, pair := range pairs {
		if !SameCleanPlatformPath(pair[0], pair[1]) {
			t.Fatalf("expected %q and %q to match", pair[0], pair[1])
		}
	}
	if SameCleanPlatformPath(`C:\repo`, `D:\repo`) {
		t.Fatal("expected different drives not to match")
	}
	if SameCleanPlatformPath(`\\server\share`, `\\other\share`) {
		t.Fatal("expected different UNC servers not to match")
	}
}
