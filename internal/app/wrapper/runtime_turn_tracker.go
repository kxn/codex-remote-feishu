package wrapper

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type runtimeActiveTurn struct {
	ThreadID               string
	TurnID                 string
	TrafficClass           agentproto.TrafficClass
	Initiator              agentproto.Initiator
	MaterializedOutputSeen bool
	InterruptRequested     bool
}

type runtimeTurnTracker struct {
	mu                     sync.Mutex
	turns                  map[string]*runtimeActiveTurn
	activeTurnByThread     map[string]string
	pendingInterruptByTurn map[string]bool
	pendingInterruptThread map[string]bool
}

func newRuntimeTurnTracker() *runtimeTurnTracker {
	return &runtimeTurnTracker{
		turns:                  map[string]*runtimeActiveTurn{},
		activeTurnByThread:     map[string]string{},
		pendingInterruptByTurn: map[string]bool{},
		pendingInterruptThread: map[string]bool{},
	}
}

func (t *runtimeTurnTracker) ObserveCommand(command agentproto.Command) {
	if t == nil || command.Kind != agentproto.CommandTurnInterrupt {
		return
	}
	threadID := strings.TrimSpace(command.Target.ThreadID)
	turnID := strings.TrimSpace(command.Target.TurnID)

	t.mu.Lock()
	defer t.mu.Unlock()

	if turnID != "" {
		t.pendingInterruptByTurn[turnID] = true
		if turn := t.turns[turnID]; turn != nil {
			turn.InterruptRequested = true
			if threadID == "" {
				threadID = turn.ThreadID
			}
		}
	}
	if threadID != "" {
		t.pendingInterruptThread[threadID] = true
		if activeTurnID := t.activeTurnByThread[threadID]; activeTurnID != "" {
			t.pendingInterruptByTurn[activeTurnID] = true
			if turn := t.turns[activeTurnID]; turn != nil {
				turn.InterruptRequested = true
			}
		}
	}
}

func (t *runtimeTurnTracker) ObserveEvents(events []agentproto.Event) {
	if t == nil || len(events) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, event := range events {
		switch event.Kind {
		case agentproto.EventTurnStarted:
			threadID := strings.TrimSpace(event.ThreadID)
			turnID := strings.TrimSpace(event.TurnID)
			if threadID == "" || turnID == "" {
				continue
			}
			if previousTurnID := t.activeTurnByThread[threadID]; previousTurnID != "" && previousTurnID != turnID {
				delete(t.turns, previousTurnID)
			}
			turn := &runtimeActiveTurn{
				ThreadID:     threadID,
				TurnID:       turnID,
				TrafficClass: event.TrafficClass,
				Initiator:    event.Initiator,
			}
			if t.pendingInterruptByTurn[turnID] || t.pendingInterruptThread[threadID] {
				turn.InterruptRequested = true
			}
			t.turns[turnID] = turn
			t.activeTurnByThread[threadID] = turnID
		case agentproto.EventTurnCompleted:
			threadID := strings.TrimSpace(event.ThreadID)
			turnID := strings.TrimSpace(event.TurnID)
			delete(t.turns, turnID)
			delete(t.pendingInterruptByTurn, turnID)
			if threadID != "" {
				delete(t.pendingInterruptThread, threadID)
				if t.activeTurnByThread[threadID] == turnID {
					delete(t.activeTurnByThread, threadID)
				}
			}
		default:
			if !runtimeTurnEventMaterializesOutput(event) {
				continue
			}
			turnID := strings.TrimSpace(event.TurnID)
			if turnID == "" {
				continue
			}
			if turn := t.turns[turnID]; turn != nil {
				turn.MaterializedOutputSeen = true
			}
		}
	}
}

func (t *runtimeTurnTracker) ReconcileChildExit(err error) []agentproto.Event {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.turns) == 0 {
		return nil
	}

	active := make([]runtimeActiveTurn, 0, len(t.turns))
	for _, turn := range t.turns {
		if turn == nil {
			continue
		}
		active = append(active, *turn)
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].ThreadID == active[j].ThreadID {
			return active[i].TurnID < active[j].TurnID
		}
		return active[i].ThreadID < active[j].ThreadID
	})

	events := make([]agentproto.Event, 0, len(active))
	for _, turn := range active {
		event := agentproto.Event{
			Kind:                 agentproto.EventTurnCompleted,
			ThreadID:             turn.ThreadID,
			TurnID:               turn.TurnID,
			TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
			TrafficClass:         turn.TrafficClass,
			Initiator:            turn.Initiator,
		}
		switch {
		case turn.InterruptRequested:
			event.Status = "interrupted"
		case err == nil && turn.MaterializedOutputSeen:
			event.Status = "completed"
		default:
			problem := runtimeExitProblem(err, turn)
			event.Status = "failed"
			event.ErrorMessage = problem.Message
			event.Problem = &problem
		}
		events = append(events, event)
	}

	t.turns = map[string]*runtimeActiveTurn{}
	t.activeTurnByThread = map[string]string{}
	t.pendingInterruptByTurn = map[string]bool{}
	t.pendingInterruptThread = map[string]bool{}
	return events
}

func runtimeTurnEventMaterializesOutput(event agentproto.Event) bool {
	switch event.Kind {
	case agentproto.EventItemCompleted:
		return true
	default:
		return false
	}
}

func runtimeExitProblem(err error, turn runtimeActiveTurn) agentproto.ErrorInfo {
	message := "provider runtime 在补发 turn.completed 前退出。"
	details := ""
	if err != nil {
		details = strings.TrimSpace(err.Error())
		message = "provider runtime 在 turn 完成前异常退出。"
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			message = fmt.Sprintf("provider runtime 在 turn 完成前退出（exit=%d）。", exitErr.ExitCode())
		}
	}
	if strings.TrimSpace(details) == "" && err != nil {
		details = err.Error()
	}
	return agentproto.ErrorInfo{
		Code:      "runtime_exit_before_turn_completed",
		Layer:     "wrapper",
		Stage:     "runtime_exit_reconciliation",
		Operation: "provider.child.wait",
		Message:   message,
		Details:   details,
		ThreadID:  turn.ThreadID,
		TurnID:    turn.TurnID,
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	if err == nil || target == nil {
		return false
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	*target = exitErr
	return true
}

func emitRuntimeExitReconciliation(client *relayws.Client, tracker *runtimeTurnTracker, err error, reportProblem func(agentproto.ErrorInfo)) {
	if client == nil || tracker == nil {
		return
	}
	events := tracker.ReconcileChildExit(err)
	if len(events) == 0 {
		return
	}
	if sendErr := client.SendEvents(events); sendErr != nil && reportProblem != nil {
		reportProblem(agentproto.ErrorInfoFromError(sendErr, agentproto.ErrorInfo{
			Code:      "relay_send_runtime_exit_reconciliation_failed",
			Layer:     "wrapper",
			Stage:     "runtime_exit_reconciliation",
			Operation: "provider.child.wait",
			Message:   "wrapper 无法把 runtime-exit reconciliation 事件发送到 relay。",
			Retryable: true,
		}))
		return
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if waitErr := client.WaitForOutboundIdle(drainCtx); waitErr != nil && reportProblem != nil {
		reportProblem(agentproto.ErrorInfoFromError(waitErr, agentproto.ErrorInfo{
			Code:      "relay_drain_runtime_exit_reconciliation_failed",
			Layer:     "wrapper",
			Stage:     "runtime_exit_reconciliation",
			Operation: "provider.child.wait",
			Message:   "wrapper 在 runtime-exit reconciliation 后等待 relay outbox 排空时失败。",
			Retryable: true,
		}))
	}
}
