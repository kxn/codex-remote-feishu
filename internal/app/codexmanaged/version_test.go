package codexmanaged

import "testing"

func TestStrictVersionAndSemanticOrdering(t *testing.T) {
	for _, text := range []string{"", "v0.159.3", "0.0159.3", "0.159", "0.159.3/escape", "0.159.3-01", "0.159.3-alpha..1", "18446744073709551616.0.0"} {
		if _, err := parseVersion(text); err == nil {
			t.Fatalf("accepted invalid version %q", text)
		}
	}
	ordered := []string{"0.159.3-alpha", "0.159.3-alpha.1", "0.159.3-alpha.2", "0.159.3-alpha.10", "0.159.3-beta", "0.159.3", "0.160.0-alpha.1", "1.0.0"}
	for i := 1; i < len(ordered); i++ {
		older, err := parseVersion(ordered[i-1])
		if err != nil {
			t.Fatal(err)
		}
		newer, err := parseVersion(ordered[i])
		if err != nil {
			t.Fatal(err)
		}
		if !newer.newerThan(older) || older.newerThan(newer) {
			t.Fatalf("wrong order %s < %s", older.text, newer.text)
		}
	}
	plain, _ := parseVersion("0.159.3")
	build, err := parseVersion("0.159.3+build.1")
	if err != nil || build.newerThan(plain) || plain.newerThan(build) {
		t.Fatal("build metadata changed precedence")
	}
}
