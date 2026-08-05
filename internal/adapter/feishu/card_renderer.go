package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtheme"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type cardEnvelopeVersion string

const (
	cardEnvelopeV2          cardEnvelopeVersion = "v2"
	cardTextTagPlainText                        = "plain_text"
	cardTextTagLarkMarkdown                     = "lark_md"
)

type cardDocument struct {
	Title       string
	TitleTag    string
	Subtitle    string
	SubtitleTag string
	ThemeKey    string
	Components  []cardComponent
}

type cardComponent interface {
	renderCardComponent(version cardEnvelopeVersion) map[string]any
}

type cardMarkdownComponent struct {
	Content string
}

type cardRawComponent struct {
	data map[string]any
}

func newCardDocumentWithHeader(title, titleTag, subtitle, subtitleTag, themeKey string, components ...cardComponent) *cardDocument {
	doc := &cardDocument{
		Title:       strings.TrimSpace(title),
		TitleTag:    normalizeCardTextTag(titleTag, cardTextTagPlainText),
		Subtitle:    strings.TrimSpace(subtitle),
		SubtitleTag: normalizeCardTextTag(subtitleTag, cardTextTagLarkMarkdown),
		ThemeKey:    strings.TrimSpace(themeKey),
		Components:  make([]cardComponent, 0, len(components)),
	}
	if doc.Subtitle == "" {
		doc.SubtitleTag = ""
	}
	for _, component := range components {
		if component == nil {
			continue
		}
		doc.Components = append(doc.Components, component)
	}
	return doc
}

func rawCardDocument(title, body, themeKey string, extraElements []map[string]any) *cardDocument {
	return rawCardDocumentWithHeader(title, cardTextTagPlainText, "", "", body, themeKey, extraElements)
}

func rawCardDocumentWithHeader(title, titleTag, subtitle, subtitleTag, body, themeKey string, extraElements []map[string]any) *cardDocument {
	components := make([]cardComponent, 0, len(extraElements)+1)
	if strings.TrimSpace(body) != "" {
		components = append(components, cardMarkdownComponent{Content: body})
	}
	for _, element := range extraElements {
		components = append(components, newRawCardComponent(element))
	}
	return newCardDocumentWithHeader(title, titleTag, subtitle, subtitleTag, themeKey, components...)
}

func newRawCardComponent(data map[string]any) cardComponent {
	return cardRawComponent{
		data: cloneCardMap(data),
	}
}

func (c cardMarkdownComponent) renderCardComponent(_ cardEnvelopeVersion) map[string]any {
	if strings.TrimSpace(c.Content) == "" {
		return nil
	}
	return map[string]any{
		"tag":     "markdown",
		"content": c.Content,
	}
}

func (c cardRawComponent) renderCardComponent(_ cardEnvelopeVersion) map[string]any {
	return cloneCardMap(c.data)
}

func renderOperationCard(operation Operation, version cardEnvelopeVersion) map[string]any {
	doc := operation.card
	if doc == nil {
		doc = rawCardDocumentWithHeader(
			operation.CardTitle,
			xutil.FirstNonEmpty(strings.TrimSpace(operation.CardTitleTag), cardTextTagPlainText),
			operation.CardSubtitle,
			xutil.FirstNonEmpty(strings.TrimSpace(operation.CardSubtitleTag), cardTextTagLarkMarkdown),
			operation.CardBody,
			operation.CardThemeKey,
			operation.CardElements,
		)
	}
	doc = withAttentionCardDocument(doc, operation.AttentionText, operation.AttentionUserID)
	if doc == nil {
		return nil
	}
	return stampRenderedCardCallbackSurface(
		renderCardDocument(doc, version, operation.CardUpdateMulti),
		operation.SurfaceSessionID,
	)
}

func (operation Operation) effectiveCardEnvelope() cardEnvelopeVersion {
	return cardEnvelopeV2
}

func renderCardDocument(doc *cardDocument, version cardEnvelopeVersion, updateMulti bool) map[string]any {
	if doc == nil {
		return nil
	}
	elements := make([]map[string]any, 0, len(doc.Components))
	for _, component := range doc.Components {
		if component == nil {
			continue
		}
		rendered := component.renderCardComponent(version)
		if len(rendered) == 0 {
			continue
		}
		elements = append(elements, rendered)
	}
	header := map[string]any{
		"template": cardtheme.Template(doc.ThemeKey, doc.Title),
		"title": map[string]any{
			"tag":     normalizeCardTextTag(doc.TitleTag, cardTextTagPlainText),
			"content": doc.Title,
		},
	}
	if strings.TrimSpace(doc.Subtitle) != "" {
		header["subtitle"] = map[string]any{
			"tag":     normalizeCardTextTag(doc.SubtitleTag, cardTextTagLarkMarkdown),
			"content": doc.Subtitle,
		}
	}
	_ = version
	config := map[string]any{
		"width_mode":     "fill",
		"enable_forward": true,
	}
	if updateMulti {
		config["update_multi"] = true
	}
	return map[string]any{
		"schema": "2.0",
		"config": config,
		"header": header,
		"body": map[string]any{
			"elements": elements,
		},
	}
}

func stampRenderedCardCallbackSurface(payload map[string]any, surfaceSessionID string) map[string]any {
	surfaceSessionID = strings.TrimSpace(surfaceSessionID)
	if len(payload) == 0 || surfaceSessionID == "" {
		return payload
	}
	stampCardCallbackValue(payload, surfaceSessionID)
	return payload
}

func stampCardCallbackValue(value any, surfaceSessionID string) {
	switch typed := value.(type) {
	case map[string]any:
		stampCallbackBehaviorValues(typed, surfaceSessionID)
		if callbackValue := legacyButtonCallbackValue(typed); callbackValue != nil {
			callbackValue[frontstagecontract.CardActionPayloadKeySurfaceSessionID] = surfaceSessionID
		}
		for _, raw := range typed {
			stampCardCallbackValue(raw, surfaceSessionID)
		}
	case []map[string]any:
		for _, item := range typed {
			stampCardCallbackValue(item, surfaceSessionID)
		}
	case []any:
		for _, item := range typed {
			stampCardCallbackValue(item, surfaceSessionID)
		}
	}
}

func stampCallbackBehaviorValues(element map[string]any, surfaceSessionID string) {
	switch behaviors := element["behaviors"].(type) {
	case []map[string]any:
		for _, behavior := range behaviors {
			if value := callbackValueFromBehavior(behavior); value != nil {
				value[frontstagecontract.CardActionPayloadKeySurfaceSessionID] = surfaceSessionID
			}
		}
	case []any:
		for _, raw := range behaviors {
			behavior, _ := raw.(map[string]any)
			if value := callbackValueFromBehavior(behavior); value != nil {
				value[frontstagecontract.CardActionPayloadKeySurfaceSessionID] = surfaceSessionID
			}
		}
	}
}

func callbackValueFromBehavior(behavior map[string]any) map[string]any {
	if behavior["type"] != "callback" {
		return nil
	}
	value, _ := behavior["value"].(map[string]any)
	if len(value) == 0 {
		return nil
	}
	return value
}

func legacyButtonCallbackValue(element map[string]any) map[string]any {
	if tag, _ := element["tag"].(string); tag != "button" {
		return nil
	}
	value, _ := element["value"].(map[string]any)
	if len(value) == 0 {
		return nil
	}
	return value
}

func InvalidateOperationCard(operation *Operation) {
	if operation == nil {
		return
	}
	operation.card = nil
}

func cardPlainText(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
}

func withAttentionCardDocument(doc *cardDocument, attentionText, mentionUserID string) *cardDocument {
	if doc == nil {
		return nil
	}
	attention := renderCardAttentionMarkdown(attentionText, mentionUserID)
	if attention == "" {
		return doc
	}
	components := make([]cardComponent, 0, len(doc.Components)+1)
	components = append(components, cardMarkdownComponent{Content: attention})
	components = append(components, doc.Components...)
	return newCardDocumentWithHeader(doc.Title, doc.TitleTag, doc.Subtitle, doc.SubtitleTag, doc.ThemeKey, components...)
}

func normalizeCardTextTag(tag, fallback string) string {
	switch strings.TrimSpace(tag) {
	case cardTextTagPlainText, cardTextTagLarkMarkdown:
		return strings.TrimSpace(tag)
	default:
		return strings.TrimSpace(fallback)
	}
}

func renderCardAttentionMarkdown(attentionText, mentionUserID string) string {
	mentionUserID = strings.TrimSpace(mentionUserID)
	if mentionUserID == "" {
		return ""
	}
	return "<at id=" + mentionUserID + "></at>"
}

func cloneCardMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, raw := range value {
		out[key] = cloneCardAny(raw)
	}
	return out
}

func cloneCardAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneCardMap(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardAny(item))
		}
		return out
	default:
		return typed
	}
}
