package xutil

import "testing"

func TestTruncateRunesPlainCut(t *testing.T) {
	t.Parallel()
	// Mirrors threadtitle.truncateText: normalize, cut at limit, "...".
	got := TruncateRunes("  你好 世界  ", 3, TruncateOptions{CollapseSpaces: true})
	if got != "你好 ..." {
		t.Fatalf("plain cut = %q, want %q", got, "你好 ...")
	}
	if got := TruncateRunes("短文本", 10, TruncateOptions{}); got != "短文本" {
		t.Fatalf("short text = %q, want original", got)
	}
	if got := TruncateRunes("", 5, TruncateOptions{}); got != "" {
		t.Fatalf("empty text = %q, want empty", got)
	}
	if got := TruncateRunes("abc", 0, TruncateOptions{}); got != "abc" {
		t.Fatalf("non-positive limit = %q, want original", got)
	}
}

func TestTruncateRunesNonPositiveLimitEmpty(t *testing.T) {
	t.Parallel()
	// Mirrors truncateThreadPreview: limit <= 0 yields "".
	if got := TruncateRunes("abc", 0, TruncateOptions{NonPositiveLimitEmpty: true}); got != "" {
		t.Fatalf("non-positive limit with empty option = %q, want empty", got)
	}
	if got := TruncateRunes("abc", -1, TruncateOptions{NonPositiveLimitEmpty: true}); got != "" {
		t.Fatalf("negative limit with empty option = %q, want empty", got)
	}
}

func TestTruncateRunesReserveEllipsis(t *testing.T) {
	t.Parallel()
	// Mirrors truncateThreadHistoryDetailText / truncateExecProgressSummary:
	// trim, clamp limit to 3, cut at limit-3, append "...".
	opts := TruncateOptions{ReserveEllipsis: true, MinLimit: 3}
	// Callers trim before calling; pass already-trimmed text.
	if got := TruncateRunes("0123456789", 6, opts); got != "012..." {
		t.Fatalf("reserve ellipsis = %q, want %q", got, "012...")
	}
	// limit <= 3 clamps to 3, so only 0 runes + "...".
	if got := TruncateRunes("0123456789", 2, opts); got != "..." {
		t.Fatalf("clamped limit = %q, want %q", got, "...")
	}
	// Short text returns trimmed original unchanged.
	if got := TruncateRunes("012", 10, opts); got != "012" {
		t.Fatalf("short text = %q, want %q", got, "012")
	}
}

func TestTruncateRunesCustomEllipsisTrimResult(t *testing.T) {
	t.Parallel()
	// Mirrors truncateTargetPickerGitImportLine: custom ellipsis, cut at
	// limit-1, trim result.
	opts := TruncateOptions{Ellipsis: "…", ReserveEllipsis: true, TrimResult: true}
	if got := TruncateRunes("0123456789", 6, opts); got != "01234…" {
		t.Fatalf("custom ellipsis = %q, want %q", got, "01234…")
	}
	if got := TruncateRunes("01", 10, opts); got != "01" {
		t.Fatalf("short text = %q, want %q", got, "01")
	}
	// limit <= 0 returns original (no NonPositiveLimitEmpty).
	if got := TruncateRunes("01", 0, opts); got != "01" {
		t.Fatalf("non-positive limit = %q, want original %q", got, "01")
	}
	// Trailing spaces in the cut portion are trimmed before the ellipsis,
	// matching the original git-import behavior.
	if got := TruncateRunes("01  234", 4, opts); got != "01…" {
		t.Fatalf("trim before ellipsis = %q, want %q", got, "01…")
	}
}

func TestTruncateRunesCollapseSpaces(t *testing.T) {
	t.Parallel()
	// Mirrors truncateThreadPreview / truncateThreadHistoryText whitespace
	// folding.
	got := TruncateRunes("  a   b   c  ", 4, TruncateOptions{CollapseSpaces: true, ReserveEllipsis: true, MinLimit: 3})
	if got != "a..." {
		t.Fatalf("collapsed = %q, want %q", got, "a...")
	}
	got = TruncateRunes("  a   b   c  ", 20, TruncateOptions{CollapseSpaces: true})
	if got != "a b c" {
		t.Fatalf("collapsed short = %q, want %q", got, "a b c")
	}
}

func TestTruncateRunesUnicode(t *testing.T) {
	t.Parallel()
	// Rune-based, not byte-based: CJK counts as single runes.
	if got := TruncateRunes("一二三四五", 3, TruncateOptions{}); got != "一二三..." {
		t.Fatalf("unicode cut = %q, want %q", got, "一二三...")
	}
}
