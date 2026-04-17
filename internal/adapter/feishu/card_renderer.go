package feishu

import "strings"

type cardEnvelopeVersion string

const (
	cardEnvelopeLegacy cardEnvelopeVersion = "legacy"
	cardEnvelopeV2     cardEnvelopeVersion = "v2"
)

type cardDocument struct {
	Title      string
	ThemeKey   string
	Components []cardComponent
}

type cardComponent interface {
	renderCardComponent(version cardEnvelopeVersion) map[string]any
}

type cardMarkdownComponent struct {
	Content string
}

type cardRawComponent struct {
	legacy map[string]any
	v2     map[string]any
}

func newCardDocument(title, themeKey string, components ...cardComponent) *cardDocument {
	doc := &cardDocument{
		Title:      strings.TrimSpace(title),
		ThemeKey:   strings.TrimSpace(themeKey),
		Components: make([]cardComponent, 0, len(components)),
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
	components := make([]cardComponent, 0, len(extraElements)+1)
	if strings.TrimSpace(body) != "" {
		components = append(components, cardMarkdownComponent{Content: body})
	}
	for _, element := range extraElements {
		components = append(components, newRawCardComponent(element))
	}
	return newCardDocument(title, themeKey, components...)
}

func legacyCardDocument(title, body, themeKey string, extraElements []map[string]any) *cardDocument {
	return rawCardDocument(title, body, themeKey, extraElements)
}

func newRawCardComponent(data map[string]any) cardComponent {
	return cardRawComponent{
		legacy: cloneCardMap(data),
		v2:     cloneCardMap(data),
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

func (c cardRawComponent) renderCardComponent(version cardEnvelopeVersion) map[string]any {
	if version == cardEnvelopeV2 && len(c.v2) != 0 {
		return cloneCardMap(c.v2)
	}
	return cloneCardMap(c.legacy)
}

func renderOperationCard(operation Operation, version cardEnvelopeVersion) map[string]any {
	doc := operation.card
	if doc == nil {
		doc = rawCardDocument(operation.CardTitle, operation.CardBody, operation.CardThemeKey, operation.CardElements)
	}
	if doc == nil {
		return nil
	}
	return renderCardDocument(doc, version, operation.CardUpdateMulti)
}

func (operation Operation) ordinaryCardEnvelope() cardEnvelopeVersion {
	if operation.cardEnvelope == cardEnvelopeLegacy {
		return cardEnvelopeLegacy
	}
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
		"template": cardTemplate(doc.ThemeKey, doc.Title),
		"title": map[string]any{
			"tag":     "plain_text",
			"content": doc.Title,
		},
	}
	switch version {
	case cardEnvelopeV2:
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
	default:
		config := map[string]any{
			"wide_screen_mode": true,
			"enable_forward":   true,
		}
		if updateMulti {
			config["update_multi"] = true
		}
		return map[string]any{
			"config":   config,
			"header":   header,
			"elements": elements,
		}
	}
}

func cardPlainText(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
}

func cardCallbackButtonElement(label, buttonType string, value map[string]any, disabled bool, width string) map[string]any {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	buttonType = strings.TrimSpace(buttonType)
	if buttonType == "" {
		buttonType = "default"
	}
	button := map[string]any{
		"tag":      "button",
		"type":     buttonType,
		"text":     cardPlainText(label),
		"disabled": disabled,
	}
	if strings.TrimSpace(width) != "" {
		button["width"] = strings.TrimSpace(width)
	}
	if len(value) != 0 {
		button["behaviors"] = []map[string]any{{
			"type":  "callback",
			"value": cloneCardMap(value),
		}}
	}
	return button
}

func cardFormSubmitButtonElement(label string, value map[string]any) map[string]any {
	button := cardFormActionButtonElement(label, "primary", value, false, "")
	if len(button) == 0 {
		return nil
	}
	return button
}

func cardFormActionButtonElement(label, buttonType string, value map[string]any, disabled bool, width string) map[string]any {
	button := cardCallbackButtonElement(label, buttonType, value, disabled, width)
	if len(button) == 0 {
		return nil
	}
	button["name"] = "submit"
	button["form_action_type"] = "submit"
	return button
}

func cardButtonGroupElement(buttons []map[string]any) map[string]any {
	filtered := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		if len(button) == 0 {
			continue
		}
		filtered = append(filtered, cloneCardMap(button))
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		columns := make([]map[string]any, 0, len(filtered))
		for _, button := range filtered {
			columns = append(columns, map[string]any{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "top",
				"elements":       []map[string]any{button},
			})
		}
		return map[string]any{
			"tag":                "column_set",
			"flex_mode":          "flow",
			"horizontal_spacing": "small",
			"columns":            columns,
		}
	}
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

func cloneCardMaps(values []map[string]any) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, cloneCardMap(value))
	}
	return out
}
