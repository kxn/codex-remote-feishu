package feishu

import (
	"strings"
	"testing"
)

func containsAll(body string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(body, part) {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cardElementButtons(t *testing.T, element map[string]any) []map[string]any {
	t.Helper()
	switch element["tag"] {
	case "button":
		return []map[string]any{element}
	case "column_set":
		columns, _ := element["columns"].([]map[string]any)
		buttons := make([]map[string]any, 0, len(columns))
		for _, column := range columns {
			elements, _ := column["elements"].([]map[string]any)
			if len(elements) == 0 {
				continue
			}
			buttons = append(buttons, elements[0])
		}
		if len(buttons) == 0 {
			t.Fatalf("expected buttons inside column_set, got %#v", element)
		}
		return buttons
	default:
		t.Fatalf("expected button or column_set, got %#v", element)
		return nil
	}
}

func assertNoLegacyCardModelMarkers(t *testing.T, values []map[string]any) {
	t.Helper()
	for _, value := range values {
		assertNoLegacyCardModelMarkersAny(t, value)
	}
}

func assertNoLegacyCardModelMarkersAny(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		if tag, _ := typed["tag"].(string); tag == "action" {
			t.Fatalf("unexpected legacy action container in production card model: %#v", typed)
		}
		if actionType, ok := typed["action_type"]; ok && actionType != nil {
			t.Fatalf("unexpected legacy action_type in production card model: %#v", typed)
		}
		for _, child := range typed {
			assertNoLegacyCardModelMarkersAny(t, child)
		}
	case []map[string]any:
		for _, child := range typed {
			assertNoLegacyCardModelMarkersAny(t, child)
		}
	case []any:
		for _, child := range typed {
			assertNoLegacyCardModelMarkersAny(t, child)
		}
	}
}

func assertNoDuplicateNamedCardElements(t *testing.T, value any) {
	t.Helper()
	seen := map[string]string{}
	assertNoDuplicateNamedCardElementsAny(t, value, seen)
}

func assertNoDuplicateNamedCardElementsAny(t *testing.T, value any, seen map[string]string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		name, _ := typed["name"].(string)
		tag, _ := typed["tag"].(string)
		if name != "" {
			current := tag
			if current == "" {
				current = "unknown"
			}
			if previous, ok := seen[name]; ok {
				t.Fatalf("duplicate card element name %q: seen on %s and %s: %#v", name, previous, current, typed)
			}
			seen[name] = current
		}
		for _, child := range typed {
			assertNoDuplicateNamedCardElementsAny(t, child, seen)
		}
	case []map[string]any:
		for _, child := range typed {
			assertNoDuplicateNamedCardElementsAny(t, child, seen)
		}
	case []any:
		for _, child := range typed {
			assertNoDuplicateNamedCardElementsAny(t, child, seen)
		}
	}
}

func assertRenderedCardPayloadBasicInvariants(t *testing.T, payload map[string]any) {
	t.Helper()
	if payload == nil {
		t.Fatal("expected rendered card payload, got nil")
	}
	if payload["schema"] != "2.0" {
		return
	}
	body, _ := payload["body"].(map[string]any)
	if body == nil {
		t.Fatalf("expected V2 body, got %#v", payload)
	}
	elements, _ := body["elements"].([]map[string]any)
	if elements == nil {
		t.Fatalf("expected V2 body elements, got %#v", payload)
	}
	assertNoDuplicateNamedCardElements(t, elements)
}

func cardButtonLabel(t *testing.T, button map[string]any) string {
	t.Helper()
	textValue, _ := button["text"].(map[string]any)
	label, _ := textValue["content"].(string)
	if label == "" {
		t.Fatalf("expected button label, got %#v", button)
	}
	return label
}

func cardButtonPayload(t *testing.T, button map[string]any) map[string]any {
	t.Helper()
	if value, _ := button["value"].(map[string]any); len(value) != 0 {
		return value
	}
	behaviors, _ := button["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "callback" {
		t.Fatalf("expected callback payload on button, got %#v", button)
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	if len(value) == 0 {
		t.Fatalf("expected callback value payload, got %#v", button)
	}
	return value
}

func renderedV2BodyElements(t *testing.T, operation Operation) []map[string]any {
	t.Helper()
	if operation.cardEnvelope != cardEnvelopeV2 || operation.card == nil {
		t.Fatalf("expected structured V2 operation, got %#v", operation)
	}
	payload := renderOperationCard(operation, operation.ordinaryCardEnvelope())
	if payload["schema"] != "2.0" {
		t.Fatalf("expected V2 schema, got %#v", payload)
	}
	assertRenderedCardPayloadBasicInvariants(t, payload)
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}

func renderedV2CardText(t *testing.T, operation Operation) string {
	t.Helper()
	elements := renderedV2BodyElements(t, operation)
	parts := make([]string, 0, len(elements))
	for _, element := range elements {
		if content := cardTextContent(element); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

func containsRenderedTag(elements []map[string]any, tag string) bool {
	for _, element := range elements {
		if element["tag"] == tag {
			return true
		}
	}
	return false
}

func renderedButtonCallbackValue(t *testing.T, button map[string]any) map[string]any {
	t.Helper()
	if button["tag"] != "button" {
		t.Fatalf("expected rendered V2 button, got %#v", button)
	}
	if button["value"] != nil {
		t.Fatalf("expected rendered V2 button to move callback payload into behaviors, got %#v", button)
	}
	behaviors, _ := button["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "callback" {
		t.Fatalf("expected one callback behavior, got %#v", button)
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	return value
}

func renderedColumnButtons(t *testing.T, element map[string]any) []map[string]any {
	t.Helper()
	if element["tag"] != "column_set" {
		t.Fatalf("expected rendered V2 column_set, got %#v", element)
	}
	columns, _ := element["columns"].([]map[string]any)
	buttons := make([]map[string]any, 0, len(columns))
	for _, column := range columns {
		elements, _ := column["elements"].([]map[string]any)
		if len(elements) != 1 || elements[0]["tag"] != "button" {
			t.Fatalf("expected one button per V2 column, got %#v", column)
		}
		buttons = append(buttons, elements[0])
	}
	return buttons
}
