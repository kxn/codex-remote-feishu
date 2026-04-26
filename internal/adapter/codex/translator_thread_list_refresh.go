package codex

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type threadListRefreshSession struct {
	ownerRequestID string
	ownerVisible   bool
	pendingReads   map[string]string
	records        map[string]agentproto.ThreadSnapshotRecord
	order          []string
}

func newThreadListRefreshSession(ownerRequestID string, ownerVisible bool) *threadListRefreshSession {
	return &threadListRefreshSession{
		ownerRequestID: strings.TrimSpace(ownerRequestID),
		ownerVisible:   ownerVisible,
		pendingReads:   map[string]string{},
		records:        map[string]agentproto.ThreadSnapshotRecord{},
	}
}

func (t *Translator) beginThreadListRefresh(ownerRequestID string, ownerVisible bool) {
	t.threadListRefresh = newThreadListRefreshSession(ownerRequestID, ownerVisible)
}

func (t *Translator) threadListRefreshPendingReadCount() int {
	if t.threadListRefresh == nil {
		return 0
	}
	return len(t.threadListRefresh.pendingReads)
}

func (t *Translator) threadListRefreshActive() bool {
	if t.threadListRefresh == nil {
		return false
	}
	return strings.TrimSpace(t.threadListRefresh.ownerRequestID) != "" || len(t.threadListRefresh.pendingReads) != 0
}

func (t *Translator) threadListRefreshWaitingForList() bool {
	if t.threadListRefresh == nil {
		return false
	}
	return strings.TrimSpace(t.threadListRefresh.ownerRequestID) != ""
}

func (t *Translator) threadListRefreshOwnsResponse(requestID string) bool {
	if t.threadListRefresh == nil {
		return false
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return false
	}
	return requestID == t.threadListRefresh.ownerRequestID
}

func (t *Translator) finishThreadListRefresh(suppress bool) Result {
	refresh := t.threadListRefresh
	if refresh == nil {
		return Result{
			Suppress: suppress,
			Events: []agentproto.Event{{
				Kind:    agentproto.EventThreadsSnapshot,
				Threads: nil,
			}},
		}
	}
	records := make([]agentproto.ThreadSnapshotRecord, 0, len(refresh.records))
	seen := map[string]bool{}
	for _, originalThreadID := range refresh.order {
		current, ok := refresh.records[originalThreadID]
		if !ok || current.ThreadID == "" || seen[current.ThreadID] {
			continue
		}
		records = append(records, current)
		seen[current.ThreadID] = true
	}
	extras := make([]agentproto.ThreadSnapshotRecord, 0, len(refresh.records))
	for _, current := range refresh.records {
		if current.ThreadID == "" || seen[current.ThreadID] {
			continue
		}
		extras = append(extras, current)
	}
	sort.Slice(extras, func(i, j int) bool {
		if extras[i].ListOrder != extras[j].ListOrder {
			return extras[i].ListOrder < extras[j].ListOrder
		}
		return strings.Compare(extras[i].ThreadID, extras[j].ThreadID) < 0
	})
	records = append(records, extras...)
	t.threadListRefresh = nil
	return Result{
		Suppress: suppress,
		Events: []agentproto.Event{{
			Kind:    agentproto.EventThreadsSnapshot,
			Threads: records,
		}},
	}
}
