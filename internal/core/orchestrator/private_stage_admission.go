package orchestrator

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// BeginPrivateStage creates the same draft the user action would create, but
// prevents its dispatch until the exact staged content has been committed.
func (s *Service) BeginPrivateStage(action control.Action) (string, []eventcontract.Event, error) {
	if !s.config.PrivateApplication || (action.Kind != control.ActionTextMessage && action.Kind != control.ActionFileMessage) {
		return "", nil, fmt.Errorf("invalid private stage action")
	}
	v := s.root.Surfaces[action.SurfaceSessionID]
	if v == nil {
		return "", nil, fmt.Errorf("private stage surface missing")
	}
	oldQueue, oldFile := s.nextQueueItemID, s.nextFileID
	s.privateStageAdmission = true
	defer func() { s.privateStageAdmission = false }()
	files := append([]control.ActionFileAttachment(nil), action.Files...)
	action.Files = nil
	if action.Kind == control.ActionTextMessage && action.Text == "" && len(action.Inputs) == 0 {
		action.Inputs = []agentproto.Input{{Type: agentproto.InputText}}
	}
	events := s.ApplySurfaceAction(action)
	if action.Kind == control.ActionTextMessage && s.nextQueueItemID == oldQueue+1 {
		id := fmt.Sprintf("queue-%d", s.nextQueueItemID)
		if item := v.QueueItems[id]; item != nil {
			for _, file := range files {
				if file.SourceMessageID != "" {
					item.SourceMessageIDs = append(item.SourceMessageIDs, file.SourceMessageID)
				}
			}
			item.SourceMessageIDs = uniqueStrings(item.SourceMessageIDs)
		}
		return id, events, nil
	}
	if action.Kind == control.ActionFileMessage && s.nextFileID == oldFile+1 {
		return fmt.Sprintf("file-%d", s.nextFileID), events, nil
	}
	return "", events, nil
}

// CompletePrivateStage consumes a confirmed local staging result only for the
// original draft. A discarded or removed draft is a successful cancellation.
func (s *Service) CompletePrivateStage(surface, id, source, actor, filePath, fileName, prompt string) ([]eventcontract.Event, error) {
	v := s.root.Surfaces[surface]
	if !s.config.PrivateApplication || v == nil || id == "" || source == "" {
		return nil, fmt.Errorf("invalid private stage receipt")
	}
	if item := v.QueueItems[id]; item != nil {
		if item.SourceMessageID != source || item.ActorUserID != actor {
			return nil, fmt.Errorf("private stage queue identity mismatch")
		}
		pending, err := s.PrivateStagePending(surface, id, source, actor)
		if err != nil || !pending {
			return nil, err
		}
		if item.Status != state.QueueItemQueued || !item.StagePending {
			return nil, fmt.Errorf("private stage queue state mismatch")
		}
		if prompt != "" {
			item.Inputs = append([]agentproto.Input{{Type: agentproto.InputText, Text: prompt}}, item.Inputs...)
		}
		item.StagePending = false
		return s.dispatchNext(v), nil
	}
	if file := v.StagedFiles[id]; file != nil {
		if file.SourceMessageID != source || file.ActorUserID != actor {
			return nil, fmt.Errorf("private stage file identity mismatch")
		}
		if file.State == state.FileDiscarded {
			return nil, nil
		}
		if file.State != state.FileStaged || !file.StagePending || filePath == "" {
			return nil, fmt.Errorf("private stage file state mismatch")
		}
		file.LocalPath, file.FileName, file.StagePending = filePath, fileName, false
		return nil, nil
	}
	return nil, nil
}

func (s *Service) PrivateStagePending(surface, id, source, actor string) (bool, error) {
	v := s.root.Surfaces[surface]
	if !s.config.PrivateApplication || v == nil || id == "" || source == "" {
		return false, fmt.Errorf("invalid private stage identity")
	}
	if item := v.QueueItems[id]; item != nil {
		if item.SourceMessageID != source || item.ActorUserID != actor {
			return false, fmt.Errorf("private stage queue identity mismatch")
		}
		if item.Status != state.QueueItemQueued || !item.StagePending {
			return false, nil
		}
		for _, queuedID := range v.QueuedQueueItemIDs {
			if queuedID == id {
				return true, nil
			}
		}
		return false, nil
	}
	if file := v.StagedFiles[id]; file != nil {
		if file.SourceMessageID != source || file.ActorUserID != actor {
			return false, fmt.Errorf("private stage file identity mismatch")
		}
		return file.State == state.FileStaged && file.StagePending, nil
	}
	return false, nil
}

func (s *Service) FailPrivateStage(surface, id, source, actor, reason string) ([]eventcontract.Event, error) {
	pending, err := s.PrivateStagePending(surface, id, source, actor)
	if err != nil || !pending {
		return nil, err
	}
	v := s.root.Surfaces[surface]
	if item := v.QueueItems[id]; item != nil {
		item.Status = state.QueueItemDiscarded
		item.StagePending = false
		v.QueuedQueueItemIDs = removeString(v.QueuedQueueItemIDs, id)
	}
	if file := v.StagedFiles[id]; file != nil {
		file.State = state.FileDiscarded
		file.StagePending = false
	}
	if reason == "" {
		reason = "附件暂存失败"
	}
	events := notice(v, "attachment_stage_failed", reason+"，原消息未发送。请检查后重新发送。")
	return append(events, s.dispatchNext(v)...), nil
}
