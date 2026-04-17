package feishu

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/branding"
)

type webPreviewLayout string

const (
	webPreviewLayoutDocument webPreviewLayout = "document"
	webPreviewLayoutImage    webPreviewLayout = "image"
	webPreviewLayoutPDF      webPreviewLayout = "pdf"
	webPreviewLayoutMessage  webPreviewLayout = "message"
)

type webPreviewPage struct {
	Title        string
	Notice       string
	BodyHTML     string
	Status       int
	DownloadHref string
	Layout       webPreviewLayout
}

func writeWebPreviewPage(w http.ResponseWriter, page webPreviewPage) {
	status := page.Status
	if status <= 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; frame-src 'self'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(renderWebPreviewPageHTML(page)))
}

func renderWebPreviewPageHTML(page webPreviewPage) string {
	title := strings.TrimSpace(page.Title)
	if title == "" {
		title = "文件预览"
	}
	body := strings.TrimSpace(page.BodyHTML)
	if body == "" {
		body = "<p>当前没有可展示的正文内容。</p>"
	}

	contentClass := "preview-content preview-content--document"
	bodyClass := "preview-body preview-body--document"
	switch page.Layout {
	case webPreviewLayoutImage:
		contentClass = "preview-content preview-content--image"
		bodyClass = "preview-body preview-body--image"
	case webPreviewLayoutPDF:
		contentClass = "preview-content preview-content--pdf"
		bodyClass = "preview-body preview-body--pdf"
	case webPreviewLayoutMessage:
		contentClass = "preview-content preview-content--message"
		bodyClass = "preview-body preview-body--message"
	}

	downloadHTML := ""
	if strings.TrimSpace(page.DownloadHref) != "" {
		downloadHTML = `<a class="preview-download" href="` + escapePreviewText(page.DownloadHref) + `">下载</a>`
	}
	noticeHTML := ""
	if strings.TrimSpace(page.Notice) != "" {
		noticeHTML = `<p class="preview-notice">` + escapePreviewText(page.Notice) + `</p>`
	}

	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>
html,body{height:100%%}
body{margin:0;background:#f7f4ee;color:#1b1812;font-family:"Segoe UI",ui-sans-serif,system-ui,sans-serif;overflow:hidden}
.preview-shell{height:100%%;display:flex;flex-direction:column;background:#f7f4ee}
.preview-topbar{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 14px;border-bottom:1px solid #e6ded1;background:rgba(250,248,243,.96);flex:0 0 auto}
.preview-brand{display:flex;align-items:center;gap:10px;min-width:0}
.preview-logo{display:block;width:24px;height:24px;flex:0 0 auto}
.preview-title{min-width:0;margin:0;font-size:14px;font-weight:600;line-height:1.4;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.preview-actions{display:flex;align-items:center;gap:10px;flex:0 0 auto}
.preview-download{display:inline-flex;align-items:center;justify-content:center;padding:8px 12px;border:1px solid #d8cfbf;border-radius:999px;color:#1b1812;text-decoration:none;font-size:13px;font-weight:600;background:#fffdf8}
.preview-content{flex:1 1 auto;min-height:0;overflow:auto}
.preview-content--document,.preview-content--message{padding:24px 18px 40px}
.preview-content--image{background:#f2eee6}
.preview-content--pdf{background:#e9e3d7;overflow:hidden}
.preview-body{box-sizing:border-box}
.preview-body--document,.preview-body--message{max-width:860px;margin:0 auto}
.preview-body--image{min-height:100%%;display:flex;align-items:flex-start;justify-content:center;padding:20px;box-sizing:border-box}
.preview-body--pdf{height:100%%}
.preview-notice{margin:0 0 18px;color:#5d5347;font-size:14px;line-height:1.6}
.preview-prose{line-height:1.72;word-break:break-word}
.preview-prose h1,.preview-prose h2,.preview-prose h3{line-height:1.3;margin-top:1.6em}
.preview-prose h1:first-child,.preview-prose h2:first-child,.preview-prose h3:first-child{margin-top:0}
.preview-prose p,.preview-prose ul,.preview-prose ol,.preview-prose blockquote{margin:0 0 1em}
.preview-prose pre,.preview-prose code{font-family:"SFMono-Regular","Cascadia Code","Consolas",monospace}
.preview-prose pre{overflow:auto;padding:14px 0}
.source-block,.diff-block,.summary-block{margin:0;white-space:pre-wrap;word-break:break-word;font-family:"SFMono-Regular","Cascadia Code","Consolas",monospace;font-size:13px;line-height:1.65;background:transparent;border:0;padding:0}
.source-block--numbered{display:block;white-space:normal}
.source-line{display:grid;grid-template-columns:minmax(48px,max-content) minmax(0,1fr);gap:14px;align-items:start;padding:0 0 0 2px;scroll-margin-block:45vh}
.source-line-number{display:block;color:#8a7b68;text-decoration:none;text-align:right;user-select:none}
.source-line-text{display:block;white-space:pre-wrap;word-break:break-word}
.source-line--target,.source-line:target{background:#fff1c7;border-radius:8px}
.source-column-target{background:#ffd778;border-radius:4px}
.preview-image{display:block;max-width:100%%;height:auto;object-fit:contain}
.preview-pdf{display:block;width:100%%;height:100%%;border:0;background:#fff}
@media (max-width:640px){
.preview-topbar{padding:10px 12px}
.preview-logo{width:22px;height:22px}
.preview-title{font-size:13px}
.preview-content--document,.preview-content--message{padding:18px 14px 28px}
.preview-body--image{padding:14px}
}
</style></head><body><div class="preview-shell"><header class="preview-topbar"><div class="preview-brand"><img class="preview-logo" src="%s" alt="" aria-hidden="true"><h1 class="preview-title">%s</h1></div><div class="preview-actions">%s</div></header><main class="%s"><div class="%s">%s%s</div></main></div></body></html>`,
		escapePreviewText(title),
		escapePreviewText(branding.LogoSVGDataURI()),
		escapePreviewText(title),
		downloadHTML,
		contentClass,
		bodyClass,
		noticeHTML,
		body,
	)
}
