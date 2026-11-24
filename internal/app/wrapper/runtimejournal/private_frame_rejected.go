package runtimejournal

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// RejectPrivatePreparedFrame records only a frame that the single writer has
// proved was never started. Replayed events retain the same journal identity.
func (s *Store) RejectPrivatePreparedFrame(frameID int64, cause error) ([]agentproto.Event, error) {
	var raw []byte
	var stage string
	err := s.db.QueryRow(`SELECT c.payload,c.stage FROM frames f JOIN commands c ON c.id=f.command_id WHERE f.id=? AND f.stage='prepared' AND f.native_method='thread/compact/start' AND c.incarnation=? AND NOT EXISTS (SELECT 1 FROM frames other WHERE other.command_id=c.id AND other.id<>f.id AND NOT(other.native_method='thread/resume' AND other.stage='written'))`, frameID, s.incarnation).Scan(&raw, &stage)
	if err != nil {
		return nil, err
	}
	if stage != StagePrepared && stage != StagePartiallyWritten {
		return nil, ErrConflict
	}
	var command agentproto.Command
	if err = json.Unmarshal(raw, &command); err != nil || command.NativeBinding == nil || command.Kind != agentproto.CommandThreadCompactStart {
		return nil, ErrConflict
	}
	problem := agentproto.ErrorInfoFromError(cause, agentproto.ErrorInfo{Code: "command_rejected", Layer: "wrapper", Stage: "before_child_write", Operation: string(command.Kind), Message: "无法确认当前会话已空闲，请等待当前任务结束后重试。", SurfaceSessionID: command.Origin.Surface, CommandID: command.CommandID, ThreadID: command.Target.ThreadID})
	problem.CommandID = command.CommandID
	event := agentproto.Event{Kind: agentproto.EventNativeCommandRejected, CommandID: command.CommandID, NativeBinding: command.NativeBinding, Status: "failed", Action: "wrapper.before_compact_write", Problem: &problem, Metadata: map[string]any{"frameID": frameID}}
	events, err := s.PersistEvents([]agentproto.Event{event})
	if err != nil {
		return nil, fmt.Errorf("persist prepared frame rejection: %w", err)
	}
	return events, nil
}
