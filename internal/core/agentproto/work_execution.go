package agentproto

import (
	"fmt"
	"strings"
)

// WorkerProtocolVersion advertises binding validation, not durable delivery.
const WorkerProtocolVersion = 1

// NativeMemoryPolicyVersion pins supported native memory controls to a fresh
// history boundary. It does not claim OS isolation or control arbitrary plugins.
const NativeMemoryPolicyVersion = 1

func (c Command) IsOwnerOnlyRuntimeCommand() bool {
	if c.Execution == nil || c.Execution.AttemptID != "" || c.Execution.RunRef != "" {
		return false
	}
	switch c.Kind {
	case CommandRuntimeInspect, CommandProcessExit, CommandModelList, CommandThreadGoalGet, CommandThreadGoalClear, CommandRuntimeMCPRead, CommandMCPOAuthLogin:
		return true
	case CommandThreadGoalSet:
		return c.Goal.Status == "paused" && c.Goal.Expected != nil && c.Goal.Expected.GoalVersion > 0 && c.Goal.Objective == c.Goal.Expected.Objective && c.Goal.TokenBudget != nil && *c.Goal.TokenBudget == c.Goal.Expected.TokenBudget
	case CommandThreadCompactStart:
		return c.Maintenance != nil && c.Maintenance.Version == NativeMaintenanceProtocolVersion && c.Maintenance.PreflightInspectionCommandID != "" && c.Maintenance.NativeWriteWatermark > 0
	}
	return false
}

type ExecutionBinding struct {
	Version              int    `json:"version"`
	ScopeID              string `json:"scopeId"`
	ContextID            string `json:"contextId"`
	RoleID               string `json:"roleId"`
	WorkerSessionID      string `json:"workerSessionId"`
	SessionGeneration    int64  `json:"sessionGeneration"`
	ControllerGeneration int64  `json:"controllerGeneration"`
	BotGeneration        int64  `json:"botGeneration"`
	AttemptID            string `json:"attemptId,omitempty"`
	RunRef               string `json:"runRef,omitempty"`
}

func (b ExecutionBinding) ValidateOwner() error {
	if b.Version != WorkerProtocolVersion {
		return fmt.Errorf("unsupported execution binding version")
	}
	for _, value := range []string{b.ScopeID, b.ContextID, b.RoleID, b.WorkerSessionID} {
		if value == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("execution owner requires canonical scope, context, role and worker IDs")
		}
	}
	if b.SessionGeneration <= 0 || b.ControllerGeneration <= 0 || b.BotGeneration <= 0 {
		return fmt.Errorf("execution owner requires positive generations")
	}
	return nil
}

func (b ExecutionBinding) ValidateExecution() error {
	if err := b.ValidateOwner(); err != nil {
		return err
	}
	if b.AttemptID == "" || b.RunRef == "" || b.AttemptID != strings.TrimSpace(b.AttemptID) || b.RunRef != strings.TrimSpace(b.RunRef) {
		return fmt.Errorf("execution requires explicit attempt ID and run reference")
	}
	return nil
}

func (b ExecutionBinding) SameOwner(other ExecutionBinding) bool {
	return b.Version == other.Version && b.ScopeID == other.ScopeID && b.ContextID == other.ContextID &&
		b.RoleID == other.RoleID && b.WorkerSessionID == other.WorkerSessionID &&
		b.SessionGeneration == other.SessionGeneration && b.ControllerGeneration == other.ControllerGeneration &&
		b.BotGeneration == other.BotGeneration
}

func CloneExecutionBinding(binding *ExecutionBinding) *ExecutionBinding {
	if binding == nil {
		return nil
	}
	clone := *binding
	return &clone
}

type InstructionBundle struct {
	Revision string `json:"revision"`
	Text     string `json:"text"`
}

// RuntimeLaunchSnapshot is frozen for a wrapper lifetime. Credentials are
// resolved separately; this contract carries only their controlled reference.
type RuntimeLaunchSnapshot struct {
	NativeBinaryPath          string            `json:"nativeBinaryPath,omitempty"`
	NativeBinaryVersion       string            `json:"nativeBinaryVersion,omitempty"`
	ReadOnly                  bool              `json:"readOnly,omitempty"`
	HostID                    string            `json:"hostId,omitempty"`
	NativeMemoryPolicyVersion int               `json:"nativeMemoryPolicyVersion,omitempty"`
	InstanceGeneration        int64             `json:"instanceGeneration,omitempty"`
	ProcessGeneration         int64             `json:"processGeneration,omitempty"`
	LocationGeneration        int64             `json:"locationGeneration,omitempty"`
	JournalPath               string            `json:"journalPath,omitempty"`
	CarrierIncarnation        string            `json:"carrierIncarnation,omitempty"`
	Version                   int               `json:"version"`
	Owner                     ExecutionBinding  `json:"owner"`
	CWD                       string            `json:"cwd"`
	InstructionBundle         InstructionBundle `json:"instructionBundle"`
	ToolEndpoint              string            `json:"toolEndpoint,omitempty"`
	SessionCredentialRef      string            `json:"sessionCredentialRef,omitempty"`
}

func (s RuntimeLaunchSnapshot) Validate() error {
	if s.NativeMemoryPolicyVersion != 0 && s.NativeMemoryPolicyVersion != NativeMemoryPolicyVersion {
		return fmt.Errorf("unsupported native memory policy")
	}
	if (s.JournalPath == "") != (s.CarrierIncarnation == "") {
		return fmt.Errorf("journal path and carrier incarnation must be supplied together")
	}
	if s.Version != WorkerProtocolVersion {
		return fmt.Errorf("unsupported runtime launch snapshot version")
	}
	if err := s.Owner.ValidateOwner(); err != nil {
		return err
	}
	if s.Owner.AttemptID != "" || s.Owner.RunRef != "" {
		return fmt.Errorf("launch owner must not contain an attempt or run reference")
	}
	if strings.TrimSpace(s.CWD) == "" || strings.TrimSpace(s.InstructionBundle.Revision) == "" || strings.TrimSpace(s.InstructionBundle.Text) == "" {
		return fmt.Errorf("launch snapshot requires cwd and a versioned instruction bundle")
	}
	return nil
}
