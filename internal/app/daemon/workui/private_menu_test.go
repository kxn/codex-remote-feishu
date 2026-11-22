package workui

import (
	"encoding/json"
	"testing"
)

func TestPrivateMenuLayoutIsOptInAndKeepsNavigationBeforeContent(t *testing.T) {
	for _, menu := range []bool{false, true} {
		panel := Panel{PrivateMenu: menu, Title: "页", Breadcrumb: "菜单首页 › 页", Navigation: []Button{{Label: "返回", Action: "back"}}, Paragraphs: []string{"当前值"}, Buttons: []Button{{Label: "短", Action: "short"}, {Label: "较长的选项", Action: "long"}}, Footer: []Button{{Label: "常用操作", Action: "common"}}}
		raw, err := Card("view-menu", panel)
		if err != nil {
			t.Fatal(err)
		}
		var card struct {
			Body struct {
				Elements []map[string]any `json:"elements"`
			} `json:"body"`
		}
		if err = json.Unmarshal(raw, &card); err != nil {
			t.Fatal(err)
		}
		elements := card.Body.Elements
		if menu {
			if elements[1]["tag"] != "button" || elements[len(elements)-2]["tag"] != "hr" {
				t.Fatal("menu navigation/footer order changed")
			}
		} else if len(elements) != 3 || elements[0]["tag"] != "div" {
			t.Fatal("business card layout changed")
		}
		count := 0
		for _, element := range elements {
			if element["tag"] != "button" {
				continue
			}
			count++
			if menu && element["width"] != "fill" {
				t.Fatal("private menu button is not equal width")
			}
			if !menu && element["width"] != nil {
				t.Fatal("private layout leaked into business card")
			}
			behavior := element["behaviors"].([]any)[0].(map[string]any)
			if behavior["value"].(map[string]any)["team_view"] != "view-menu" {
				t.Fatal("callback lost durable view identity")
			}
		}
		if menu && count != 4 {
			t.Fatalf("buttons=%d", count)
		}
	}
}
