package work

import "fmt"

// Self transitions are valid for known states, making validation reusable for
// idempotent updates. Unknown states and terminal-to-active moves are rejected.
func transition[T ~string](from, to T, graph map[T][]T) error {
	next, known := graph[from]
	if !known {
		return fmt.Errorf("unknown state %q", from)
	}
	if _, known = graph[to]; !known {
		return fmt.Errorf("unknown state %q", to)
	}
	if from == to {
		return nil
	}
	for _, state := range next {
		if state == to {
			return nil
		}
	}
	return fmt.Errorf("illegal transition %s -> %s", from, to)
}
func ValidateTaskTransition(from, to TaskState) error {
	return transition(from, to, map[TaskState][]TaskState{
		TaskTodo: {TaskActive, TaskCancelled}, TaskActive: {TaskWaiting, TaskChecking, TaskDone, TaskCancelled}, TaskWaiting: {TaskActive, TaskChecking, TaskDone, TaskCancelled}, TaskChecking: {TaskActive, TaskWaiting, TaskDone, TaskCancelled}, TaskDone: {}, TaskCancelled: {},
	})
}
func ValidateWorkerTransition(from, to WorkerState) error {
	return transition(from, to, map[WorkerState][]WorkerState{
		WorkerPreparing: {WorkerReady, WorkerRecovering, WorkerUnavailable, WorkerRetired}, WorkerReady: {WorkerBusy, WorkerMaintaining, WorkerHandoff, WorkerRecovering, WorkerUnavailable, WorkerRetired}, WorkerBusy: {WorkerReady, WorkerRecovering, WorkerHandoff, WorkerUnavailable}, WorkerMaintaining: {WorkerReady, WorkerRecovering, WorkerUnavailable, WorkerRetired}, WorkerRecovering: {WorkerReady, WorkerUnavailable, WorkerRetired}, WorkerHandoff: {WorkerReady, WorkerRecovering, WorkerUnavailable, WorkerRetired}, WorkerUnavailable: {WorkerRecovering, WorkerRetired}, WorkerRetired: {},
	})
}
func ValidateAttemptTransition(from, to AttemptState) error {
	return transition(from, to, map[AttemptState][]AttemptState{
		AttemptPrepared: {AttemptDispatchPending, AttemptInterrupted, AttemptFailed}, AttemptDispatchPending: {AttemptRunning, AttemptReconciling, AttemptInterrupted, AttemptFailed}, AttemptRunning: {AttemptCompleted, AttemptInterrupted, AttemptFailed, AttemptReconciling}, AttemptReconciling: {AttemptRunning, AttemptCompleted, AttemptInterrupted, AttemptFailed}, AttemptCompleted: {}, AttemptInterrupted: {}, AttemptFailed: {},
	})
}
func ValidateProjectTransition(from, to ProjectState) error {
	return transition(from, to, map[ProjectState][]ProjectState{
		ProjectProvisioning: {ProjectActive, ProjectDegraded, ProjectArchiving}, ProjectActive: {ProjectDegraded, ProjectPausing, ProjectArchiving}, ProjectDegraded: {ProjectActive, ProjectPausing, ProjectArchiving}, ProjectPausing: {ProjectPaused, ProjectDegraded, ProjectArchiving}, ProjectPaused: {ProjectActive, ProjectDegraded, ProjectArchiving}, ProjectArchiving: {ProjectArchived, ProjectDegraded}, ProjectArchived: {ProjectPaused},
	})
}
func ValidateInboxTransition(from, to InboxState) error {
	return transition(from, to, map[InboxState][]InboxState{InboxPending: {InboxClaimed, InboxCancelled}, InboxClaimed: {InboxProcessed, InboxPending}, InboxProcessed: {}, InboxCancelled: {}})
}
func ValidateOperationTransition(from, to OperationState) error {
	return transition(from, to, map[OperationState][]OperationState{OperationPrepared: {OperationSending, OperationTerminal}, OperationSending: {OperationConfirmed, OperationRetryable, OperationTerminal, OperationUnknown}, OperationRetryable: {OperationSending, OperationTerminal}, OperationUnknown: {OperationConfirmed, OperationRetryable, OperationTerminal}, OperationConfirmed: {}, OperationTerminal: {}})
}
func ValidateDecisionTransition(from, to DecisionState) error {
	return transition(from, to, map[DecisionState][]DecisionState{DecisionOpen: {DecisionRecorded, DecisionSuperseded, DecisionAborted}, DecisionRecorded: {DecisionApplying, DecisionSuperseded, DecisionAborted}, DecisionApplying: {DecisionResolved, DecisionUnknown}, DecisionUnknown: {DecisionResolved, DecisionApplying, DecisionAborted}, DecisionResolved: {}, DecisionSuperseded: {}, DecisionAborted: {}})
}
