package work

import (
	"testing"
	"time"
)

func source() SourceRef {
	return SourceRef{Kind: "message", ID: "msg", Version: "1", EvidenceRef: "receipt"}
}
func TestScopeOwnershipIsolation(t *testing.T) {
	tests := []struct {
		name  string
		scope WorkScope
		valid bool
	}{
		{"personal", WorkScope{ID: "s", Kind: ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, true},
		{"last speaker cannot own project", WorkScope{ID: "s", Kind: ScopeProject, PrincipalID: "p", ProjectID: "project", TeamID: "team", Revision: 1}, false},
		{"project", WorkScope{ID: "s", Kind: ScopeProject, ProjectID: "project", TeamID: "team", Revision: 1}, true},
		{"personal cannot leak to team", WorkScope{ID: "s", Kind: ScopePersonal, PrincipalID: "p", RoleID: "r", TeamID: "team", Revision: 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.Validate(); (got == nil) != tt.valid {
				t.Fatalf("Validate()=%v, valid=%v", got, tt.valid)
			}
		})
	}
}
func TestStateTransitionsDoNotInferRecovery(t *testing.T) {
	if err := ValidateWorkerTransition(WorkerUnavailable, WorkerReady); err == nil {
		t.Fatal("unavailable worker bypassed reconciliation")
	}
	if err := ValidateWorkerTransition(WorkerUnavailable, WorkerRecovering); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttemptTransition(AttemptReconciling, AttemptRunning); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttemptTransition(AttemptCompleted, AttemptRunning); err == nil {
		t.Fatal("terminal attempt resurrected")
	}
	if err := ValidateOperationTransition(OperationUnknown, OperationSending); err == nil {
		t.Fatal("unknown effect was blindly retried")
	}
	if err := ValidateTaskTransition(TaskDone, TaskActive); err == nil {
		t.Fatal("late result reopened task")
	}
	if err := ValidateProjectTransition(ProjectArchived, ProjectActive); err == nil {
		t.Fatal("reopen blindly resumed work")
	}
	if err := ValidateInboxTransition(InboxState("typo"), InboxState("typo")); err == nil {
		t.Fatal("unknown enum accepted")
	}
}
func TestDependencyGraph(t *testing.T) {
	tasks := []Task{{ID: "a", ScopeID: "s"}, {ID: "b", ScopeID: "s"}, {ID: "c", ScopeID: "s"}, {ID: "foreign", ScopeID: "x"}}
	edge := func(a, b string) Dependency {
		return Dependency{TaskID: a, DependsOnTaskID: b, Condition: DependencyResultAvailable}
	}
	if err := ValidateDependencies(tasks, []Dependency{edge("a", "b"), edge("b", "c")}); err != nil {
		t.Fatal(err)
	}
	for _, deps := range [][]Dependency{{edge("a", "a")}, {edge("a", "b"), edge("b", "c"), edge("c", "a")}, {edge("a", "foreign")}, {edge("a", "missing")}, {edge("a", "b"), edge("a", "b")}} {
		if err := ValidateDependencies(tasks, deps); err == nil {
			t.Fatalf("invalid graph accepted: %+v", deps)
		}
	}
}
func TestIndependentPauseSources(t *testing.T) {
	gates := []PauseGate{{ID: "project", ScopeID: "s", Source: GateProject, SourceID: "project", State: GateOpen, Revision: 2}, {ID: "task", ScopeID: "s", TaskID: "task", Source: GateTask, SourceID: "user", State: GateClosed, Revision: 1}}
	if allowed, err := GatesAllowRun("s", "task", gates); err != nil || allowed {
		t.Fatalf("project resume cleared task pause: %v %v", allowed, err)
	}
	if allowed, err := GatesAllowRun("s", "other", gates); err != nil || !allowed {
		t.Fatalf("task pause blocked independent task: %v %v", allowed, err)
	}
}
func TestRunRefRejectsStaleGeneration(t *testing.T) {
	worker := WorkerSession{ID: "w", ContextID: "c", ScopeID: "s", RoleID: "r", Backend: BackendCodex, State: WorkerReady, Generation: 2, Controller: ControllerAutomatic, ControllerGeneration: 1, Revision: 1}
	attempt := ExecutionAttempt{ID: "a", ContextID: "c", ScopeID: "s", RunRef: RunRef{WorkerID: "w", Generation: 1, RunID: "run"}, State: AttemptRunning, CommandID: "command", Revision: 1}
	if err := ValidateAttemptBinding(attempt, worker); err == nil {
		t.Fatal("old binding accepted")
	}
	attempt.RunRef.Generation = 2
	if err := ValidateAttemptBinding(attempt, worker); err != nil {
		t.Fatal(err)
	}
	attempt.State = AttemptCompleted
	if err := attempt.Validate(); err == nil {
		t.Fatal("completion without evidence accepted")
	}
	attempt.TerminalEvidence = []SourceRef{source()}
	if err := attempt.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestConsumptionRequiresExactInputClaim(t *testing.T) {
	attempt := ExecutionAttempt{ID: "a", ScopeID: "s", ContextID: "c", InputIDs: []string{"old"}}
	old := Inbox{ID: "old", ScopeID: "s", RecipientContextID: "c", State: InboxClaimed, ClaimedAttemptID: "a"}
	late := Inbox{ID: "late", ScopeID: "s", RecipientContextID: "c", State: InboxPending}
	if err := ValidateInputConsumption(attempt, []Inbox{old, late}, []string{"old"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInputConsumption(attempt, []Inbox{old, late}, []string{"old", "late"}); err == nil {
		t.Fatal("late input marked consumed")
	}
	old.ClaimedAttemptID = "other"
	if err := ValidateInputConsumption(attempt, []Inbox{old}, []string{"old"}); err == nil {
		t.Fatal("foreign claim marked consumed")
	}
	read := time.Now()
	inbox := Inbox{ID: "input", ScopeID: "s", RecipientRoleID: "r", EventID: "event", State: InboxProcessed, ClaimedAttemptID: "a", ReadAt: &read, Revision: 1}
	if err := inbox.Validate(); err == nil {
		t.Fatal("read receipt inferred processing")
	}
}
func TestDecisionPinsActorAndRevision(t *testing.T) {
	actor := Actor{Kind: ActorPrincipal, ID: "human"}
	request := DecisionRequest{ID: "request", ScopeID: "s", Kind: DecisionBusiness, State: DecisionOpen, Owner: actor, AllowedActors: []Actor{actor}, AgreementID: "agreement", AgreementRevision: 2, RequestGeneration: 3, Revision: 4}
	decision := Decision{ID: "decision", RequestID: "request", RequestRevision: 4, RequestGeneration: 3, AgreementRevision: 2, Actor: actor, Choice: "approve", Source: source()}
	if err := ValidateDecision(request, decision); err != nil {
		t.Fatal(err)
	}
	decision.AgreementRevision = 1
	if err := ValidateDecision(request, decision); err == nil {
		t.Fatal("stale card decision accepted")
	}
	decision.AgreementRevision = 2
	decision.Actor = Actor{Kind: ActorRole, ID: "leader"}
	if err := ValidateDecision(request, decision); err == nil {
		t.Fatal("delegation inferred human approval")
	}
}
func TestReplayRejectsChangedBodyAndTarget(t *testing.T) {
	existing := Operation{Actor: Actor{Kind: ActorPrincipal, ID: "p"}, ScopeID: "s", Kind: "send", IdempotencyKey: "k", RequestHash: "hash", TargetID: "doc", TargetGeneration: 1}
	incoming := existing
	if err := ValidateReplay(existing, incoming); err != nil {
		t.Fatal(err)
	}
	incoming.RequestHash = "changed"
	if err := ValidateReplay(existing, incoming); err == nil {
		t.Fatal("same key accepted different body")
	}
	incoming = existing
	incoming.TargetGeneration++
	if err := ValidateReplay(existing, incoming); err == nil {
		t.Fatal("same key applied to new binding")
	}
}
func TestResourceVersionSemantics(t *testing.T) {
	snapshot := ResourceVersion{ID: "v", ResourceID: "resource", ScopeID: "s", Kind: VersionImmutableSnapshot, Source: source(), ContentRef: "blob", ContentHash: "sha256:hash", Consistency: ConsistencyAtomic, Saved: true, Complete: true}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	dynamic := snapshot
	dynamic.Kind = VersionCurrentReference
	dynamic.Consistency = ConsistencyCurrent
	if err := dynamic.Validate(); err == nil {
		t.Fatal("current link masquerades as immutable")
	}
	delivery := Delivery{ID: "delivery", ScopeID: "s", Audience: []Actor{{Kind: ActorPrincipal, ID: "p"}}, Purpose: "result", Summary: "done", References: []DeliveryRef{{ResourceID: "resource", VersionID: "v"}}, Retention: RetentionPermanent, ConversationTarget: "chat", SourceAttemptID: "a", NotificationOutboxID: "o", Revision: 1}
	if err := ValidateDeliveryVersions(delivery, []ResourceVersion{snapshot}); err != nil {
		t.Fatal(err)
	}
	snapshot.ScopeID = "foreign"
	if err := ValidateDeliveryVersions(delivery, []ResourceVersion{snapshot}); err == nil {
		t.Fatal("cross-project version delivered")
	}
	if err := (DeliveryRef{ResourceID: "r", VersionID: "v", Current: true}).Validate(); err == nil {
		t.Fatal("ambiguous delivery version accepted")
	}
}
func TestMemoryScopeCannotExposePersonalData(t *testing.T) {
	if err := (MemoryScope{Kind: MemoryPersonal, PrincipalID: "p", ProjectID: "project"}).Validate(); err == nil {
		t.Fatal("personal memory promoted to project")
	}
	if err := (MemoryScope{Kind: MemoryRoleProject, RoleID: "r", ProjectID: "project"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (SourceRef{Kind: "doc", ID: "url", Version: "latest"}).Validate(); err == nil {
		t.Fatal("source without evidence accepted")
	}
}

func TestCompletionRequiresCurrentDeclaredEvidence(t *testing.T) {
	requirement := TaskRequirementRef{ID: "req", ScopeID: "s", Source: source(), AdoptedBy: "p", RequiredEvidence: []string{"review"}}
	task := Task{ID: "task", ScopeID: "s", Title: "work", OwnerRoleID: "r", AssignmentGeneration: 1, RequirementsRevision: 1, State: TaskActive, Requirements: []TaskRequirementRef{requirement}, Revision: 2}
	if err := ValidateTaskCompletion(task, nil); err == nil {
		t.Fatal("declared review omitted")
	}
	evidence := RequirementEvidence{ID: "e", ScopeID: "s", TaskID: "task", RequirementsRevision: 1, AssignmentGeneration: 1, RequirementID: "req", RequirementVersion: "1", Kind: "review", Source: source(), AcceptedBy: Actor{Kind: ActorRole, ID: "reviewer"}}
	if err := ValidateTaskCompletion(task, []RequirementEvidence{evidence}); err != nil {
		t.Fatal(err)
	}
	evidence.RequirementsRevision = 2
	if err := ValidateTaskCompletion(task, []RequirementEvidence{evidence}); err == nil {
		t.Fatal("old review satisfies revised task")
	}
	evidence.RequirementsRevision = 1
	evidence.Obsolete = true
	if err := ValidateTaskCompletion(task, []RequirementEvidence{evidence}); err == nil {
		t.Fatal("obsolete evidence accepted")
	}
	task.Requirements = nil
	if err := ValidateTaskCompletion(task, nil); err != nil {
		t.Fatalf("unadopted human approval invented: %v", err)
	}
}

func TestDeclaredEvidenceActorRequiresActualPrincipal(t *testing.T) {
	requirement := TaskRequirementRef{ID: "req", ScopeID: "s", Source: source(), AdoptedBy: "p", RequiredEvidence: []string{"review"}, EvidenceActors: map[string]ActorKind{"review": ActorPrincipal}}
	task := Task{ID: "task", ScopeID: "s", Title: "work", OwnerRoleID: "r", AssignmentGeneration: 1, RequirementsRevision: 1, State: TaskChecking, Requirements: []TaskRequirementRef{requirement}, Revision: 1}
	evidence := RequirementEvidence{ID: "e", ScopeID: "s", TaskID: "task", RequirementsRevision: 1, AssignmentGeneration: 1, RequirementID: "req", RequirementVersion: "1", Kind: "review", Source: source(), AcceptedBy: Actor{Kind: ActorRole, ID: "reviewer"}}
	if err := ValidateTaskCompletion(task, []RequirementEvidence{evidence}); err == nil {
		t.Fatal("role conclusion impersonated declared human review")
	}
	evidence.AcceptedBy = Actor{Kind: ActorPrincipal, ID: "human"}
	if err := ValidateTaskCompletion(task, []RequirementEvidence{evidence}); err != nil {
		t.Fatal(err)
	}
	requirement.EvidenceActors = map[string]ActorKind{"undeclared": ActorPrincipal}
	if err := requirement.Validate(); err == nil {
		t.Fatal("constraint introduced undeclared evidence")
	}
}

func TestContextPauseDoesNotStopOtherPersonalWorkstreams(t *testing.T) {
	gates := []PauseGate{{ID: "stop", ScopeID: "personal", ContextID: "first", Source: GateContext, SourceID: "human-stop", State: GateClosed, Revision: 1}}
	if allowed, err := GatesAllowRunForContext("personal", "", "first", gates); err != nil || allowed {
		t.Fatalf("target context was not paused %v %v", allowed, err)
	}
	if allowed, err := GatesAllowRunForContext("personal", "", "second", gates); err != nil || !allowed {
		t.Fatalf("independent private workstream paused %v %v", allowed, err)
	}
	if allowed, err := GatesAllowRun("personal", "", gates); err != nil || !allowed {
		t.Fatal("scope-only check expanded context pause globally")
	}
}
