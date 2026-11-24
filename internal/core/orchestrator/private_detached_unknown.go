package orchestrator

import (
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// detachPrivateUnknown removes only the user's navigation, retaining the exact
// old execution bindings and input references outside every dispatch queue.
func (s *Service) detachPrivateUnknown(surfaceID string) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil
	}
	snapshot, err := s.ExportPrivateCheckpoint(surfaceID)
	if err != nil {
		return notice(surface, "private_recovery_hold", "无法保存旧会话的未确认操作，请稍后重试。")
	}
	if events, handled := s.detachPendingPrivateReplacement(surface, snapshot); handled {
		return events
	}
	instance := surface.AttachedInstanceID
	for _, h := range snapshot.Holds {
		candidate := h.InstanceID
		if h.Kind == "headless_launch" {
			var pending state.HeadlessLaunchRecord
			if json.Unmarshal(h.Frozen, &pending) != nil || pending.InstanceID == "" {
				return notice(surface, "private_recovery_hold", "旧会话的执行归属尚未确认，请稍后重试。")
			}
			if candidate != "" && candidate != pending.InstanceID {
				return notice(surface, "private_recovery_hold", "旧会话的执行归属存在冲突，请稍后重试。")
			}
			candidate = pending.InstanceID
		}
		if candidate == "" {
			continue
		}
		if instance == "" {
			instance = candidate
		} else if candidate != instance {
			return notice(surface, "private_recovery_hold", "旧会话的执行归属存在冲突，请稍后重试。")
		}
	}
	if instance == "" && (snapshot.ActiveQueueItemID != "" || len(snapshot.Holds) > 0) {
		return notice(surface, "private_recovery_hold", "旧会话的执行归属尚未确认，请稍后重试。")
	}
	if instance != "" {
		kept := make([]PrivateRecoveryHold, 0, len(snapshot.Holds))
		for _, h := range snapshot.Holds {
			if h.Kind != "abandoning" {
				kept = append(kept, h)
			}
		}
		s.privateDetachedExecutions[surfaceID] = append(s.privateDetachedExecutions[surfaceID], PrivateDetachedExecution{InstanceID: instance, ActiveQueueItemID: snapshot.ActiveQueueItemID, QueuedQueueItemIDs: append([]string(nil), snapshot.QueuedQueueItemIDs...), Holds: kept, OfflineStop: snapshot.OfflineStop})
	}
	delete(s.privateRecoveryHolds, surfaceID)
	delete(s.privateOfflineStops, surfaceID)
	events := s.finalizeDetachedSurface(surface)
	if old := s.root.Instances[instance]; old != nil {
		old.Online = false
	}
	surface.QueuedQueueItemIDs = nil
	if instance != "" {
		events = append(events, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: surfaceID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandKillHeadless, SurfaceSessionID: surfaceID, InstanceID: instance}})
	}
	return append(events, notice(surface, "detached_unknown", "已退出旧会话。旧执行结果仍未确认，不会自动重发；可以选择或新建其他会话。")...)
}

func (s *Service) PrivateDetachedInstance(surfaceID, instanceID string) bool {
	for _, v := range s.privateDetachedExecutions[surfaceID] {
		if v.InstanceID == instanceID {
			return true
		}
	}
	return false
}

func (s *Service) privateDetachDeadlineDue(surfaceID string, now time.Time) bool {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return false
	}
	if surface.Abandoning {
		until, ok := s.abandoningUntil[surfaceID]
		return ok && !now.Before(until)
	}
	held, ok := s.privateRecoveryHolds[surfaceID]
	if !ok {
		return false
	}
	found := false
	for _, h := range held.Holds {
		if h.Kind != "abandoning" {
			continue
		}
		var v struct{ Deadline time.Time }
		if found || json.Unmarshal(h.Frozen, &v) != nil || v.Deadline.IsZero() {
			return false
		}
		found = true
		if now.Before(v.Deadline) {
			return false
		}
	}
	return found
}

// RecordPrivateDetachedEvent records only an immutable journal reference; the
// old event does not mutate or publish into the currently selected session.
func (s *Service) RecordPrivateDetachedEvent(surfaceID, instanceID, incarnation string, sequence uint64, hash string) bool {
	if incarnation == "" || sequence == 0 || hash == "" {
		return false
	}
	list := s.privateDetachedExecutions[surfaceID]
	match := -1
	for i, v := range list {
		if v.InstanceID == instanceID {
			if match >= 0 {
				return false
			}
			match = i
		}
	}
	if match < 0 {
		return false
	}
	for _, ref := range list[match].Events {
		if ref.Incarnation == incarnation && ref.Sequence == sequence {
			return ref.PayloadHash == hash
		}
	}
	list[match].Events = append(list[match].Events, PrivateDetachedEventRef{Incarnation: incarnation, Sequence: sequence, PayloadHash: hash})
	s.privateDetachedExecutions[surfaceID] = list
	return true
}

// A replacement has two distinct identities: the already retired source and
// the new launch which may still be in flight. Detach only isolates the latter.
func (s *Service) detachPendingPrivateReplacement(surface *state.SurfaceConsoleRecord, snapshot PrivateCheckpoint) ([]eventcontract.Event, bool) {
	if surface == nil || surface.AttachedInstanceID == "" || snapshot.ActiveQueueItemID != "" {
		return nil, false
	}
	var pending state.HeadlessLaunchRecord
	var hold PrivateRecoveryHold
	found := false
	for _, h := range snapshot.Holds {
		if h.Kind != "headless_launch" {
			return nil, false
		}
		if found || json.Unmarshal(h.Frozen, &pending) != nil || pending.InstanceID == "" || pending.SourceInstanceID != surface.AttachedInstanceID || pending.Purpose != state.HeadlessLaunchPurposePrivateUpgrade || !pending.AutoRestore {
			return nil, false
		}
		hold = h
		hold.InstanceID = pending.InstanceID
		found = true
	}
	if !found {
		return nil, false
	}
	s.privateDetachedExecutions[surface.SurfaceSessionID] = append(s.privateDetachedExecutions[surface.SurfaceSessionID], PrivateDetachedExecution{InstanceID: pending.InstanceID, Holds: []PrivateRecoveryHold{hold}})
	delete(s.privateRecoveryHolds, surface.SurfaceSessionID)
	delete(s.privateOfflineStops, surface.SurfaceSessionID)
	events := s.discardDrafts(surface)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	events = append(events, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: surface.SurfaceSessionID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandKillHeadless, SurfaceSessionID: surface.SurfaceSessionID, InstanceID: pending.InstanceID}})
	return append(events, notice(surface, "private_runtime_replacement_cancelled", "已退出当前会话；正在取消原会话的恢复。可以选择或新建其他会话。")...), true
}
