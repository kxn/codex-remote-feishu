package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) releaseCompletedShellCommandAttachments(surface *state.SurfaceConsoleRecord, threadID, turnID string) []eventcontract.Event {
	if surface == nil || strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" {
		return nil
	}
	for _, item := range surface.QueueItems {
		if item == nil || item.Status != state.QueueItemShellCommanded || item.ShellCommandThreadID != threadID || item.ShellCommandTurnID != turnID {
			continue
		}
		paths := map[string]struct{}{}
		for _, input := range item.Inputs {
			if input.Type == agentproto.InputLocalImage && strings.TrimSpace(input.Path) != "" {
				paths[strings.TrimSpace(input.Path)] = struct{}{}
			}
		}
		for imageID, image := range surface.StagedImages {
			if image == nil || image.State != state.ImageBound {
				continue
			}
			if _, ok := paths[strings.TrimSpace(image.LocalPath)]; ok {
				image.State = state.ImageDiscarded
				delete(surface.StagedImages, imageID)
			}
		}
	}
	return nil
}
