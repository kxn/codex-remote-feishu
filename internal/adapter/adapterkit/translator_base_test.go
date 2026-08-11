package adapterkit

import "testing"

func TestDebugLoggerNilSink(t *testing.T) {
	t.Parallel()
	var logger DebugLogger
	// Must not panic with no sink installed.
	logger.Debugf("format %s", "arg")
	logger.SetDebugLogger(nil)
	logger.Debugf("still fine")
}

func TestDebugLoggerForwards(t *testing.T) {
	t.Parallel()
	var got []any
	logger := DebugLogger{}
	logger.SetDebugLogger(func(format string, args ...any) {
		got = append(got, append([]any{format}, args...)...)
	})
	logger.Debugf("hello %s", "world")
	if len(got) != 2 || got[0] != "hello %s" || got[1] != "world" {
		t.Fatalf("forwarded = %#v, want [hello %%s world]", got)
	}
}

func TestTranslatorBaseNextIDAndRequest(t *testing.T) {
	t.Parallel()
	var base TranslatorBase
	if got := base.NextID(); got != 0 {
		t.Fatalf("first NextID = %d, want 0", got)
	}
	if got := base.NextID(); got != 1 {
		t.Fatalf("second NextID = %d, want 1", got)
	}
	if got := base.NextRequest("init"); got != "relay-init-2" {
		t.Fatalf("NextRequest = %q, want relay-init-2", got)
	}
}

func TestTranslatorBaseInitNextID(t *testing.T) {
	t.Parallel()
	var base TranslatorBase
	base.InitNextID(1)
	if got := base.NextRequest("init"); got != "relay-init-1" {
		t.Fatalf("NextRequest after InitNextID(1) = %q, want relay-init-1", got)
	}
}
