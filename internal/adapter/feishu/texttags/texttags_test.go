package texttags

import "testing"

func TestFormatNeutralTextTagEscapesMarkup(t *testing.T) {
	got := FormatNeutralTextTag(` a < b > c & "q" `)
	want := "<text_tag color='neutral'>a &lt; b &gt; c &amp; &#34;q&#34;</text_tag>"
	if got != want {
		t.Fatalf("unexpected neutral tag: got %q want %q", got, want)
	}
}

func TestFormatCommandTextTagPreservesCommandOperators(t *testing.T) {
	got := FormatCommandTextTag(" cd web && npm test -- --run src/lib/api.test.ts ")
	want := "<text_tag color='neutral'>cd web && npm test -- --run src/lib/api.test.ts</text_tag>"
	if got != want {
		t.Fatalf("unexpected command tag: got %q want %q", got, want)
	}
}

func TestFormatInlineCodeTextTagPreservesAngleBracketsAndQuotes(t *testing.T) {
	got := FormatInlineCodeTextTag(` /model <模型> "high" 'safe' `)
	want := `<text_tag color='neutral'>/model <模型> "high" 'safe'</text_tag>`
	if got != want {
		t.Fatalf("unexpected inline code tag: got %q want %q", got, want)
	}
}

func TestFormatInlineCodeTextTagKeepsLiteralEntitiesEscaped(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "named",
			input: "&lt;text_tag&gt;",
			want:  "<text_tag color='neutral'>&amp;lt;text_tag&amp;gt;</text_tag>",
		},
		{
			name:  "numeric",
			input: "&#60;text_tag&#62;",
			want:  "<text_tag color='neutral'>&amp;#60;text_tag&amp;#62;</text_tag>",
		},
		{
			name:  "hex",
			input: "&#x3c;text_tag&#x3e;",
			want:  "<text_tag color='neutral'>&amp;#x3c;text_tag&amp;#x3e;</text_tag>",
		},
		{
			name:  "invalid numeric falls back to literal ampersand",
			input: "&#;",
			want:  "<text_tag color='neutral'>&#;</text_tag>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatInlineCodeTextTag(tc.input); got != tc.want {
				t.Fatalf("unexpected inline code tag: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRenderSystemInlineTagsRewritesInlineCodeAndSkipsFences(t *testing.T) {
	input := "请运行 `a < b > c`。\n```\n`inside fence`\n```\n再看 `\"quoted\"`。"
	got := RenderSystemInlineTags(input)
	want := "请运行 <text_tag color='neutral'>a < b > c</text_tag>。\n```\n`inside fence`\n```\n再看 <text_tag color='neutral'>\"quoted\"</text_tag>。"
	if got != want {
		t.Fatalf("unexpected rendered inline tags:\n got: %q\nwant: %q", got, want)
	}
}
