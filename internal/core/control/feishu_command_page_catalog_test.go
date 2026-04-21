package control

import "testing"

func TestBuildFeishuCommandPageCatalogPromotesNoticeAndSealedContract(t *testing.T) {
	view := FeishuCommandPageView{
		CommandID: "compact",
		Title:     "上下文已压缩",
		SummarySections: []FeishuCardTextSection{{
			Label: "当前会话",
			Lines: []string{"修复登录流程 (thread-1)"},
		}},
		NoticeSections: []FeishuCardTextSection{{
			Lines: []string{"当前会话的上下文已压缩完成。"},
		}},
		StatusKind:  "info",
		StatusText:  "可继续普通输入。",
		Interactive: true,
		Sealed:      true,
		RelatedButtons: []CommandCatalogButton{{
			Label:       "返回菜单",
			CommandText: "/menu",
		}},
	}

	catalog := BuildFeishuCommandPageCatalog(view)
	if !catalog.Sealed {
		t.Fatalf("expected sealed page catalog, got %#v", catalog)
	}
	if catalog.Interactive {
		t.Fatalf("expected sealed page catalog to drop interactive footer, got %#v", catalog)
	}
	if len(catalog.RelatedButtons) != 0 {
		t.Fatalf("expected sealed page catalog to clear related buttons, got %#v", catalog.RelatedButtons)
	}
	if len(catalog.BodySections) != 1 || catalog.BodySections[0].Label != "当前会话" {
		t.Fatalf("expected body sections to preserve business state, got %#v", catalog.BodySections)
	}
	if len(catalog.NoticeSections) != 2 {
		t.Fatalf("expected notice sections to include status feedback plus explicit notices, got %#v", catalog.NoticeSections)
	}
	if catalog.NoticeSections[0].Label != "说明" || catalog.NoticeSections[1].Lines[0] != "当前会话的上下文已压缩完成。" {
		t.Fatalf("unexpected notice sections: %#v", catalog.NoticeSections)
	}
	roundTrip := FeishuCommandPageViewFromCatalog("", catalog, catalog.Breadcrumbs, catalog.RelatedButtons)
	if !roundTrip.Sealed || roundTrip.Interactive {
		t.Fatalf("expected sealed round-trip view, got %#v", roundTrip)
	}
	if len(roundTrip.BodySections) != 1 || roundTrip.BodySections[0].Label != "当前会话" {
		t.Fatalf("expected round-trip body sections, got %#v", roundTrip.BodySections)
	}
	if len(roundTrip.NoticeSections) != 2 {
		t.Fatalf("expected round-trip notice sections, got %#v", roundTrip.NoticeSections)
	}
}
