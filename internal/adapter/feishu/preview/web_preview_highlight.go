package preview

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

const (
	previewSyntaxStyleName               = "github"
	previewSyntaxClassPrefix             = "pv-"
	previewSyntaxHighlightThresholdBytes = 512 * 1024
)

var (
	previewSourceHighlighterFormatter = chromahtml.New(
		chromahtml.WithClasses(true),
		chromahtml.ClassPrefix(previewSyntaxClassPrefix),
	)
	previewSourceHighlighterStyle = previewSyntaxStyle()
	plainMarkdownPreviewRenderer  = goldmark.New(
		goldmark.WithRendererOptions(goldmarkhtml.WithHardWraps()),
	)
	highlightedMarkdownRenderer = goldmark.New(
		goldmark.WithRendererOptions(goldmarkhtml.WithHardWraps()),
		goldmark.WithExtensions(highlighting.NewHighlighting(
			highlighting.WithStyle(previewSyntaxStyleName),
			highlighting.WithGuessLanguage(false),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
				chromahtml.ClassPrefix(previewSyntaxClassPrefix),
			),
		)),
	)
	previewSyntaxCSSOnce sync.Once
	previewSyntaxCSS     string
)

var previewSourceHighlightExtensions = map[string]struct{}{
	".diff":  {},
	".go":    {},
	".htm":   {},
	".html":  {},
	".ini":   {},
	".js":    {},
	".json":  {},
	".jsx":   {},
	".patch": {},
	".py":    {},
	".sh":    {},
	".sql":   {},
	".svg":   {},
	".toml":  {},
	".ts":    {},
	".tsx":   {},
	".xml":   {},
	".yaml":  {},
	".yml":   {},
}

func previewSyntaxStyle() *chroma.Style {
	if style := styles.Get(previewSyntaxStyleName); style != nil {
		return style
	}
	return styles.Fallback
}

func previewSyntaxStylesheet() string {
	previewSyntaxCSSOnce.Do(func() {
		var buf bytes.Buffer
		if err := previewSourceHighlighterFormatter.WriteCSS(&buf, previewSourceHighlighterStyle); err == nil {
			previewSyntaxCSS = buf.String()
		}
	})
	return previewSyntaxCSS
}

func shouldHighlightMarkdownPreview(record webPreviewRecord) bool {
	return record.SizeBytes > 0 && record.SizeBytes <= previewSyntaxHighlightThresholdBytes
}

func shouldHighlightSourcePreview(record webPreviewRecord) bool {
	switch strings.TrimSpace(record.RendererKind) {
	case "text", "html_source", "svg_source":
	default:
		return false
	}
	return record.SizeBytes > 0 &&
		record.SizeBytes <= previewSyntaxHighlightThresholdBytes &&
		previewSourceLexer(record.SourcePath) != nil
}

func previewSourceLexer(sourcePath string) chroma.Lexer {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(sourcePath)))
	if _, ok := previewSourceHighlightExtensions[ext]; !ok {
		return nil
	}
	lexer := lexers.Match(filepath.Base(strings.TrimSpace(sourcePath)))
	if lexer == nil {
		return nil
	}
	return chroma.Coalesce(lexer)
}

func renderHighlightedSourcePreviewHTML(sourcePath string, content []byte) (string, error) {
	lexer := previewSourceLexer(sourcePath)
	if lexer == nil {
		return "", nil
	}
	iterator, err := lexer.Tokenise(nil, string(content))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := previewSourceHighlighterFormatter.Format(&buf, previewSourceHighlighterStyle, iterator); err != nil {
		return "", err
	}
	return `<div class="preview-syntax preview-syntax--source">` + buf.String() + `</div>`, nil
}

func renderMarkdownHTML(content []byte, highlight bool) (string, error) {
	var buf bytes.Buffer
	renderer := plainMarkdownPreviewRenderer
	if highlight {
		renderer = highlightedMarkdownRenderer
	}
	if err := renderer.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
