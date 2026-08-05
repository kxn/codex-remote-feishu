package preview

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
	"github.com/pmezard/go-difflib/difflib"
)

var (
	errPreviewRecordExpired              = fmt.Errorf("preview record expired")
	errPreviewArtifactExpired            = fmt.Errorf("preview artifact expired")
	previewDiffFirstThresholdBytes int64 = 2 * 1024 * 1024
)

func (p *DriveMarkdownPreviewer) ServeWebPreview(w http.ResponseWriter, r *http.Request, scopePublicID, previewID string, download bool) bool {
	if p == nil {
		return false
	}
	if strings.TrimSpace(previewID) == "" {
		if download {
			return false
		}
		writeWebPreviewPage(w, webPreviewPage{
			Title:    "选择具体文件查看预览",
			Notice:   "这个地址只代表一组可复用的预览授权，不会列出目录内容。",
			BodyHTML: `<p>请回到原消息并点击其中的具体文件链接进入预览。</p>`,
			Status:   http.StatusOK,
			Layout:   webPreviewLayoutMessage,
		})
		return true
	}
	location := previewLocationFromRequest(r)
	current, previous, err := p.loadWebPreviewArtifactsForServe(scopePublicID, previewID)
	switch {
	case err == nil:
		if current != nil && strings.TrimSpace(current.Record.RendererKind) == turnDiffPreviewRendererKind {
			var serveErr error
			if download {
				serveErr = serveTurnDiffPreviewDownloadHTTP(w, current)
			} else {
				serveErr = serveTurnDiffPreviewPageHTTP(w, current)
			}
			if serveErr == nil {
				return true
			}
			writeWebPreviewPage(w, webPreviewPage{
				Title:    "预览暂不可用",
				Notice:   "服务暂时无法读取这份预览快照。",
				BodyHTML: "<p>你可以稍后重试，或重新生成新的产物链接。</p>",
				Status:   http.StatusInternalServerError,
				Layout:   webPreviewLayoutMessage,
			})
			return true
		}
		if download {
			serveWebPreviewDownloadHTTP(w, r, current)
		} else {
			writeWebPreviewPage(w, buildWebPreviewPage(current, previous, previewRelativeDownloadHref(previewID), location))
		}
		return true
	case err == errPreviewRecordExpired || err == errPreviewArtifactExpired:
		writeWebPreviewPage(w, webPreviewPage{
			Title:    "预览已过期",
			Notice:   "这份预览快照已经过期，请重新生成新的产物链接。",
			BodyHTML: "<p>当前链接不再对应可访问的预览内容。</p>",
			Status:   http.StatusGone,
			Layout:   webPreviewLayoutMessage,
		})
		return true
	case os.IsNotExist(err):
		return false
	default:
		writeWebPreviewPage(w, webPreviewPage{
			Title:    "预览暂不可用",
			Notice:   "服务暂时无法读取这份预览快照。",
			BodyHTML: "<p>你可以稍后重试，或重新生成新的产物链接。</p>",
			Status:   http.StatusInternalServerError,
			Layout:   webPreviewLayoutMessage,
		})
		return true
	}
}

func previewRelativeDownloadHref(previewID string) string {
	previewID = strings.TrimSpace(previewID)
	if previewID == "" {
		return ""
	}
	return previewID + "/download"
}

func (p *DriveMarkdownPreviewer) loadWebPreviewArtifactsForServe(scopePublicID, previewID string) (*webPreviewArtifact, *webPreviewArtifact, error) {
	p.webPreviewMu.Lock()
	defer p.webPreviewMu.Unlock()

	manifest, err := p.loadWebPreviewScopeManifest(scopePublicID)
	if err != nil {
		return nil, nil, err
	}
	if manifest == nil || manifest.ScopePublicID != scopePublicID {
		return nil, nil, os.ErrNotExist
	}
	record := manifest.Records[strings.TrimSpace(previewID)]
	if record == nil {
		return nil, nil, os.ErrNotExist
	}
	now := p.nowFn().UTC()
	if !record.ExpiresAt.IsZero() && record.ExpiresAt.Before(now) {
		return nil, nil, errPreviewRecordExpired
	}
	current, err := p.loadWebPreviewArtifact(scopePublicID, previewID)
	if err != nil {
		return nil, nil, err
	}
	record.LastUsedAt = now
	record.ExpiresAt = now.Add(defaultPreviewRecordTTL)
	manifest.LastUsedAt = now
	_ = p.saveWebPreviewScopeManifest(manifest)
	_ = p.touchPreviewBlob(record.BlobKey, now)

	var previous *webPreviewArtifact
	if previousID := strings.TrimSpace(record.PreviousID); previousID != "" {
		if previousRecord := manifest.Records[previousID]; previousRecord != nil {
			if content, readErr := os.ReadFile(p.previewBlobPath(previousRecord.BlobKey)); readErr == nil {
				previous = &webPreviewArtifact{
					ScopePublicID: scopePublicID,
					PreviewID:     previousID,
					Record:        *previousRecord,
					Content:       content,
				}
			}
		}
	}
	return current, previous, nil
}

func allowsLineAddressedWebPreview(rendererKind string) bool {
	switch strings.TrimSpace(rendererKind) {
	case "markdown", "text", "html_source", "svg_source":
		return true
	default:
		return false
	}
}

func previewLocationFromRequest(r *http.Request) PreviewLocation {
	if r == nil || r.URL == nil {
		return PreviewLocation{}
	}
	return parsePreviewLocationValue(r.URL.Query().Get("loc"))
}

func parsePreviewLocationValue(value string) PreviewLocation {
	value = strings.TrimSpace(value)
	if value == "" {
		return PreviewLocation{}
	}
	if matched := previewHashLocationSuffixPattern.FindStringSubmatch("#" + value); len(matched) == 3 {
		return PreviewLocation{
			Line:   parsePreviewLocationNumber(matched[1]),
			Column: parsePreviewLocationNumber(matched[2]),
		}
	}
	return PreviewLocation{}
}

func previewLocationNotice(location PreviewLocation, rendererKind string) string {
	if !location.Valid() {
		return ""
	}
	base := "已定位到第 " + previewItoa(location.Line) + " 行"
	if location.Column > 0 {
		base += " 第 " + previewItoa(location.Column) + " 列"
	}
	if strings.TrimSpace(rendererKind) == "markdown" {
		return base + "。为保证行号对应源码，当前按源码视图展示。"
	}
	return base + "。"
}

func appendPreviewInlineQuery(downloadHref string) string {
	downloadHref = strings.TrimSpace(downloadHref)
	if downloadHref == "" {
		return ""
	}
	if strings.Contains(downloadHref, "?") {
		return downloadHref + "&inline=1"
	}
	return downloadHref + "?inline=1"
}

func serveWebPreviewDownloadHTTP(w http.ResponseWriter, r *http.Request, artifact *webPreviewArtifact) {
	if artifact == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	record := artifact.Record
	mediaType := xutil.FirstNonEmpty(strings.TrimSpace(record.MIMEType), detectContentType(artifact.Content))
	disposition := "attachment"
	if strings.TrimSpace(r.URL.Query().Get("inline")) == "1" && allowsInlinePreviewDownload(record.RendererKind) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, sanitizePreviewDownloadName(record.DisplayName)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Content)
}

func allowsInlinePreviewDownload(rendererKind string) bool {
	switch strings.TrimSpace(rendererKind) {
	case "image", "pdf":
		return true
	default:
		return false
	}
}

func shouldRenderDiffFirst(record webPreviewRecord, previous *webPreviewArtifact) bool {
	if previous == nil || record.SizeBytes <= previewDiffFirstThresholdBytes {
		return false
	}
	switch strings.TrimSpace(record.RendererKind) {
	case "markdown", "text", "html_source", "svg_source":
		return true
	default:
		return false
	}
}

func shouldRenderSummaryOnly(record webPreviewRecord) bool {
	if record.SizeBytes <= previewDiffFirstThresholdBytes {
		return false
	}
	switch strings.TrimSpace(record.RendererKind) {
	case "markdown", "text", "html_source", "svg_source":
		return true
	default:
		return false
	}
}

func renderNumberedSourcePreviewHTML(content []byte, location PreviewLocation) string {
	lines := splitPreviewSourceLines(string(content))
	if len(lines) == 0 {
		lines = []string{""}
	}
	var builder strings.Builder
	builder.WriteString(`<div class="source-block source-block--numbered">`)
	for index, line := range lines {
		lineNumber := index + 1
		lineID := "L" + previewItoa(lineNumber)
		lineClass := "source-line"
		if location.Line == lineNumber {
			lineClass += " source-line--target"
		}
		builder.WriteString(`<div id="`)
		builder.WriteString(lineID)
		builder.WriteString(`" class="`)
		builder.WriteString(lineClass)
		builder.WriteString(`"><a class="source-line-number" href="#`)
		builder.WriteString(lineID)
		builder.WriteString(`">`)
		builder.WriteString(previewItoa(lineNumber))
		builder.WriteString(`</a><span class="source-line-text">`)
		builder.WriteString(renderPreviewSourceLineText(line, lineNumber, location))
		builder.WriteString(`</span></div>`)
	}
	builder.WriteString(`</div>`)
	return builder.String()
}

func splitPreviewSourceLines(text string) []string {
	if text == "" {
		return []string{""}
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func renderPreviewSourceLineText(line string, lineNumber int, location PreviewLocation) string {
	if location.Line != lineNumber || location.Column <= 0 {
		return escapePreviewText(line)
	}
	runes := []rune(line)
	columnIndex := location.Column - 1
	if columnIndex < 0 || columnIndex >= len(runes) {
		return escapePreviewText(line)
	}
	prefix := string(runes[:columnIndex])
	suffix := string(runes[columnIndex:])
	return escapePreviewText(prefix) + `<span class="source-column-target">` + escapePreviewText(suffix) + `</span>`
}

func renderTextSummaryHTML(content []byte) string {
	text := string(content)
	lines := strings.Split(text, "\n")
	limit := 60
	if len(lines) > limit {
		lines = lines[:limit]
	}
	snippet := strings.Join(lines, "\n")
	if strings.TrimSpace(snippet) == "" {
		snippet = "(文件内容为空)"
	}
	return `<p>文件体积较大，页面不直接展开完整正文。下面是开头摘要：</p><pre class="summary-block">` + escapePreviewText(snippet) + `</pre>`
}

func renderDiffPreviewHTML(previous, current *webPreviewArtifact) string {
	if previous == nil || current == nil {
		return `<p>当前没有可比较的上一版内容。</p>`
	}
	diffText, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(previous.Content)),
		B:        difflib.SplitLines(string(current.Content)),
		FromFile: "previous",
		ToFile:   "current",
		Context:  3,
	})
	if err != nil {
		return renderTextSummaryHTML(current.Content)
	}
	if strings.TrimSpace(diffText) == "" {
		diffText = "No textual changes."
	}
	return `<pre class="diff-block">` + escapePreviewText(diffText) + `</pre>`
}

func escapePreviewText(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
	)
	return replacer.Replace(value)
}

func sanitizePreviewDownloadName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "preview.bin"
	}
	name = strings.NewReplacer("\n", "-", "\r", "-", "\"", "-", "\\", "-", "/", "-").Replace(name)
	return name
}

func detectContentType(content []byte) string {
	if len(content) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(content)
}

func (p *DriveMarkdownPreviewer) touchPreviewBlob(blobKey string, when time.Time) error {
	path := p.previewBlobPath(blobKey)
	if when.IsZero() {
		when = time.Now().UTC()
	}
	if err := os.Chtimes(path, when, when); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func blobSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func blobModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}

func isPreviewManifestFile(path string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(path)), ".json")
}
