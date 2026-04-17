package daemon

import (
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type adminSurfaceStatusSummary struct {
	SurfaceSessionID     string     `json:"surfaceSessionId"`
	Platform             string     `json:"platform,omitempty"`
	ProductMode          string     `json:"productMode,omitempty"`
	DisplayTitle         string     `json:"displayTitle"`
	ThreadTitle          string     `json:"threadTitle,omitempty"`
	FirstUserMessage     string     `json:"firstUserMessage,omitempty"`
	LastUserMessage      string     `json:"lastUserMessage,omitempty"`
	LastAssistantMessage string     `json:"lastAssistantMessage,omitempty"`
	WorkspacePath        string     `json:"workspacePath,omitempty"`
	InstanceDisplayName  string     `json:"instanceDisplayName,omitempty"`
	LastActiveAt         *time.Time `json:"lastActiveAt,omitempty"`
}

func (a *App) runtimeSurfaceStatusesLocked(surfaces []*state.SurfaceConsoleRecord) []adminSurfaceStatusSummary {
	summaries := make([]adminSurfaceStatusSummary, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface == nil {
			continue
		}
		snapshot := a.service.SurfaceSnapshot(surface.SurfaceSessionID)
		summary := adminSurfaceStatusSummary{
			SurfaceSessionID: surface.SurfaceSessionID,
			Platform:         strings.TrimSpace(surface.Platform),
			ProductMode:      string(state.NormalizeProductMode(surface.ProductMode)),
		}
		if snapshot != nil {
			summary.ThreadTitle = normalizeAdminSurfaceText(snapshot.Attachment.SelectedThreadTitle)
			summary.FirstUserMessage = normalizeAdminSurfaceText(snapshot.Attachment.SelectedThreadFirstUserMessage)
			summary.LastUserMessage = normalizeAdminSurfaceText(snapshot.Attachment.SelectedThreadLastUserMessage)
			summary.LastAssistantMessage = normalizeAdminSurfaceText(snapshot.Attachment.SelectedThreadLastAssistantMessage)
			summary.WorkspacePath = normalizeAdminSurfaceText(snapshot.WorkspaceKey)
			summary.InstanceDisplayName = normalizeAdminSurfaceText(snapshot.Attachment.DisplayName)
		}
		if summary.WorkspacePath == "" {
			summary.WorkspacePath = normalizeAdminSurfaceText(surface.ClaimedWorkspaceKey)
		}
		if !surface.LastInboundAt.IsZero() {
			lastActive := surface.LastInboundAt
			summary.LastActiveAt = &lastActive
		}
		summary.DisplayTitle = adminSurfaceDisplayTitle(summary)
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		leftAt := adminSurfaceLastActiveUnix(summaries[i])
		rightAt := adminSurfaceLastActiveUnix(summaries[j])
		if leftAt != rightAt {
			return leftAt > rightAt
		}
		if summaries[i].DisplayTitle != summaries[j].DisplayTitle {
			return summaries[i].DisplayTitle < summaries[j].DisplayTitle
		}
		return summaries[i].SurfaceSessionID < summaries[j].SurfaceSessionID
	})
	return summaries
}

func normalizeAdminSurfaceText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func adminSurfaceDisplayTitle(summary adminSurfaceStatusSummary) string {
	for _, candidate := range []string{
		summary.ThreadTitle,
		summary.LastUserMessage,
		summary.FirstUserMessage,
		summary.LastAssistantMessage,
		summary.InstanceDisplayName,
		summary.WorkspacePath,
	} {
		if normalized := normalizeAdminSurfaceText(candidate); normalized != "" {
			return normalized
		}
	}
	return "未命名会话"
}

func adminSurfaceLastActiveUnix(summary adminSurfaceStatusSummary) int64 {
	if summary.LastActiveAt == nil {
		return 0
	}
	return summary.LastActiveAt.Unix()
}
