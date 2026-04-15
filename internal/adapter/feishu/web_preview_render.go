package feishu

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/yuin/goldmark"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

var (
	errPreviewRecordExpired              = fmt.Errorf("preview record expired")
	previewDiffFirstThresholdBytes int64 = 2 * 1024 * 1024
	markdownPreviewRenderer              = goldmark.New(goldmark.WithRendererOptions(goldmarkhtml.WithHardWraps()))
)

type webPreviewPage struct {
	Title        string
	TypeLabel    string
	Notice       string
	BodyHTML     string
	Status       int
	DownloadHref string
}

func (p *DriveMarkdownPreviewer) ServeWebPreview(w http.ResponseWriter, r *http.Request, scopePublicID, previewID string, download bool) bool {
	if p == nil {
		return false
	}
	if strings.TrimSpace(previewID) == "" {
		if download {
			return false
		}
		writeWebPreviewPage(w, webPreviewPage{
			Title:     "选择具体文件查看预览",
			TypeLabel: "Snapshot",
			Notice:    "这个地址只代表一组可复用的预览授权，不会列出目录内容。",
			BodyHTML:  `<p>请回到原消息并点击其中的具体文件链接进入预览。</p>`,
			Status:    http.StatusOK,
		})
		return true
	}
	current, previous, err := p.loadWebPreviewArtifactsForServe(scopePublicID, previewID)
	switch {
	case err == nil:
		if download {
			serveWebPreviewDownloadHTTP(w, r, current)
		} else {
			writeWebPreviewPage(w, buildWebPreviewPage(current, previous))
		}
		return true
	case err == errPreviewRecordExpired:
		writeWebPreviewPage(w, webPreviewPage{
			Title:        "预览已过期",
			TypeLabel:    "Snapshot",
			Notice:       "这份预览快照已经过期，请重新生成新的产物链接。",
			BodyHTML:     "<p>当前链接不再对应可访问的预览内容。</p>",
			Status:       http.StatusGone,
			DownloadHref: "",
		})
		return true
	case os.IsNotExist(err):
		return false
	default:
		writeWebPreviewPage(w, webPreviewPage{
			Title:        "预览暂不可用",
			TypeLabel:    "Snapshot",
			Notice:       "服务暂时无法读取这份预览快照。",
			BodyHTML:     "<p>你可以稍后重试，或重新生成新的产物链接。</p>",
			Status:       http.StatusInternalServerError,
			DownloadHref: "",
		})
		return true
	}
}

func (p *DriveMarkdownPreviewer) loadWebPreviewArtifactsForServe(scopePublicID, previewID string) (*webPreviewArtifact, *webPreviewArtifact, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

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
	record.LastUsedAt = now
	record.ExpiresAt = now.Add(defaultPreviewRecordTTL)
	manifest.LastUsedAt = now
	_ = p.saveWebPreviewScopeManifest(manifest)
	_ = p.touchPreviewBlob(record.BlobKey, now)

	current, err := p.loadWebPreviewArtifact(scopePublicID, previewID)
	if err != nil {
		return nil, nil, err
	}

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

func buildWebPreviewPage(current, previous *webPreviewArtifact) webPreviewPage {
	record := current.Record
	page := webPreviewPage{
		Title:        firstNonEmpty(strings.TrimSpace(record.DisplayName), "文件预览"),
		TypeLabel:    previewTypeLabel(record),
		Status:       http.StatusOK,
		DownloadHref: "./download",
	}

	switch strings.TrimSpace(record.RendererKind) {
	case "markdown":
		if shouldRenderDiffFirst(record, previous) {
			page.TypeLabel = "Diff"
			page.Notice = "文件较大，默认展示与上一版的差异。"
			page.BodyHTML = renderDiffPreviewHTML(previous, current)
			return page
		}
		if shouldRenderSummaryOnly(record) {
			page.Notice = "文件较大，当前没有可用的上一版差异，页面只展示摘要与下载入口。"
			page.BodyHTML = renderTextSummaryHTML(current.Content)
			return page
		}
		html, err := renderMarkdownHTML(current.Content)
		if err != nil {
			page.Notice = "Markdown 渲染失败，已回退为源码视图。"
			page.BodyHTML = renderSourcePreviewHTML(current.Content)
			return page
		}
		page.BodyHTML = `<article class="preview-prose">` + html + `</article>`
		return page
	case "text":
		if shouldRenderDiffFirst(record, previous) {
			page.TypeLabel = "Diff"
			page.Notice = "文件较大，默认展示与上一版的差异。"
			page.BodyHTML = renderDiffPreviewHTML(previous, current)
			return page
		}
		if shouldRenderSummaryOnly(record) {
			page.Notice = "文件较大，当前没有可用的上一版差异，页面只展示摘要与下载入口。"
			page.BodyHTML = renderTextSummaryHTML(current.Content)
			return page
		}
		page.BodyHTML = renderSourcePreviewHTML(current.Content)
		return page
	case "html_source":
		page.Notice = "出于安全考虑，HTML 以源码方式展示，不会在页面内直接执行。"
		if shouldRenderDiffFirst(record, previous) {
			page.TypeLabel = "Diff"
			page.BodyHTML = renderDiffPreviewHTML(previous, current)
			return page
		}
		page.BodyHTML = renderSourcePreviewHTML(current.Content)
		return page
	case "svg_source":
		page.Notice = "出于安全考虑，SVG 以源码方式展示，不会作为同源文档直接渲染。"
		page.BodyHTML = renderSourcePreviewHTML(current.Content)
		return page
	case "image":
		page.BodyHTML = `<figure class="asset-frame"><img src="./download?inline=1" alt="预览图片" /></figure>`
		return page
	case "pdf":
		page.BodyHTML = `<div class="asset-frame pdf-frame"><iframe src="./download?inline=1" title="PDF 预览"></iframe></div><p class="asset-help">如果当前浏览器无法内嵌 PDF，可直接下载原文件。</p>`
		return page
	default:
		page.Notice = "当前文件类型不提供在线正文渲染，可直接下载原文件。"
		page.BodyHTML = `<p>这份文件保留了快照下载能力，但当前不会在页面内直接展开。</p>`
		return page
	}
}

func serveWebPreviewDownloadHTTP(w http.ResponseWriter, r *http.Request, artifact *webPreviewArtifact) {
	if artifact == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	record := artifact.Record
	mediaType := firstNonEmpty(strings.TrimSpace(record.MIMEType), detectContentType(artifact.Content))
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

func writeWebPreviewPage(w http.ResponseWriter, page webPreviewPage) {
	status := page.Status
	if status <= 0 {
		status = http.StatusOK
	}
	body := page.BodyHTML
	if strings.TrimSpace(body) == "" {
		body = "<p>当前没有可展示的正文内容。</p>"
	}
	downloadHTML := ""
	if strings.TrimSpace(page.DownloadHref) != "" {
		downloadHTML = `<a class="primary-action" href="` + escapePreviewText(page.DownloadHref) + `">下载原文件</a>`
	}
	noticeHTML := ""
	if strings.TrimSpace(page.Notice) != "" {
		noticeHTML = `<p class="notice">` + escapePreviewText(page.Notice) + `</p>`
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; frame-src 'self'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(
		w,
		`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>
body{margin:0;background:#f5f1e8;color:#1f1c16;font-family:"Segoe UI",ui-sans-serif,system-ui,sans-serif}
main{max-width:980px;margin:0 auto;padding:20px 16px 40px}
.hero{background:linear-gradient(135deg,#fdfbf6,#efe4d0);border:1px solid #dbc8ab;border-radius:18px;padding:18px 18px 16px;box-shadow:0 10px 30px rgba(63,45,24,.08)}
.eyebrow{margin:0 0 6px;font-size:13px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:#8a6840}
h1{margin:0;font-size:28px;line-height:1.2}
.meta{margin:10px 0 0;color:#6f604d}
.actions{margin-top:14px}
.primary-action{display:inline-block;padding:10px 14px;border-radius:999px;background:#164b48;color:#fff;text-decoration:none;font-weight:700}
.notice{margin:18px 0 0;color:#5f513f}
.panel{margin-top:18px;background:#fffdf8;border:1px solid #e5d6bf;border-radius:16px;padding:16px;overflow:hidden}
.preview-prose{line-height:1.7}
.preview-prose h1,.preview-prose h2,.preview-prose h3{line-height:1.3}
.source-block,.diff-block,.summary-block{margin:0;white-space:pre-wrap;word-break:break-word;background:#fbf7ef;border:1px solid #eadcc7;border-radius:12px;padding:16px;overflow:auto}
.asset-frame{background:#faf6ee;border:1px solid #e8dcc8;border-radius:14px;padding:10px;display:flex;justify-content:center;align-items:center;min-height:220px}
.asset-frame img{max-width:100%%;height:auto;border-radius:8px}
.pdf-frame iframe{width:100%%;min-height:72vh;border:0;border-radius:8px;background:#fff}
.asset-help{color:#6a5b47}
@media (max-width:640px){main{padding:14px 12px 28px}h1{font-size:23px}.hero{padding:16px}}
</style></head><body><main><section class="hero"><p class="eyebrow">文件预览</p><h1>%s</h1><p class="meta">类型：%s</p>%s%s</section><section class="panel">%s</section></main></body></html>`,
		escapePreviewText(page.Title),
		escapePreviewText(page.Title),
		escapePreviewText(firstNonEmpty(page.TypeLabel, "文件")),
		downloadHTML,
		noticeHTML,
		body,
	)
}

func previewTypeLabel(record webPreviewRecord) string {
	switch strings.TrimSpace(record.RendererKind) {
	case "markdown":
		return "Markdown"
	case "text":
		return "文本"
	case "html_source":
		return "HTML 源码"
	case "svg_source":
		return "SVG 源码"
	case "image":
		return "图片"
	case "pdf":
		return "PDF"
	default:
		return "下载"
	}
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

func renderMarkdownHTML(content []byte) (string, error) {
	var buf bytes.Buffer
	if err := markdownPreviewRenderer.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderSourcePreviewHTML(content []byte) string {
	return `<pre class="source-block">` + escapePreviewText(string(content)) + `</pre>`
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

func previewScopeRootPageHTML(scopePublicID string) string {
	title := "预览链接需要具体文件"
	body := `<p>这个地址只代表一组可复用的预览授权，不会列出目录内容。</p><p>请从原消息里的具体文件链接进入预览。</p>`
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title></head><body style="margin:0;background:#f5f1e8;color:#1f1c16;font-family:'Segoe UI',ui-sans-serif,system-ui,sans-serif"><main style="max-width:760px;margin:0 auto;padding:28px 16px"><p style="margin:0 0 8px;color:#8a6840;font-size:13px;font-weight:700;letter-spacing:.08em;text-transform:uppercase">文件预览</p><h1 style="margin:0 0 14px">%s</h1>%s</main></body></html>`, escapePreviewText(title), escapePreviewText(title), body)
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
