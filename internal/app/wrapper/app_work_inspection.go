package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/nativeinspection"
)

func (a *App) markWorkBootstrap(request, response []byte) {
	s := a.inspectionState()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.nativeReady = true
	s.bootstrap = agentproto.RPCProbe{Request: append([]byte(nil), request...), Response: append([]byte(nil), response...)}
	s.dirty = true
	s.mu.Unlock()
	_ = a.persistInspection()
}

func (a *App) inspectionProtocolVersion() int {
	if a.work != nil && a.managedInspectionEnabled() {
		return agentproto.RuntimeInspectionProtocolVersion
	}
	return 0
}

func (a *App) privateInspectionProtocolVersion() int {
	if a.privateNative != nil && a.managedInspectionEnabled() {
		return agentproto.PrivateNativeInspectionProtocolVersion
	}
	return 0
}

func (a *App) inspectNative(ctx context.Context, command agentproto.Command, writeCh chan<- []byte) agentproto.Event {
	result := &agentproto.RuntimeInspectionResult{Version: agentproto.RuntimeInspectionProtocolVersion, RequestCommandID: command.CommandID, ObservedAt: time.Now().UTC()}
	event := agentproto.Event{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, RuntimeInspection: result}
	fail := func(code, message string) agentproto.Event {
		result.Problem = &agentproto.ErrorInfo{Code: code, Layer: "wrapper", Stage: "runtime_inspection", Message: message, CommandID: command.CommandID}
		return event
	}
	if !a.managedInspectionEnabled() {
		return fail("native_inventory_unavailable", "当前 wrapper 缺少可核查的宿主进程与代次绑定。")
	}
	var journal *runtimejournal.Store
	var incarnation string
	var instanceGeneration, processGeneration, locationGeneration int64
	var ownerOnly bool
	if a.work != nil {
		snapshot := a.work.snapshot
		journal = a.work.journal
		incarnation = snapshot.CarrierIncarnation
		instanceGeneration = snapshot.InstanceGeneration
		processGeneration = snapshot.ProcessGeneration
		locationGeneration = snapshot.LocationGeneration
		if command.Execution == nil || command.NativeBinding != nil {
			return fail("native_inspection_owner_mismatch", "inspection owner mismatch")
		}
		ownerOnly = command.Execution.AttemptID == "" && command.Execution.RunRef == ""
	} else {
		owner := a.privateNative.snapshot.Owner
		journal = a.privateNative.journal
		incarnation = owner.CarrierIncarnation
		instanceGeneration = owner.InstanceGeneration
		processGeneration = owner.ProcessGeneration
		locationGeneration = 0
		if command.NativeBinding == nil || command.Execution != nil || command.NativeBinding.Owner != owner {
			return fail("native_inspection_owner_mismatch", "inspection owner mismatch")
		}
		ownerOnly = command.NativeBinding.ExecutionID == ""
	}
	result.CarrierIncarnation = incarnation
	result.InstanceGeneration = instanceGeneration
	result.ProcessGeneration = processGeneration
	result.LocationGeneration = locationGeneration
	request := command.RuntimeInspection
	if request == nil || request.Version != agentproto.RuntimeInspectionProtocolVersion || request.ExpectedInstanceGeneration != instanceGeneration || request.ExpectedProcessGeneration != processGeneration || request.ExpectedLocationGeneration != locationGeneration {
		return fail("native_inspection_generation_mismatch", "检查请求与不可变宿主代次不一致。")
	}
	if a.privateNative != nil && ownerOnly && (command.Target.ThreadID != "" || command.Target.TurnID != "" || request.SubjectCommandID != "") {
		return fail("native_inspection_owner_mismatch", "owner-wide private inspection cannot name a subject or turn")
	}
	s := a.inspectionState()
	if request.IncludePendingRequests {
		if a.privateNative == nil || !ownerOnly || request.StopBackground {
			return fail("native_inspection_owner_mismatch", "pending request inventory requires a private owner-only inspection")
		}
		s.mu.Lock()
		ready, bootstrap := s.nativeReady, s.bootstrap
		if !ready || !nativeinspection.ValidBootstrap(a.privateNative.snapshot.Owner.Backend, bootstrap) {
			s.mu.Unlock()
			return fail("native_initialization_unproven", "原生进程尚无成功初始化证据。")
		}
		live := make(map[string]string)
		for key, value := range s.pendingInputs {
			if strings.HasPrefix(key, "request:") {
				live[strings.TrimPrefix(key, "request:")] = value
			}
		}
		inventory, err := journal.PendingPrivateRequests(ctx, a.privateNative.snapshot.Owner, live)
		s.mu.Unlock()
		if err != nil {
			return fail("native_inventory_unavailable", "无法核查待处理请求的持久记录。")
		}
		result.PendingRequests = &inventory
		result.Bootstrap = bootstrap
		return event
	}
	s.mu.Lock()
	result.Bootstrap = s.bootstrap
	s.mu.Unlock()
	if !ownerOnly && a.privateNative != nil {
		if request.SubjectCommandID == "" {
			return fail("native_inspection_target_unproven", "private subject required")
		}
		var proven bool
		var err error
		if command.Target.ThreadID != "" && command.Target.TurnID != "" {
			proven, err = journal.PrivateSubjectOwnsTurn(request.SubjectCommandID, command.Target.ThreadID, command.Target.TurnID, *command.NativeBinding)
			if err != nil {
				return fail("native_inspection_target_unproven", err.Error())
			}
		}
		if !proven && command.Target.TurnID == "" {
			result.NativeRejection, err = journal.PrivateSubjectRejection(request.SubjectCommandID, command.Target.ThreadID, *command.NativeBinding)
			if err != nil {
				return fail("native_inspection_target_unproven", err.Error())
			}
			proven = result.NativeRejection != nil
		}
		if !proven {
			return fail("native_inspection_target_unproven", "private target lacks exact journaled native association")
		}
	}

	if !ownerOnly && a.work != nil {
		s.mu.Lock()
		proof, known := s.turns[nativeTurnKey(command.Target.ThreadID, command.Target.TurnID)]
		s.mu.Unlock()
		if !known || proof.Binding != *command.Execution {
			if request.SubjectCommandID != "" {
				binding, err := journal.Binding(request.SubjectCommandID)
				if err == nil && binding != nil && *binding == *command.Execution {
					rejection, e := journal.RejectedEvent(request.SubjectCommandID)
					if e == nil {
						result.NativeRejection = rejection
					}
				}
			}
			if result.NativeRejection == nil {
				return fail("native_inspection_target_unproven", "目标原生轮次尚无精确命令关联证据。")
			}
		}
	}
	probe := func(session, method string) (agentproto.RPCProbe, error) {
		return a.nativeInspectionProbe(ctx, command, session, method, writeCh, false)
	}
	if ownerOnly && command.Target.TurnID == "" {
		s.mu.Lock()
		ready := s.nativeReady
		bootstrap := s.bootstrap
		var sessions []string
		for id := range s.sessionIDs {
			sessions = append(sessions, id)
		}
		s.mu.Unlock()
		if !ready {
			return fail("native_initialization_unproven", "原生进程尚无成功初始化证据。")
		}
		if len(sessions) > 128 {
			return fail("native_inventory_session_limit", "当前 carrier 的会话清单超出单次核查上限，需要受控回收。")
		}
		sort.Strings(sessions)
		result.OwnerScope = &agentproto.RuntimeOwnerInspection{Bootstrap: bootstrap, KnownSessionsComplete: true}
		for _, session := range sessions {
			record := agentproto.RuntimeSessionInspection{SessionID: session}
			if a.runtime.Backend() == agentproto.BackendCodex {
				var err error
				if a.privateNative != nil {
					record.EphemeralReview, err = journal.PrivateEphemeralReviewProof(a.privateNative.snapshot.Owner, session)
					if err != nil {
						return fail("native_review_proof_unavailable", err.Error())
					}
				}
				record.Native, err = a.nativeInspectionProbe(ctx, command, session, "thread/read", writeCh, record.EphemeralReview != nil)
				if err != nil {
					return fail("native_read_probe_failed", err.Error())
				}
				record.Continuation, err = probe(session, "thread/goal/get")
				if err != nil {
					return fail("native_goal_probe_failed", err.Error())
				}
			} else {
				s.mu.Lock()
				var terminalTurn string
				hadInput := false
				for key, proof := range s.turns {
					if len(key) > len(session) && key[:len(session)+1] == session+"\x00" {
						hadInput = true
						if proof.Terminal {
							terminalTurn = key[len(session)+1:]
						}
					}
				}
				s.mu.Unlock()
				record.NoNativeInputObserved = !hadInput
				if terminalTurn != "" {
					record.StreamTerminal, _ = journal.TerminalEvent(session, terminalTurn)
				}
			}
			result.OwnerScope.Sessions = append(result.OwnerScope.Sessions, record)
		}
	} else if a.runtime.Backend() == agentproto.BackendCodex {
		if command.Target.ThreadID != "" {
			var err error
			result.Native, err = probe(command.Target.ThreadID, "thread/read")
			if err != nil {
				return fail("native_read_probe_failed", err.Error())
			}
			result.Continuation, err = probe(command.Target.ThreadID, "thread/goal/get")
			if err != nil {
				return fail("native_goal_probe_failed", err.Error())
			}
		}
	} else {
		terminal, err := journal.TerminalEvent(command.Target.ThreadID, command.Target.TurnID)
		if err != nil {
			return fail("native_terminal_lookup_failed", err.Error())
		}
		result.StreamTerminal = terminal
	}
	if request.StopBackground {
		s.mu.Lock()
		scopes := append([]*hostinventory.Scope(nil), s.scopes...)
		s.mu.Unlock()
		for _, scope := range scopes {
			if err := scope.StopChildren(ctx); err != nil {
				return fail("native_background_stop_failed", err.Error())
			}
		}
	}
	if request.StopBackground {
		deadline := time.Now().Add(2 * time.Second)
		for {
			inventory := s.inspectInventoryIdentity(incarnation, processGeneration, locationGeneration)
			if len(inventory.RunningProcessIDs) == 0 && len(inventory.PendingTaskIDs) == 0 {
				break
			}
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return fail("native_background_stop_timeout", ctx.Err().Error())
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	result.Background = s.inspectInventoryIdentity(incarnation, processGeneration, locationGeneration)
	if a.privateNative != nil {
		if result.OwnerScope != nil && !result.Background.ContinuationInventoryKnown {
			s.mu.Lock()
			known := make(map[string]bool, len(s.goalKnown))
			for id, value := range s.goalKnown {
				known[id] = value
			}
			s.mu.Unlock()
			for _, observed := range result.OwnerScope.Sessions {
				if observed.EphemeralReview != nil && nativeinspection.ValidEphemeralReviewGoalProbe(observed.Continuation, observed.SessionID) {
					known[observed.SessionID] = true
				}
			}
			complete := true
			for _, value := range known {
				if !value {
					complete = false
					break
				}
			}
			result.Background.ContinuationInventoryKnown = complete
		}
		active := result.Background.ActiveNativeTurnIDs[:0]
		for _, id := range result.Background.ActiveNativeTurnIDs {
			parts := strings.SplitN(id, "\x00", 2)
			if len(parts) == 2 {
				closed, err := journal.PrivateReviewSubturnClosed(parts[0], parts[1])
				if err != nil {
					return fail("native_review_inventory_unavailable", err.Error())
				}
				if closed {
					continue
				}
			}
			active = append(active, id)
		}
		result.Background.ActiveNativeTurnIDs = active
	}
	var pending int
	var pendingErr error
	if a.work != nil {
		pending, pendingErr = journal.PendingNativeInputs(a.work.snapshot.Owner)
	} else {
		pending, pendingErr = journal.PendingPrivateNativeInputs(a.privateNative.snapshot.Owner)
	}
	if pendingErr != nil {
		result.Background.NativeInputsComplete = false
	} else if pending > result.Background.PendingNativeInputs {
		result.Background.PendingNativeInputs = pending
	}
	watermark, err := journal.NativeWriteWatermark()
	if err != nil {
		return fail("native_write_watermark_unavailable", err.Error())
	}
	result.NativeWriteWatermark = watermark
	result.ObservedAt = time.Now().UTC()
	return event
}

func (a *App) nativeInspectionProbe(ctx context.Context, command agentproto.Command, session, method string, writeCh chan<- []byte, ephemeralReview bool) (agentproto.RPCProbe, error) {
	if session == "" || (method != "thread/read" && method != "thread/goal/get") {
		return agentproto.RPCProbe{}, fmt.Errorf("unsupported fixed native inspection probe")
	}
	params := map[string]any{"threadId": session}
	if method == "thread/read" {
		params["includeTurns"] = !ephemeralReview
	}
	requestID := command.CommandID + ":" + method + ":" + session
	raw, err := json.Marshal(map[string]any{"id": requestID, "method": method, "params": params})
	if err != nil {
		return agentproto.RPCProbe{}, err
	}
	raw = append(raw, '\n')
	response := make(chan []byte, 1)
	s := a.inspectionState()
	s.mu.Lock()
	s.waiters[requestID] = response
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.waiters, requestID); s.mu.Unlock() }()
	if a.work != nil {
		if err := a.work.registerFrames(command.CommandID, [][]byte{raw}); err != nil {
			return agentproto.RPCProbe{}, err
		}
	} else {
		runtime, ok := a.runtime.(*privateNativeRuntime)
		if !ok {
			return agentproto.RPCProbe{}, fmt.Errorf("private runtime unavailable")
		}
		if err := runtime.registerFrames(command.CommandID, [][]byte{raw}); err != nil {
			return agentproto.RPCProbe{}, err
		}
	}
	if err := a.persistInspection(); err != nil {
		return agentproto.RPCProbe{}, err
	}
	select {
	case writeCh <- raw:
	case <-ctx.Done():
		return agentproto.RPCProbe{}, ctx.Err()
	}
	select {
	case reply := <-response:
		return agentproto.RPCProbe{Request: raw, Response: reply}, nil
	case <-ctx.Done():
		return agentproto.RPCProbe{}, ctx.Err()
	case <-time.After(5 * time.Second):
		return agentproto.RPCProbe{}, fmt.Errorf("native inspection probe timed out")
	}
}
