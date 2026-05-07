package control

import "testing"

func TestBuildFeishuAdminRootPageViewPlatformVisibility(t *testing.T) {
	windows := buildFeishuAdminRootPageViewForGOOS(false, "windows")
	if got := entryTitles(windows.Sections[0].Entries); len(got) != 2 {
		t.Fatalf("windows admin root management entries = %#v, want 2 entries without autostart", got)
	}

	linux := buildFeishuAdminRootPageViewForGOOS(false, "linux")
	if got := entryTitles(linux.Sections[0].Entries); len(got) != 3 {
		t.Fatalf("linux admin root management entries = %#v, want autostart to be visible", got)
	}
	if got := entryTitles(linux.Sections[0].Entries)[2]; got != "自动启动" {
		t.Fatalf("linux admin root third entry = %q, want 自动启动", got)
	}
}

func entryTitles(entries []CommandCatalogEntry) []string {
	titles := make([]string, 0, len(entries))
	for _, entry := range entries {
		titles = append(titles, entry.Title)
	}
	return titles
}
