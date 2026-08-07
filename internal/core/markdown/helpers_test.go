package markdown

import (
	"testing"
)

func TestSplitFenceSegmentsEmpty(t *testing.T) {
	if got := SplitFenceSegments(""); got != nil {
		t.Fatalf("empty: got %v, want nil", got)
	}
}

func TestSplitFenceSegmentsPlainText(t *testing.T) {
	segments := SplitFenceSegments("hello world")
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	if segments[0].Fenced {
		t.Fatal("expected non-fenced segment")
	}
	if segments[0].Text != "hello world" {
		t.Fatalf("text = %q, want %q", segments[0].Text, "hello world")
	}
}

func TestSplitFenceSegmentsBacktickFence(t *testing.T) {
	text := "before\n```\ncode\n```\nafter"
	segments := SplitFenceSegments(text)
	if len(segments) != 3 {
		t.Fatalf("got %d segments, want 3", len(segments))
	}
	if segments[0].Fenced || segments[0].Text != "before\n" {
		t.Fatalf("seg[0] = fenced:%v text:%q", segments[0].Fenced, segments[0].Text)
	}
	if !segments[1].Fenced || segments[1].Text != "```\ncode\n```\n" {
		t.Fatalf("seg[1] = fenced:%v text:%q", segments[1].Fenced, segments[1].Text)
	}
	if segments[2].Fenced || segments[2].Text != "after" {
		t.Fatalf("seg[2] = fenced:%v text:%q", segments[2].Fenced, segments[2].Text)
	}
}

func TestSplitFenceSegmentsTildeFence(t *testing.T) {
	text := "~~~python\nprint('hi')\n~~~"
	segments := SplitFenceSegments(text)
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	if !segments[0].Fenced {
		t.Fatal("expected fenced segment for tilde fence")
	}
}

func TestSplitFenceSegmentsLongerClosingFence(t *testing.T) {
	// Closing fence with more backticks than opening is valid.
	text := "```\ncode\n````\n"
	segments := SplitFenceSegments(text)
	if len(segments) != 1 || !segments[0].Fenced {
		t.Fatalf("expected single fenced segment, got %d segments", len(segments))
	}
}

func TestSplitFenceSegmentsIndentedOpeningFence(t *testing.T) {
	// Up to 3 spaces of indentation is allowed for fenced code blocks.
	text := "   ```\ncode\n   ```\n"
	segments := SplitFenceSegments(text)
	if len(segments) != 1 || !segments[0].Fenced {
		t.Fatalf("expected single fenced segment for indented fence, got %d", len(segments))
	}
}

func TestSplitFenceSegmentsMultipleFences(t *testing.T) {
	text := "a\n```\nc1\n```\nb\n~~~\nc2\n~~~\nc"
	segments := SplitFenceSegments(text)
	// Expect: non-fenced, fenced, non-fenced, fenced, non-fenced
	if len(segments) != 5 {
		t.Fatalf("got %d segments, want 5", len(segments))
	}
	if segments[0].Fenced || segments[1].Fenced != true || segments[2].Fenced || segments[3].Fenced != true || segments[4].Fenced {
		t.Fatal("fence pattern mismatch")
	}
}

func TestFenceMarkerBackticks(t *testing.T) {
	char, count, ok := FenceMarker("```go\n")
	if !ok || char != '`' || count != 3 {
		t.Fatalf("FenceMarker(\"```go\\n\") = %c, %d, %v", char, count, ok)
	}
}

func TestFenceMarkerTildes(t *testing.T) {
	char, count, ok := FenceMarker("~~~~\n")
	if !ok || char != '~' || count != 4 {
		t.Fatalf("FenceMarker(\"~~~~\\n\") = %c, %d, %v", char, count, ok)
	}
}

func TestFenceMarkerTooShort(t *testing.T) {
	_, _, ok := FenceMarker("``\n")
	if ok {
		t.Fatal("expected false for 2-char fence")
	}
}

func TestFenceMarkerIndented(t *testing.T) {
	char, count, ok := FenceMarker("   ```\n")
	if !ok || char != '`' || count != 3 {
		t.Fatalf("FenceMarker(\"   ```\\n\") = %c, %d, %v", char, count, ok)
	}
}

func TestFenceMarkerPlainText(t *testing.T) {
	_, _, ok := FenceMarker("hello\n")
	if ok {
		t.Fatal("expected false for plain text")
	}
}

func TestConsecutiveByteRun(t *testing.T) {
	if got := ConsecutiveByteRun("aaabbb", 0, 'a'); got != 3 {
		t.Fatalf("ConsecutiveByteRun(aaa) = %d, want 3", got)
	}
	if got := ConsecutiveByteRun("aaabbb", 3, 'b'); got != 3 {
		t.Fatalf("ConsecutiveByteRun(bbb) = %d, want 3", got)
	}
	if got := ConsecutiveByteRun("abc", 0, 'x'); got != 0 {
		t.Fatalf("ConsecutiveByteRun(nomatch) = %d, want 0", got)
	}
	if got := ConsecutiveByteRun("", 0, 'a'); got != 0 {
		t.Fatalf("ConsecutiveByteRun(empty) = %d, want 0", got)
	}
}

func TestClosingBacktickRun(t *testing.T) {
	// `` `foo` `` — find closing `` at position 7
	text := "``foo``"
	got := ClosingBacktickRun(text, 4, 2)
	if got != 5 {
		t.Fatalf("ClosingBacktickRun = %d, want 5", got)
	}
}

func TestClosingBacktickRunNotFound(t *testing.T) {
	text := "no closing here"
	got := ClosingBacktickRun(text, 0, 3)
	if got != -1 {
		t.Fatalf("ClosingBacktickRun = %d, want -1", got)
	}
}

func TestParseMarkdownLinkAtSimple(t *testing.T) {
	text := "[click](https://example.com) end"
	end, label, target, ok := ParseMarkdownLinkAt(text, 0)
	if !ok {
		t.Fatal("ParseMarkdownLinkAt failed")
	}
	if label != "click" {
		t.Fatalf("label = %q, want %q", label, "click")
	}
	if target != "https://example.com" {
		t.Fatalf("target = %q, want %q", target, "https://example.com")
	}
	if end != 28 {
		t.Fatalf("end = %d, want 28", end)
	}
}

func TestParseMarkdownLinkAtNestedParens(t *testing.T) {
	// This is the key behavioral difference from the old final_card_markdown version.
	text := "[link](url_(foo)) end"
	end, label, target, ok := ParseMarkdownLinkAt(text, 0)
	if !ok {
		t.Fatal("ParseMarkdownLinkAt failed for nested parens")
	}
	if label != "link" {
		t.Fatalf("label = %q, want %q", label, "link")
	}
	if target != "url_(foo)" {
		t.Fatalf("target = %q, want %q", target, "url_(foo)")
	}
	if end != 17 {
		t.Fatalf("end = %d, want 17", end)
	}
}

func TestParseMarkdownLinkAtDeepNestedParens(t *testing.T) {
	text := "[x](a(b(c)))"
	end, label, target, ok := ParseMarkdownLinkAt(text, 0)
	if !ok {
		t.Fatal("failed for deep nested parens")
	}
	if label != "x" || target != "a(b(c))" || end != len(text) {
		t.Fatalf("label=%q target=%q end=%d", label, target, end)
	}
}

func TestParseMarkdownLinkAtEscapedParen(t *testing.T) {
	text := `[x](url\)here)`
	end, label, target, ok := ParseMarkdownLinkAt(text, 0)
	if !ok {
		t.Fatal("failed for escaped paren")
	}
	if label != "x" || target != `url\)here` {
		t.Fatalf("label=%q target=%q", label, target)
	}
	if end != len(text) {
		t.Fatalf("end = %d, want %d", end, len(text))
	}
}

func TestParseMarkdownLinkAtNoClosingParen(t *testing.T) {
	text := "[link](unclosed"
	_, _, _, ok := ParseMarkdownLinkAt(text, 0)
	if ok {
		t.Fatal("expected false for unclosed paren")
	}
}

func TestParseMarkdownLinkAtNoBracket(t *testing.T) {
	text := "not a link"
	_, _, _, ok := ParseMarkdownLinkAt(text, 0)
	if ok {
		t.Fatal("expected false for non-link text")
	}
}

func TestParseMarkdownLinkAtMissingParen(t *testing.T) {
	text := "[label] no parens"
	_, _, _, ok := ParseMarkdownLinkAt(text, 0)
	if ok {
		t.Fatal("expected false when paren is missing")
	}
}

func TestParseMarkdownLinkAtEmptyLabel(t *testing.T) {
	text := "[](target)"
	end, label, target, ok := ParseMarkdownLinkAt(text, 0)
	if !ok {
		t.Fatal("failed for empty label")
	}
	if label != "" || target != "target" || end != len(text) {
		t.Fatalf("label=%q target=%q end=%d", label, target, end)
	}
}

func TestParseMarkdownLinkAtStartOffset(t *testing.T) {
	text := "xx[label](url)"
	end, label, target, ok := ParseMarkdownLinkAt(text, 2)
	if !ok {
		t.Fatal("failed with start offset")
	}
	if label != "label" || target != "url" || end != len(text) {
		t.Fatalf("label=%q target=%q end=%d", label, target, end)
	}
}

func TestParseMarkdownLinkAtNegativeStart(t *testing.T) {
	_, _, _, ok := ParseMarkdownLinkAt("[x](y)", -1)
	if ok {
		t.Fatal("expected false for negative start")
	}
}
