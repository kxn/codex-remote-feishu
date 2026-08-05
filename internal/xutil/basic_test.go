package xutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty(); got != "" {
		t.Errorf("no args = %q, want empty", got)
	}
	if got := FirstNonEmpty("", "  ", "a", "b"); got != "a" {
		t.Errorf("first non-empty = %q, want a", got)
	}
	if got := FirstNonEmpty("  a  "); got != "a" {
		t.Errorf("trim case = %q, want a", got)
	}
	if got := FirstNonEmpty("", " ", "\t"); got != "" {
		t.Errorf("all blank = %q, want empty", got)
	}
}

func TestMaxInt(t *testing.T) {
	if got := MaxInt(1, 2); got != 2 {
		t.Errorf("MaxInt(1,2) = %d, want 2", got)
	}
	if got := MaxInt(3, 2); got != 3 {
		t.Errorf("MaxInt(3,2) = %d, want 3", got)
	}
	if got := MaxInt(-1, -2); got != -1 {
		t.Errorf("MaxInt(-1,-2) = %d, want -1", got)
	}
}
