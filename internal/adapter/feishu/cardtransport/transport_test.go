package cardtransport

import "testing"

func TestBuildCardUsesSharedThemeTemplate(t *testing.T) {
	card := RenderInteractiveCardPayload("工作中", "", "progress", nil, true)
	header, _ := card["header"].(map[string]any)
	if got := header["template"]; got != "wathet" {
		t.Fatalf("expected progress theme to render wathet template, got %#v", card)
	}
}
