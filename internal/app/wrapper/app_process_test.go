package wrapper

import "testing"

func TestCodexChildLaunchOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want childLaunchOptions
	}{
		{
			name: "managed headless hides child window",
			cfg: Config{
				Source:  "headless",
				Managed: true,
			},
			want: childLaunchOptions{
				HideWindow:     true,
				CreateNoWindow: true,
			},
		},
		{
			name: "headless source is case and whitespace tolerant",
			cfg: Config{
				Source:  "  HeAdLeSs ",
				Managed: true,
			},
			want: childLaunchOptions{
				HideWindow:     true,
				CreateNoWindow: true,
			},
		},
		{
			name: "unmanaged headless stays interactive",
			cfg: Config{
				Source:  "headless",
				Managed: false,
			},
			want: childLaunchOptions{},
		},
		{
			name: "managed vscode stays interactive",
			cfg: Config{
				Source:  "vscode",
				Managed: true,
			},
			want: childLaunchOptions{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := childLaunchOptionsForAgent(tt.cfg); got != tt.want {
				t.Fatalf("childLaunchOptionsForAgent() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
