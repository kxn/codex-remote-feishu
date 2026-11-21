package work

import (
	"fmt"
	"time"
)

type TaskState string

const (
	TaskTodo      TaskState = "todo"
	TaskActive    TaskState = "active"
	TaskWaiting   TaskState = "waiting"
	TaskChecking  TaskState = "checking"
	TaskDone      TaskState = "done"
	TaskCancelled TaskState = "cancelled"
)

type GateState string

const (
	GateOpen   GateState = "open"
	GateClosed GateState = "closed"
)

type GateSource string

const (
	GateProject        GateSource = "project"
	GateTask           GateSource = "task"
	GateContext        GateSource = "context"
	GateHumanControl   GateSource = "human_control"
	GateReconciliation GateSource = "reconciliation"
)

type PauseGate struct {
	ContextID string     `json:"context_id,omitempty"`
	ID        string     `json:"id"`
	ScopeID   string     `json:"scope_id"`
	TaskID    string     `json:"task_id,omitempty"`
	Source    GateSource `json:"source"`
	SourceID  string     `json:"source_id"`
	State     GateState  `json:"state"`
	Revision  int64      `json:"revision"`
}

func (v PauseGate) Validate() error {
	if err := required(v.ID, v.ScopeID, v.SourceID); err != nil {
		return err
	}
	if !oneOf(v.Source, GateProject, GateTask, GateContext, GateHumanControl, GateReconciliation) || !oneOf(v.State, GateOpen, GateClosed) {
		return fmt.Errorf("invalid pause gate")
	}
	if v.Source == GateContext && v.ContextID == "" {
		return fmt.Errorf("context gate needs context")
	}
	if v.Source == GateTask && v.TaskID == "" {
		return fmt.Errorf("task gate needs task")
	}
	return positive("gate revision", v.Revision)
}

// GatesAllowRun checks every independent source; reopening a project gate never
// clears a task-specific or human-control gate.
func GatesAllowRun(scopeID, taskID string, gates []PauseGate) (bool, error) {
	return GatesAllowRunForContext(scopeID, taskID, "", gates)
}
func GatesAllowRunForContext(scopeID, taskID, contextID string, gates []PauseGate) (bool, error) {
	return GatesAllowRunForPurpose(scopeID, taskID, contextID, "", gates)
}

func GatesAllowRunForPurpose(scopeID, taskID, contextID string, purpose ContextPurpose, gates []PauseGate) (bool, error) {
	if err := required(scopeID); err != nil {
		return false, err
	}
	for _, gate := range gates {
		if err := gate.Validate(); err != nil {
			return false, err
		}
		if purpose == PurposeControl && gate.Source == GateProject && gate.ScopeID == scopeID {
			continue
		}
		if gate.ScopeID == scopeID && (gate.TaskID == "" || gate.TaskID == taskID) && (gate.ContextID == "" || gate.ContextID == contextID) && gate.State == GateClosed {
			return false, nil
		}
	}
	return true, nil
}

type TaskRequirementRef struct {
	EvidenceActors   map[string]ActorKind `json:"evidence_actors,omitempty"`
	ID               string               `json:"id"`
	ScopeID          string               `json:"scope_id"`
	Source           SourceRef            `json:"source"`
	AdoptedBy        string               `json:"adopted_by"`
	AdoptedAt        time.Time            `json:"adopted_at"`
	RequiredEvidence []string             `json:"required_evidence"`
}

func (v TaskRequirementRef) Validate() error {
	requiredKinds := map[string]bool{}
	for _, kind := range v.RequiredEvidence {
		if err := required(kind); err != nil {
			return err
		}
		if requiredKinds[kind] {
			return fmt.Errorf("duplicate evidence kind")
		}
		requiredKinds[kind] = true
	}
	for kind, actor := range v.EvidenceActors {
		if !requiredKinds[kind] || !oneOf(actor, ActorPrincipal, ActorRole) {
			return fmt.Errorf("evidence actor constraint requires declared evidence kind and principal/role")
		}
	}
	if err := required(v.ID, v.ScopeID, v.AdoptedBy); err != nil {
		return err
	}
	return v.Source.Validate()
}

type Task struct {
	ID                   string               `json:"id"`
	ScopeID              string               `json:"scope_id"`
	Title                string               `json:"title"`
	OwnerRoleID          string               `json:"owner_role_id"`
	RequirementsRevision int64                `json:"requirements_revision"`
	AssignmentGeneration int64                `json:"assignment_generation"`
	State                TaskState            `json:"state"`
	BusinessSource       *SourceRef           `json:"business_source,omitempty"`
	Requirements         []TaskRequirementRef `json:"requirements"`
	Revision             int64                `json:"revision"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
}

func (v Task) Validate() error {
	if err := required(v.ID, v.ScopeID, v.Title, v.OwnerRoleID); err != nil {
		return err
	}
	if err := positive("requirements revision", v.RequirementsRevision); err != nil {
		return err
	}
	if err := positive("assignment generation", v.AssignmentGeneration); err != nil {
		return err
	}
	if err := positive("task revision", v.Revision); err != nil {
		return err
	}
	if err := ValidateTaskTransition(v.State, v.State); err != nil {
		return err
	}
	if v.BusinessSource != nil {
		if err := v.BusinessSource.Validate(); err != nil {
			return err
		}
	}
	for _, ref := range v.Requirements {
		if err := ref.Validate(); err != nil {
			return err
		}
		if ref.ScopeID != v.ScopeID {
			return fmt.Errorf("requirement outside task scope")
		}
	}
	return nil
}

type DependencyCondition string

const (
	DependencyResultAvailable DependencyCondition = "result_available"
	DependencyReviewAccepted  DependencyCondition = "review_accepted"
	DependencyIntegrated      DependencyCondition = "integrated"
)

type Dependency struct {
	TaskID          string              `json:"task_id"`
	DependsOnTaskID string              `json:"depends_on_task_id"`
	Condition       DependencyCondition `json:"condition"`
	SatisfiedBy     *SourceRef          `json:"satisfied_by,omitempty"`
}

func (v Dependency) Validate() error {
	if err := required(v.TaskID, v.DependsOnTaskID); err != nil {
		return err
	}
	if v.TaskID == v.DependsOnTaskID {
		return fmt.Errorf("task cannot depend on itself")
	}
	if !oneOf(v.Condition, DependencyResultAvailable, DependencyReviewAccepted, DependencyIntegrated) {
		return fmt.Errorf("invalid dependency condition")
	}
	if v.SatisfiedBy != nil {
		return v.SatisfiedBy.Validate()
	}
	return nil
}

// ValidateDependencies rejects unknown endpoints, cross-scope edges, duplicate
// conditions and cycles, including cycles whose edges have already satisfied.
func ValidateDependencies(tasks []Task, dependencies []Dependency) error {
	scopes := map[string]string{}
	for _, task := range tasks {
		if err := required(task.ID, task.ScopeID); err != nil {
			return err
		}
		if _, ok := scopes[task.ID]; ok {
			return fmt.Errorf("duplicate task %q", task.ID)
		}
		scopes[task.ID] = task.ScopeID
	}
	edges := map[string][]string{}
	seen := map[[3]string]bool{}
	for _, dep := range dependencies {
		if err := dep.Validate(); err != nil {
			return err
		}
		scope, ok := scopes[dep.TaskID]
		if !ok {
			return fmt.Errorf("unknown task %q", dep.TaskID)
		}
		other, ok := scopes[dep.DependsOnTaskID]
		if !ok {
			return fmt.Errorf("unknown dependency %q", dep.DependsOnTaskID)
		}
		if scope != other {
			return fmt.Errorf("cross-scope dependency")
		}
		key := [3]string{dep.TaskID, dep.DependsOnTaskID, string(dep.Condition)}
		if seen[key] {
			return fmt.Errorf("duplicate dependency")
		}
		seen[key] = true
		edges[dep.TaskID] = append(edges[dep.TaskID], dep.DependsOnTaskID)
	}
	colors := map[string]uint8{}
	var visit func(string) error
	visit = func(id string) error {
		if colors[id] == 1 {
			return fmt.Errorf("dependency cycle at %q", id)
		}
		if colors[id] == 2 {
			return nil
		}
		colors[id] = 1
		for _, next := range edges[id] {
			if err := visit(next); err != nil {
				return err
			}
		}
		colors[id] = 2
		return nil
	}
	for id := range scopes {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
