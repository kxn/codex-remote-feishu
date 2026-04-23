package daemon

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func catalogSummaryText(catalog *control.FeishuPageView) string {
	if catalog == nil {
		return ""
	}
	normalized := control.NormalizeFeishuPageView(*catalog)
	sections := control.BuildFeishuPageBodySections(normalized)
	parts := []string{}
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label != "" {
			parts = append(parts, normalized.Label)
		}
		parts = append(parts, normalized.Lines...)
	}
	for _, section := range control.BuildFeishuPageNoticeSections(normalized) {
		normalized := section.Normalized()
		if normalized.Label != "" {
			parts = append(parts, normalized.Label)
		}
		parts = append(parts, normalized.Lines...)
	}
	return strings.Join(parts, "\n")
}

func catalogFromUIEvent(t *testing.T, event eventcontract.Event) *control.FeishuPageView {
	t.Helper()
	if event.PageView != nil {
		page := control.NormalizeFeishuPageView(*event.PageView)
		catalog := control.NormalizeFeishuPageView(control.FeishuPageView{
			CommandID:       page.CommandID,
			Title:           page.Title,
			MessageID:       page.MessageID,
			TrackingKey:     page.TrackingKey,
			ThemeKey:        page.ThemeKey,
			Patchable:       page.Patchable,
			Breadcrumbs:     append([]control.CommandCatalogBreadcrumb(nil), page.Breadcrumbs...),
			SummarySections: append([]control.FeishuCardTextSection(nil), page.SummarySections...),
			BodySections:    append([]control.FeishuCardTextSection(nil), page.BodySections...),
			NoticeSections:  append([]control.FeishuCardTextSection(nil), page.NoticeSections...),
			StatusKind:      page.StatusKind,
			StatusText:      page.StatusText,
			Interactive:     page.Interactive,
			Sealed:          page.Sealed,
			DisplayStyle:    page.DisplayStyle,
			Sections:        append([]control.CommandCatalogSection(nil), page.Sections...),
			RelatedButtons:  append([]control.CommandCatalogButton(nil), page.RelatedButtons...),
		})
		return &catalog
	}
	t.Fatalf("expected page view event, got %#v", event)
	return nil
}

func assertCatalogUsesPlainTextContracts(t *testing.T, catalog *control.FeishuPageView) {
	t.Helper()
	if catalog == nil {
		t.Fatal("expected command catalog")
	}
	normalizedPage := control.NormalizeFeishuPageView(*catalog)
	for _, section := range normalizedPage.SummarySections {
		normalized := section.Normalized()
		assertCatalogTextAvoidsFeishuMarkdown(t, "summary section label", normalized.Label)
		for _, line := range normalized.Lines {
			assertCatalogTextAvoidsFeishuMarkdown(t, "summary section line", line)
		}
	}
	for _, section := range normalizedPage.BodySections {
		normalized := section.Normalized()
		assertCatalogTextAvoidsFeishuMarkdown(t, "body section label", normalized.Label)
		for _, line := range normalized.Lines {
			assertCatalogTextAvoidsFeishuMarkdown(t, "body section line", line)
		}
	}
	for _, section := range normalizedPage.NoticeSections {
		normalized := section.Normalized()
		assertCatalogTextAvoidsFeishuMarkdown(t, "notice section label", normalized.Label)
		for _, line := range normalized.Lines {
			assertCatalogTextAvoidsFeishuMarkdown(t, "notice section line", line)
		}
	}
	for _, section := range normalizedPage.Sections {
		for _, entry := range section.Entries {
			assertCatalogTextAvoidsFeishuMarkdown(t, "entry title", entry.Title)
			assertCatalogTextAvoidsFeishuMarkdown(t, "entry description", entry.Description)
			for _, command := range entry.Commands {
				assertCatalogTextAvoidsFeishuMarkdown(t, "entry command", command)
			}
			for _, example := range entry.Examples {
				assertCatalogTextAvoidsFeishuMarkdown(t, "entry example", example)
			}
		}
	}
}

func assertCatalogTextAvoidsFeishuMarkdown(t *testing.T, label, text string) {
	t.Helper()
	if strings.Contains(strings.TrimSpace(text), "<text_tag") {
		t.Fatalf("expected %s to stay in plain-text fields, got %q", label, text)
	}
}

func catalogHasOpenURLButton(catalog *control.FeishuPageView, label, openURL string) bool {
	if catalog == nil {
		return false
	}
	for _, section := range control.NormalizeFeishuPageView(*catalog).Sections {
		for _, entry := range section.Entries {
			for _, button := range entry.Buttons {
				if button.Kind == control.CommandCatalogButtonOpenURL &&
					button.Label == label &&
					button.OpenURL == openURL {
					return true
				}
			}
		}
	}
	return false
}

func operationCardText(operation feishu.Operation) string {
	parts := []string{}
	if body := strings.TrimSpace(operation.CardBody); body != "" {
		parts = append(parts, body)
	}
	for _, element := range operation.CardElements {
		appendOperationElementText(&parts, element)
	}
	return strings.Join(parts, "\n")
}

func appendOperationElementText(parts *[]string, element map[string]any) {
	if len(element) == 0 {
		return
	}
	switch strings.TrimSpace(stringAnyValue(element["tag"])) {
	case "markdown":
		if content := strings.TrimSpace(stringAnyValue(element["content"])); content != "" {
			*parts = append(*parts, content)
		}
	case "div":
		if textNode, _ := element["text"].(map[string]any); len(textNode) != 0 {
			if content := strings.TrimSpace(stringAnyValue(textNode["content"])); content != "" {
				*parts = append(*parts, content)
			}
		}
	case "button":
		if textNode, _ := element["text"].(map[string]any); len(textNode) != 0 {
			if content := strings.TrimSpace(stringAnyValue(textNode["content"])); content != "" {
				*parts = append(*parts, content)
			}
		}
	case "column_set":
		columns, _ := element["columns"].([]map[string]any)
		for _, column := range columns {
			children, _ := column["elements"].([]map[string]any)
			for _, child := range children {
				appendOperationElementText(parts, child)
			}
		}
	case "form":
		children, _ := element["elements"].([]map[string]any)
		for _, child := range children {
			appendOperationElementText(parts, child)
		}
	}
}

func operationHasOpenURLButton(operation feishu.Operation, label, openURL string) bool {
	for _, button := range operationCardButtons(operation) {
		textNode, _ := button["text"].(map[string]any)
		if strings.TrimSpace(stringAnyValue(textNode["content"])) != label {
			continue
		}
		behaviors, _ := button["behaviors"].([]map[string]any)
		for _, behavior := range behaviors {
			if strings.TrimSpace(stringAnyValue(behavior["type"])) != "open_url" {
				continue
			}
			if strings.TrimSpace(stringAnyValue(behavior["default_url"])) == openURL {
				return true
			}
		}
	}
	return false
}

func stringAnyValue(value any) string {
	text, _ := value.(string)
	return text
}
