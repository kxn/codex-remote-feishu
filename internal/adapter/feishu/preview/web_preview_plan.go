package preview

import (
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func buildWebPreviewPage(current, previous *webPreviewArtifact, downloadHref string, location PreviewLocation) webPreviewPage {
	record := current.Record
	page := webPreviewPage{
		Title:        xutil.FirstNonEmpty(strings.TrimSpace(record.DisplayName), "文件预览"),
		Status:       http.StatusOK,
		DownloadHref: strings.TrimSpace(downloadHref),
		Layout:       webPreviewLayoutDocument,
	}
	input := webPreviewRenderInput{
		Current:      current,
		Previous:     previous,
		DownloadHref: page.DownloadHref,
		PageTitle:    page.Title,
		Location:     location,
	}
	plan := planWebPreviewRender(input)
	page.Layout = plan.Layout
	bodyHTML, planNoticeParts := executeWebPreviewRenderPlan(plan, input)
	page.BodyHTML = bodyHTML
	page.Notice = joinWebPreviewNoticeParts(planNoticeParts)
	return page
}

type webPreviewSourcePlanOptions struct {
	AllowDiffFirst  bool
	AllowSummary    bool
	SafetyNotice    string
	HighlightSource bool
}

func planWebPreviewRender(input webPreviewRenderInput) webPreviewRenderPlan {
	if input.Current == nil {
		return webPreviewRenderPlan{
			Layout: webPreviewLayoutMessage,
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererUnsupportedMessage,
			}},
		}
	}
	switch strings.TrimSpace(input.Current.Record.RendererKind) {
	case "markdown":
		return planMarkdownWebPreview(input)
	case "text":
		return planSourceLikeWebPreview(input, webPreviewSourcePlanOptions{
			AllowDiffFirst:  true,
			AllowSummary:    true,
			HighlightSource: shouldHighlightSourcePreview(input.Current.Record),
		})
	case "html_source":
		return planSourceLikeWebPreview(input, webPreviewSourcePlanOptions{
			AllowDiffFirst:  true,
			SafetyNotice:    previewRendererSafetyNotice(input.Current.Record.RendererKind),
			HighlightSource: shouldHighlightSourcePreview(input.Current.Record),
		})
	case "svg_source":
		return planSourceLikeWebPreview(input, webPreviewSourcePlanOptions{
			SafetyNotice:    previewRendererSafetyNotice(input.Current.Record.RendererKind),
			HighlightSource: shouldHighlightSourcePreview(input.Current.Record),
		})
	case "image":
		return webPreviewRenderPlan{
			Layout: webPreviewLayoutImage,
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererImage,
			}},
		}
	case "pdf":
		return webPreviewRenderPlan{
			Layout: webPreviewLayoutPDF,
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererPDF,
			}},
		}
	default:
		return webPreviewRenderPlan{
			Layout:      webPreviewLayoutMessage,
			NoticeParts: []string{"当前文件类型不提供在线正文渲染，可直接下载原文件。"},
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererUnsupportedMessage,
			}},
		}
	}
}

func planMarkdownWebPreview(input webPreviewRenderInput) webPreviewRenderPlan {
	if input.Current == nil {
		return webPreviewRenderPlan{}
	}
	if previewLocationEnabled(input.Location, input.Current.Record.RendererKind) {
		return webPreviewRenderPlan{
			Layout:      webPreviewLayoutDocument,
			NoticeParts: []string{previewLocationNotice(input.Location, input.Current.Record.RendererKind)},
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererNumberedSource,
			}},
		}
	}
	if shouldRenderDiffFirst(input.Current.Record, input.Previous) {
		return webPreviewRenderPlan{
			Layout:      webPreviewLayoutDocument,
			NoticeParts: []string{"文件较大，默认展示与上一版的差异。"},
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererDiff,
			}},
		}
	}
	if shouldRenderSummaryOnly(input.Current.Record) {
		return webPreviewRenderPlan{
			Layout:      webPreviewLayoutDocument,
			NoticeParts: []string{"文件较大，当前没有可用的上一版差异，页面只展示摘要与下载入口。"},
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererSummary,
			}},
		}
	}
	return webPreviewRenderPlan{
		Layout: webPreviewLayoutDocument,
		Choices: []webPreviewRendererChoice{
			{RendererKey: webPreviewRendererMarkdownProse},
			{
				RendererKey: webPreviewRendererNumberedSource,
				NoticeParts: []string{"Markdown 渲染失败，已回退为源码视图。"},
			},
		},
	}
}

func planSourceLikeWebPreview(input webPreviewRenderInput, options webPreviewSourcePlanOptions) webPreviewRenderPlan {
	record := input.Current.Record
	if options.AllowDiffFirst && shouldRenderDiffFirst(record, input.Previous) {
		return webPreviewRenderPlan{
			Layout:      webPreviewLayoutDocument,
			NoticeParts: appendWebPreviewNoticePart(nil, options.SafetyNotice, "文件较大，默认展示与上一版的差异。"),
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererDiff,
			}},
		}
	}
	if options.AllowSummary && shouldRenderSummaryOnly(record) {
		return webPreviewRenderPlan{
			Layout:      webPreviewLayoutDocument,
			NoticeParts: appendWebPreviewNoticePart(nil, options.SafetyNotice, "文件较大，当前没有可用的上一版差异，页面只展示摘要与下载入口。"),
			Choices: []webPreviewRendererChoice{{
				RendererKey: webPreviewRendererSummary,
			}},
		}
	}

	noticeParts := appendWebPreviewNoticePart(nil, previewLocationNoticeForRecord(input.Location, record.RendererKind), options.SafetyNotice)
	choices := make([]webPreviewRendererChoice, 0, 2)
	if options.HighlightSource {
		choices = append(choices, webPreviewRendererChoice{
			RendererKey: webPreviewRendererNumberedHighlightedSource,
		})
	}
	choices = append(choices, webPreviewRendererChoice{
		RendererKey: webPreviewRendererNumberedSource,
	})
	return webPreviewRenderPlan{
		Layout:      webPreviewLayoutDocument,
		NoticeParts: noticeParts,
		Choices:     choices,
	}
}

func previewLocationEnabled(location PreviewLocation, rendererKind string) bool {
	return location.Valid() && allowsLineAddressedWebPreview(rendererKind)
}

func previewLocationNoticeForRecord(location PreviewLocation, rendererKind string) string {
	if !previewLocationEnabled(location, rendererKind) {
		return ""
	}
	return previewLocationNotice(location, rendererKind)
}

func previewRendererSafetyNotice(rendererKind string) string {
	switch strings.TrimSpace(rendererKind) {
	case "html_source":
		return "出于安全考虑，HTML 以源码方式展示，不会在页面内直接执行。"
	case "svg_source":
		return "出于安全考虑，SVG 以源码方式展示，不会作为同源文档直接渲染。"
	default:
		return ""
	}
}

func appendWebPreviewNoticePart(parts []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return parts
}

func joinWebPreviewNoticeParts(parts []string) string {
	filtered := appendWebPreviewNoticePart(nil, parts...)
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, "")
}
