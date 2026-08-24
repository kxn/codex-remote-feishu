package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/gpustatus"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestBuildGPUStatusPageViewAdaptsToDetectedGPUs(t *testing.T) {
	t.Parallel()

	view := buildGPUStatusPageView(gpustatus.Snapshot{
		CollectedAt: time.Date(2026, 8, 16, 22, 30, 0, 0, time.Local),
		GPUs: []gpustatus.GPU{
			{Index: 0, Name: "GPU Alpha", TemperatureC: 45, UtilizationPercent: 0, MemoryUsedMiB: 10, MemoryTotalMiB: 49140, PowerDrawW: 17, PowerLimitW: 300},
			{Index: 1, Name: "GPU Beta", TemperatureC: 63, UtilizationPercent: 17, MemoryUsedMiB: 8221, MemoryTotalMiB: 49140, PowerDrawW: 116, PowerLimitW: 300},
			{Index: 2, Name: "GPU Gamma", TemperatureC: 76, UtilizationPercent: 98, MemoryUsedMiB: 45000, MemoryTotalMiB: 49140},
		},
	})
	normalized := control.NormalizeFeishuPageView(view)
	if normalized.Title != "GPU 状态" || normalized.ThemeKey != "success" {
		t.Fatalf("GPU page header = %#v", normalized)
	}
	if len(normalized.BodySections) != 4 {
		t.Fatalf("GPU page sections = %d, want overview + dynamic count 3", len(normalized.BodySections))
	}
	if summary := strings.Join(normalized.BodySections[0].Lines, "\n"); !strings.Contains(summary, "3 张 GPU") {
		t.Fatalf("GPU page summary = %q", summary)
	}
	if normalized.BodySections[3].Label != "🟠 GPU 2 · GPU Gamma" {
		t.Fatalf("GPU 2 label = %q", normalized.BodySections[3].Label)
	}
	if len(normalized.RelatedButtons) != 1 || normalized.RelatedButtons[0].CommandText != "/gpu" {
		t.Fatalf("GPU refresh button = %#v", normalized.RelatedButtons)
	}
}

func TestBuildGPUStatusErrorPageViewKeepsRetry(t *testing.T) {
	t.Parallel()

	view := buildGPUStatusErrorPageView(errors.New("driver unavailable"))
	if view.ThemeKey != "error" || view.Title != "GPU 状态读取失败" {
		t.Fatalf("GPU error page = %#v", view)
	}
	if len(view.BodySections) == 0 || !strings.Contains(strings.Join(view.BodySections[0].Lines, "\n"), "driver unavailable") {
		t.Fatalf("GPU error body = %#v", view.BodySections)
	}
	if len(view.RelatedButtons) != 1 || view.RelatedButtons[0].CommandText != "/gpu" {
		t.Fatalf("GPU retry button = %#v", view.RelatedButtons)
	}
}

func TestGPUStatusBarClampsValues(t *testing.T) {
	t.Parallel()

	if got := gpuStatusBar(-10); got != "░░░░░░░░░░" {
		t.Fatalf("gpuStatusBar(-10) = %q", got)
	}
	if got := gpuStatusBar(1000); got != "██████████" {
		t.Fatalf("gpuStatusBar(1000) = %q", got)
	}
}
