package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

const maxProtocolNoticesPerScope = 20

func (s *Service) recordProtocolNotice(instanceID string, event agentproto.Event) []eventcontract.Event {
	notice := agentproto.NormalizeProtocolNotice(event.ProtocolNotice)
	if notice == nil {
		return nil
	}
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.ProtocolNotices = appendBoundedProtocolNotice(inst.ProtocolNotices, *notice)
	if notice.ThreadID == "" {
		return nil
	}
	thread := s.ensureThread(inst, notice.ThreadID)
	thread.ProtocolNotices = appendBoundedProtocolNotice(thread.ProtocolNotices, *notice)
	return nil
}

func appendBoundedProtocolNotice(notices []agentproto.ProtocolNotice, notice agentproto.ProtocolNotice) []agentproto.ProtocolNotice {
	notices = append(notices, notice)
	if len(notices) <= maxProtocolNoticesPerScope {
		return notices
	}
	trimmed := make([]agentproto.ProtocolNotice, maxProtocolNoticesPerScope)
	copy(trimmed, notices[len(notices)-maxProtocolNoticesPerScope:])
	return trimmed
}
