package daemon

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

type feishuP2PSurfaceResumeCandidate struct {
	entry SurfaceResumeEntry
	ref   feishu.SurfaceRef
}

func canonicalizeSurfaceResumeEntries(entries map[string]SurfaceResumeEntry) (map[string]SurfaceResumeEntry, bool) {
	if len(entries) == 0 {
		return map[string]SurfaceResumeEntry{}, false
	}

	canonical := make(map[string]SurfaceResumeEntry, len(entries))
	grouped := make(map[string][]feishuP2PSurfaceResumeCandidate)
	changed := false

	for key, entry := range entries {
		normalized, ok := normalizeSurfaceResumeEntry(entry)
		if !ok {
			changed = true
			continue
		}
		if strings.TrimSpace(key) != normalized.SurfaceSessionID {
			changed = true
		}
		if groupKey, candidate, ok := feishuP2PSurfaceResumeGroup(normalized); ok {
			grouped[groupKey] = append(grouped[groupKey], candidate)
			continue
		}
		canonical[normalized.SurfaceSessionID] = normalized
		if !sameSurfaceResumeEntryContent(normalized, entry) {
			changed = true
		}
	}

	for _, candidates := range grouped {
		merged := mergeFeishuP2PSurfaceResumeCandidates(candidates)
		canonical[merged.SurfaceSessionID] = merged
		if len(candidates) != 1 {
			changed = true
			continue
		}
		if only := candidates[0].entry; merged.SurfaceSessionID != only.SurfaceSessionID || !sameSurfaceResumeEntryContent(merged, only) {
			changed = true
		}
	}

	if len(canonical) != len(entries) {
		changed = true
	}
	return canonical, changed
}

func feishuP2PSurfaceResumeGroup(entry SurfaceResumeEntry) (string, feishuP2PSurfaceResumeCandidate, bool) {
	ref, ok := feishu.ParseSurfaceRef(entry.SurfaceSessionID)
	if !ok || ref.ScopeKind != feishu.ScopeKindUser {
		return "", feishuP2PSurfaceResumeCandidate{}, false
	}
	chatID := strings.TrimSpace(entry.ChatID)
	if chatID == "" {
		return "", feishuP2PSurfaceResumeCandidate{}, false
	}
	gatewayID := strings.TrimSpace(firstNonEmpty(entry.GatewayID, ref.GatewayID))
	if gatewayID == "" {
		return "", feishuP2PSurfaceResumeCandidate{}, false
	}
	return gatewayID + "|" + chatID, feishuP2PSurfaceResumeCandidate{entry: entry, ref: ref}, true
}

func mergeFeishuP2PSurfaceResumeCandidates(candidates []feishuP2PSurfaceResumeCandidate) SurfaceResumeEntry {
	bestIdentity := ""
	gatewayID := ""
	chatID := ""
	latest := candidates[0]
	bestResume := candidates[0]
	latestAt := time.Time{}

	for _, candidate := range candidates {
		entry := candidate.entry
		if gatewayID == "" {
			gatewayID = strings.TrimSpace(firstNonEmpty(entry.GatewayID, candidate.ref.GatewayID))
		}
		if chatID == "" {
			chatID = strings.TrimSpace(entry.ChatID)
		}
		bestIdentity = betterFeishuSurfaceUserID(bestIdentity, candidate.ref.ScopeID)
		bestIdentity = betterFeishuSurfaceUserID(bestIdentity, entry.ActorUserID)
		if entry.UpdatedAt.After(latest.entry.UpdatedAt) {
			latest = candidate
		}
		if resumeStatePayloadScore(entry) > resumeStatePayloadScore(bestResume.entry) ||
			(resumeStatePayloadScore(entry) == resumeStatePayloadScore(bestResume.entry) && entry.UpdatedAt.After(bestResume.entry.UpdatedAt)) {
			bestResume = candidate
		}
		if entry.UpdatedAt.After(latestAt) {
			latestAt = entry.UpdatedAt
		}
	}

	merged := latest.entry
	merged.GatewayID = strings.TrimSpace(firstNonEmpty(gatewayID, merged.GatewayID))
	merged.ChatID = strings.TrimSpace(firstNonEmpty(chatID, merged.ChatID))
	merged.ActorUserID = strings.TrimSpace(firstNonEmpty(bestIdentity, merged.ActorUserID))
	merged.SurfaceSessionID = feishu.SurfaceRef{
		Platform:  feishu.PlatformFeishu,
		GatewayID: merged.GatewayID,
		ScopeKind: feishu.ScopeKindUser,
		ScopeID:   merged.ActorUserID,
	}.SurfaceID()

	merged.ResumeInstanceID = bestResume.entry.ResumeInstanceID
	merged.ResumeThreadID = bestResume.entry.ResumeThreadID
	merged.ResumeThreadTitle = bestResume.entry.ResumeThreadTitle
	merged.ResumeThreadCWD = bestResume.entry.ResumeThreadCWD
	merged.ResumeWorkspaceKey = bestResume.entry.ResumeWorkspaceKey
	merged.ResumeRouteMode = bestResume.entry.ResumeRouteMode
	merged.ResumeHeadless = bestResume.entry.ResumeHeadless
	merged.UpdatedAt = latestAt

	normalized, ok := normalizeSurfaceResumeEntry(merged)
	if !ok {
		return merged
	}
	return normalized
}

func betterFeishuSurfaceUserID(current, candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return strings.TrimSpace(current)
	}
	current = strings.TrimSpace(current)
	if current == "" {
		return candidate
	}
	currentRank := feishuSurfaceUserIDRank(current)
	candidateRank := feishuSurfaceUserIDRank(candidate)
	if candidateRank > currentRank {
		return candidate
	}
	if candidateRank < currentRank {
		return current
	}
	if len(candidate) > len(current) {
		return candidate
	}
	return current
}

func feishuSurfaceUserIDRank(value string) int {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, "ou_"):
		return 3
	case strings.HasPrefix(value, "on_"):
		return 2
	case value != "":
		return 1
	default:
		return 0
	}
}

func resumeStatePayloadScore(entry SurfaceResumeEntry) int {
	score := 0
	if strings.TrimSpace(entry.ResumeThreadID) != "" {
		score += 100
	}
	switch strings.TrimSpace(entry.ResumeRouteMode) {
	case "pinned":
		score += 30
	case "follow_local":
		score += 20
	case "new_thread_ready":
		score += 10
	}
	if strings.TrimSpace(entry.ResumeInstanceID) != "" {
		score += 8
	}
	if strings.TrimSpace(entry.ResumeWorkspaceKey) != "" {
		score += 4
	}
	if strings.TrimSpace(entry.ResumeThreadCWD) != "" {
		score += 2
	}
	if entry.ResumeHeadless {
		score++
	}
	return score
}
