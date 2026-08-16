package daemon

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/gpustatus"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

const gpuStatusTimeout = 4 * time.Second

func (a *App) handleGPUStatusDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	ctx, cancel := context.WithTimeout(context.Background(), gpuStatusTimeout)
	defer cancel()

	snapshot, err := gpustatus.Collect(ctx)
	if err != nil {
		return commandPageEvents(command.SurfaceSessionID, buildGPUStatusErrorPageView(err))
	}
	return commandPageEvents(command.SurfaceSessionID, buildGPUStatusPageView(snapshot))
}

func buildGPUStatusPageView(snapshot gpustatus.Snapshot) control.FeishuPageView {
	sections := make([]control.FeishuCardTextSection, 0, len(snapshot.GPUs)+1)
	totalUsedMiB := 0.0
	totalMemoryMiB := 0.0
	for _, gpu := range snapshot.GPUs {
		totalUsedMiB += gpu.MemoryUsedMiB
		totalMemoryMiB += gpu.MemoryTotalMiB
		sections = append(sections, gpuStatusSection(gpu))
	}
	collectedAt := snapshot.CollectedAt.Local()
	if collectedAt.IsZero() {
		collectedAt = time.Now()
	}
	summary := fmt.Sprintf(
		"%d 张 GPU · 总显存 %s / %s · %s",
		len(snapshot.GPUs),
		formatGPUmemory(totalUsedMiB),
		formatGPUmemory(totalMemoryMiB),
		collectedAt.Format("15:04:05"),
	)
	sections = append([]control.FeishuCardTextSection{control.CommandCatalogTextSection("概览", summary)}, sections...)
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		PageID:       control.FeishuCommandGPUStatus,
		CommandID:    control.FeishuCommandGPUStatus,
		Title:        "GPU 状态",
		ThemeKey:     "success",
		BodySections: sections,
		Interactive:  true,
		RelatedButtons: []control.CommandCatalogButton{
			control.FeishuLocalPageCommandButton("刷新 GPU 状态", "/gpu", "primary", false),
		},
		SuppressDefaultRelatedButtons: true,
	})
}

func buildGPUStatusErrorPageView(err error) control.FeishuPageView {
	message := "nvidia-smi 未返回可用数据。"
	if err != nil {
		message = truncateGPUStatusError(err.Error(), 800)
	}
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		PageID:    control.FeishuCommandGPUStatus,
		CommandID: control.FeishuCommandGPUStatus,
		Title:     "GPU 状态读取失败",
		ThemeKey:  "error",
		BodySections: []control.FeishuCardTextSection{
			control.CommandCatalogTextSection("错误", message),
			control.CommandCatalogTextSection("检查", "请确认 NVIDIA 驱动正常，并且运行 codex-remote 的用户可以执行 nvidia-smi。"),
		},
		Interactive: true,
		RelatedButtons: []control.CommandCatalogButton{
			control.FeishuLocalPageCommandButton("重新读取", "/gpu", "primary", false),
		},
		SuppressDefaultRelatedButtons: true,
	})
}

func gpuStatusSection(gpu gpustatus.GPU) control.FeishuCardTextSection {
	memoryPercent := percent(gpu.MemoryUsedMiB, gpu.MemoryTotalMiB)
	utilization := clampPercent(gpu.UtilizationPercent)
	lines := []string{
		fmt.Sprintf("显存  %s / %s  %s %.0f%%", formatGPUmemory(gpu.MemoryUsedMiB), formatGPUmemory(gpu.MemoryTotalMiB), gpuStatusBar(memoryPercent), memoryPercent),
		fmt.Sprintf("负载  %s %.0f%%", gpuStatusBar(utilization), utilization),
	}
	power := ""
	if gpu.PowerLimitW > 0 {
		power = fmt.Sprintf(" · 功耗 %.0f / %.0f W", gpu.PowerDrawW, gpu.PowerLimitW)
	} else if gpu.PowerDrawW > 0 {
		power = fmt.Sprintf(" · 功耗 %.0f W", gpu.PowerDrawW)
	}
	lines = append(lines, fmt.Sprintf("温度  %.0f°C%s", gpu.TemperatureC, power))
	return control.CommandCatalogTextSection(
		fmt.Sprintf("%s GPU %d · %s", gpuHealthMarker(gpu, memoryPercent), gpu.Index, gpu.Name),
		lines...,
	)
}

func gpuHealthMarker(gpu gpustatus.GPU, memoryPercent float64) string {
	switch {
	case gpu.TemperatureC >= 85:
		return "🔴"
	case gpu.TemperatureC >= 75 || gpu.UtilizationPercent >= 95 || memoryPercent >= 95:
		return "🟠"
	default:
		return "🟢"
	}
}

func gpuStatusBar(value float64) string {
	const width = 10
	filled := int(math.Round(clampPercent(value) / 100 * width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatGPUmemory(mib float64) string {
	return fmt.Sprintf("%.1f GiB", mib/1024)
}

func percent(value, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return clampPercent(value / total * 100)
}

func clampPercent(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func truncateGPUStatusError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "…"
}
