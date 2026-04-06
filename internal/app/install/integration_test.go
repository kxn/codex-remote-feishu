package install

import "testing"

func TestDefaultIntegrations(t *testing.T) {
	tests := map[string][]WrapperIntegrationMode{
		"linux":   {IntegrationEditorSettings, IntegrationManagedShim},
		"darwin":  {IntegrationEditorSettings},
		"windows": {IntegrationEditorSettings},
	}
	for goos, want := range tests {
		got := DefaultIntegrations(goos)
		if len(got) != len(want) {
			t.Fatalf("DefaultIntegrations(%q) len=%d, want %d", goos, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("DefaultIntegrations(%q)[%d]=%q, want %q", goos, i, got[i], want[i])
			}
		}
	}
}

func TestParseIntegrations(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		goos  string
		want  []WrapperIntegrationMode
		isErr bool
	}{
		{
			name: "auto on linux",
			raw:  "auto",
			goos: "linux",
			want: []WrapperIntegrationMode{IntegrationEditorSettings, IntegrationManagedShim},
		},
		{
			name: "both alias",
			raw:  "both",
			goos: "darwin",
			want: []WrapperIntegrationMode{IntegrationEditorSettings, IntegrationManagedShim},
		},
		{
			name: "comma list dedupes",
			raw:  "managed_shim,editor_settings,managed_shim",
			goos: "linux",
			want: []WrapperIntegrationMode{IntegrationManagedShim, IntegrationEditorSettings},
		},
		{
			name:  "unsupported",
			raw:   "unknown",
			goos:  "linux",
			isErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIntegrations(tt.raw, tt.goos)
			if tt.isErr {
				if err == nil {
					t.Fatalf("ParseIntegrations(%q, %q) expected error", tt.raw, tt.goos)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseIntegrations(%q, %q): %v", tt.raw, tt.goos, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseIntegrations(%q, %q) len=%d, want %d", tt.raw, tt.goos, len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("ParseIntegrations(%q, %q)[%d]=%q, want %q", tt.raw, tt.goos, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIntegrationsConfigValue(t *testing.T) {
	tests := []struct {
		values []WrapperIntegrationMode
		want   string
	}{
		{nil, "editor_settings"},
		{[]WrapperIntegrationMode{IntegrationEditorSettings}, "editor_settings"},
		{[]WrapperIntegrationMode{IntegrationManagedShim}, "managed_shim"},
		{[]WrapperIntegrationMode{IntegrationEditorSettings, IntegrationManagedShim}, "both"},
	}
	for _, tt := range tests {
		if got := integrationsConfigValue(tt.values); got != tt.want {
			t.Fatalf("integrationsConfigValue(%v)=%q, want %q", tt.values, got, tt.want)
		}
	}
}

func TestIntegrationsConfigValueOr(t *testing.T) {
	if got := integrationsConfigValueOr(nil, "none"); got != "none" {
		t.Fatalf("integrationsConfigValueOr(nil, none)=%q, want none", got)
	}
}
