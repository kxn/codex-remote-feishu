package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) markWorkBootstrap(request, response []byte) {
	if a.work == nil {
		return
	}
	s := a.work.inspection
	s.mu.Lock()
	s.nativeReady = true
	s.bootstrap = agentproto.RPCProbe{Request: append([]byte(nil), request...), Response: append([]byte(nil), response...)}
	s.dirty = true
	s.mu.Unlock()
	_ = s.persist(a.work.journal, a.work.snapshot.Owner)
}

func (a *App) inspectionProtocolVersion() int {
	if a.managedInspectionEnabled() {
		return agentproto.RuntimeInspectionProtocolVersion
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
	work := a.work
	snapshot := work.snapshot
	result.CarrierIncarnation = snapshot.CarrierIncarnation
	result.InstanceGeneration = snapshot.InstanceGeneration
	result.ProcessGeneration = snapshot.ProcessGeneration
	result.LocationGeneration = snapshot.LocationGeneration
	request := command.RuntimeInspection
	if request == nil || request.Version != agentproto.RuntimeInspectionProtocolVersion || request.ExpectedInstanceGeneration != snapshot.InstanceGeneration || request.ExpectedProcessGeneration != snapshot.ProcessGeneration || request.ExpectedLocationGeneration != snapshot.LocationGeneration {
		return fail("native_inspection_generation_mismatch", "检查请求与不可变宿主代次不一致。")
	}
	s := work.inspection
	s.mu.Lock()
	result.Bootstrap = s.bootstrap
	s.mu.Unlock()
	ownerOnly := command.Execution.AttemptID == "" && command.Execution.RunRef == ""
	if !ownerOnly {
		s.mu.Lock()
		proof, known := s.turns[nativeTurnKey(command.Target.ThreadID, command.Target.TurnID)]
		s.mu.Unlock()
		if !known || proof.Binding != *command.Execution {
			if request.SubjectCommandID != "" {
				binding, err := work.journal.Binding(request.SubjectCommandID)
				if err == nil && binding != nil && *binding == *command.Execution {
					rejection, e := work.journal.RejectedEvent(request.SubjectCommandID)
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
		return a.nativeInspectionProbe(ctx, command, session, method, writeCh)
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
				record.Native, err = probe(session, "thread/read")
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
					record.StreamTerminal, _ = work.journal.TerminalEvent(session, terminalTurn)
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
		terminal, err := work.journal.TerminalEvent(command.Target.ThreadID, command.Target.TurnID)
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
			inventory := s.inspectInventory(snapshot)
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
	result.Background = s.inspectInventory(snapshot)
	if pending, err := work.journal.PendingNativeInputs(snapshot.Owner); err != nil {
		result.Background.NativeInputsComplete = false
	} else if pending > result.Background.PendingNativeInputs {
		result.Background.PendingNativeInputs = pending
	}
	watermark, err := work.journal.NativeWriteWatermark()
	if err != nil {
		return fail("native_write_watermark_unavailable", err.Error())
	}
	result.NativeWriteWatermark = watermark
	result.ObservedAt = time.Now().UTC()
	return event
}

func (a *App) nativeInspectionProbe(ctx context.Context, command agentproto.Command, session, method string, writeCh chan<- []byte) (agentproto.RPCProbe, error) {
	if session == "" || (method != "thread/read" && method != "thread/goal/get") {
		return agentproto.RPCProbe{}, fmt.Errorf("unsupported fixed native inspection probe")
	}
	params := map[string]any{"threadId": session}
	if method == "thread/read" {
		params["includeTurns"] = true
	}
	requestID := command.CommandID + ":" + method + ":" + session
	raw, err := json.Marshal(map[string]any{"id": requestID, "method": method, "params": params})
	if err != nil {
		return agentproto.RPCProbe{}, err
	}
	raw = append(raw, '\n')
	response := make(chan []byte, 1)
	s := a.work.inspection
	s.mu.Lock()
	s.waiters[requestID] = response
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.waiters, requestID); s.mu.Unlock() }()
	if err := a.work.registerFrames(command.CommandID, [][]byte{raw}); err != nil {
		return agentproto.RPCProbe{}, err
	}
	if err := s.persist(a.work.journal, a.work.snapshot.Owner); err != nil {
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
