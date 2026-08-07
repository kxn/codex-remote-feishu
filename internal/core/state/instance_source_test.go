package state

import "testing"

func TestIsInstanceSource(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   InstanceSource
		ok     bool
	}{
		{"headless exact", "headless", InstanceSourceHeadless, true},
		{"headless upper", "HEADLESS", InstanceSourceHeadless, true},
		{"headless mixed case", "HeadLess", InstanceSourceHeadless, true},
		{"headless padded", "  headless ", InstanceSourceHeadless, true},
		{"vscode exact", "vscode", InstanceSourceVSCode, true},
		{"vscode upper", "VSCode", InstanceSourceVSCode, true},
		{"empty is not vscode", "", InstanceSourceVSCode, false},
		{"empty is not headless", "", InstanceSourceHeadless, false},
		{"unknown is not headless", "desktop", InstanceSourceHeadless, false},
		{"unknown is not vscode", "desktop", InstanceSourceVSCode, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsInstanceSource(tc.source, tc.want); got != tc.ok {
				t.Fatalf("IsInstanceSource(%q, %q) = %v, want %v", tc.source, tc.want, got, tc.ok)
			}
		})
	}
}

func TestNormalizeInstanceSource(t *testing.T) {
	cases := []struct {
		in   string
		want InstanceSource
	}{
		{"headless", InstanceSourceHeadless},
		{"  HEADLESS ", InstanceSourceHeadless},
		{"vscode", InstanceSourceVSCode},
		{"", InstanceSource("")},
		{"desktop", InstanceSource("desktop")},
	}
	for _, tc := range cases {
		if got := NormalizeInstanceSource(tc.in); got != tc.want {
			t.Fatalf("NormalizeInstanceSource(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
