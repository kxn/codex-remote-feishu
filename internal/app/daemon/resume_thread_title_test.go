package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestNormalizeResumeThreadTitle(t *testing.T) {
	t.Parallel()

	threadID := "019d56f0-de5e-7943-bc9a-18c42ef11acb"
	shortID := control.ShortenThreadID(threadID)

	cases := []struct {
		name         string
		title        string
		threadID     string
		threadCWD    string
		workspaceKey string
		want         string
	}{
		{
			name:      "keeps raw title",
			title:     "修复登录流程",
			threadID:  threadID,
			threadCWD: "/data/dl/droid",
			want:      "修复登录流程",
		},
		{
			name:      "strips display prefix",
			title:     "droid · 修复登录流程",
			threadID:  threadID,
			threadCWD: "/data/dl/droid",
			want:      "修复登录流程",
		},
		{
			name:      "strips repeated display prefix and suffix",
			title:     "droid · droid · 修复登录流程 · " + shortID,
			threadID:  threadID,
			threadCWD: "/data/dl/droid",
			want:      "修复登录流程",
		},
		{
			name:      "clears unnamed display title",
			title:     "droid · " + shortID,
			threadID:  threadID,
			threadCWD: "/data/dl/droid",
			want:      "",
		},
		{
			name:         "uses workspace fallback when cwd missing",
			title:        "droid · 修复登录流程",
			threadID:     threadID,
			workspaceKey: "/data/dl/droid",
			want:         "修复登录流程",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeResumeThreadTitle(tc.title, tc.threadID, tc.threadCWD, tc.workspaceKey); got != tc.want {
				t.Fatalf("normalizeResumeThreadTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSurfaceResumeStoreNormalizesLegacyDisplayThreadTitle(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := loadSurfaceResumeStore(surfaceResumeStatePath(stateDir))
	if err != nil {
		t.Fatalf("load surface resume store: %v", err)
	}

	threadID := "019d56f0-de5e-7943-bc9a-18c42ef11acb"
	shortID := control.ShortenThreadID(threadID)
	if err := store.Put(SurfaceResumeEntry{
		SurfaceSessionID:  "surface-1",
		ProductMode:       "normal",
		ResumeThreadID:    threadID,
		ResumeThreadTitle: "droid · droid · 修复登录流程 · " + shortID,
		ResumeThreadCWD:   "/data/dl/droid",
		ResumeHeadless:    true,
	}); err != nil {
		t.Fatalf("put surface resume entry: %v", err)
	}

	entry, ok := store.Get("surface-1")
	if !ok {
		t.Fatal("expected normalized surface resume entry")
	}
	if entry.ResumeThreadTitle != "修复登录流程" {
		t.Fatalf("expected legacy display title to normalize to raw thread name, got %#v", entry)
	}
}
