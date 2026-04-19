package daemon

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func catalogSummaryText(catalog *control.FeishuDirectCommandCatalog) string {
	if catalog == nil {
		return ""
	}
	if len(catalog.SummarySections) == 0 {
		return strings.TrimSpace(catalog.Summary)
	}
	parts := []string{}
	for _, section := range catalog.SummarySections {
		normalized := section.Normalized()
		if normalized.Label != "" {
			parts = append(parts, normalized.Label)
		}
		parts = append(parts, normalized.Lines...)
	}
	return strings.Join(parts, "\n")
}

func assertCatalogUsesNonLegacyContracts(t *testing.T, catalog *control.FeishuDirectCommandCatalog) {
	t.Helper()
	if catalog == nil {
		t.Fatal("expected command catalog")
	}
	if catalog.LegacySummaryMarkdown {
		t.Fatalf("expected non-legacy summary contract: %#v", catalog)
	}
	for _, section := range catalog.Sections {
		for _, entry := range section.Entries {
			if entry.LegacyMarkdown {
				t.Fatalf("expected non-legacy entry contract: %#v", entry)
			}
		}
	}
}

func catalogHasOpenURLButton(catalog *control.FeishuDirectCommandCatalog, label, openURL string) bool {
	if catalog == nil {
		return false
	}
	for _, section := range catalog.Sections {
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
