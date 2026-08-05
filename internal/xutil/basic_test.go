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

func TestBoolPtrBoolValue(t *testing.T) {
	p := BoolPtr(true)
	if p == nil || *p != true {
		t.Errorf("BoolPtr(true) = %v, want non-nil true", p)
	}
	if !BoolValue(p) {
		t.Errorf("BoolValue(BoolPtr(true)) = false, want true")
	}
	if BoolValue(nil) {
		t.Errorf("BoolValue(nil) = true, want false")
	}
	f := false
	if BoolValue(&f) {
		t.Errorf("BoolValue(&false) = true, want false")
	}
}

func TestStringPtrStringValue(t *testing.T) {
	p := StringPtr("x")
	if p == nil || *p != "x" {
		t.Errorf("StringPtr(x) = %v, want non-nil x", p)
	}
	if got := StringValue(p); got != "x" {
		t.Errorf("StringValue(StringPtr(x)) = %q, want x", got)
	}
	if got := StringValue(nil); got != "" {
		t.Errorf("StringValue(nil) = %q, want empty", got)
	}
	s := "y"
	if got := StringValue(&s); got != "y" {
		t.Errorf("StringValue(&y) = %q, want y", got)
	}
}

func TestContainsString(t *testing.T) {
	values := []string{"a", "b", "c"}
	if !ContainsString(values, "b") {
		t.Errorf("ContainsString(values, b) = false, want true")
	}
	if ContainsString(values, "z") {
		t.Errorf("ContainsString(values, z) = true, want false")
	}
	if ContainsString(nil, "a") {
		t.Errorf("ContainsString(nil, a) = true, want false")
	}
}
