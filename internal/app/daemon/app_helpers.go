package daemon

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func (a *App) nextCommandID() string {
	return "cmd-" + strconv.FormatUint(atomic.AddUint64(&a.commandSeq, 1), 10)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatStatusSnapshotBinary(identity agentproto.ServerIdentity) string {
	branch := strings.TrimSpace(identity.Branch)
	version := strings.TrimSpace(identity.Version)
	fingerprint := strings.TrimSpace(identity.BuildFingerprint)
	if len(fingerprint) > 10 {
		fingerprint = fingerprint[:10]
	}
	parts := make([]string, 0, 3)
	if branch != "" {
		parts = append(parts, branch)
	}
	if version != "" {
		parts = append(parts, version)
	}
	if fingerprint != "" {
		parts = append(parts, fingerprint)
	}
	return strings.Join(parts, " / ")
}

func (a *App) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.runtimeStatusPayload())
}

func summarizeRemoteStatuses(values []orchestrator.RemoteTurnStatus) string {
	if len(values) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strings.Join([]string{
			"instance=" + value.InstanceID,
			"surface=" + value.SurfaceSessionID,
			"queue=" + value.QueueItemID,
			"command=" + value.CommandID,
			"thread=" + value.ThreadID,
			"turn=" + value.TurnID,
			"status=" + value.Status,
		}, ","))
	}
	return strings.Join(parts, "; ")
}

func snapshotSelectedThreadID(snapshot *control.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.Attachment.SelectedThreadID
}

func snapshotRouteMode(snapshot *control.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.Attachment.RouteMode
}

func snapshotPromptThreadID(snapshot *control.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.NextPrompt.ThreadID
}

func snapshotPromptCreateThread(snapshot *control.Snapshot) bool {
	if snapshot == nil {
		return false
	}
	return snapshot.NextPrompt.CreateThread
}

func actionTextPreview(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) <= 120 {
		return text
	}
	return text[:117] + "..."
}
