package turnpatchruntime

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type CandidateKind string

const (
	CandidateKindRefusal     CandidateKind = "refusal"
	CandidateKindPlaceholder CandidateKind = "placeholder"
)

type Candidate struct {
	CandidateID   string
	MessageKey    string
	Kind          CandidateKind
	Label         string
	Excerpt       string
	DefaultText   string
	OriginalText  string
	QuestionID    string
	QuestionTitle string
}

type FlowStage string

const (
	FlowStageEditing         FlowStage = "editing"
	FlowStageApplying        FlowStage = "applying"
	FlowStageApplied         FlowStage = "applied"
	FlowStageRollbackRunning FlowStage = "rollback_running"
	FlowStageRolledBack      FlowStage = "rolled_back"
	FlowStageFailed          FlowStage = "failed"
	FlowStageCancelled       FlowStage = "cancelled"
	FlowStageExpired         FlowStage = "expired"
)

type FlowRecord struct {
	FlowID               string
	RequestID            string
	InstanceID           string
	SurfaceSessionID     string
	OwnerUserID          string
	ThreadID             string
	ThreadTitle          string
	TurnID               string
	RolloutDigest        string
	MessageID            string
	Revision             int
	CurrentQuestionIndex int
	Answers              map[string]string
	Candidates           []Candidate
	Stage                FlowStage
	StatusText           string
	ErrorText            string
	PatchID              string
	BackupPath           string
	ReplacedCount        int
	RemovedReasoning     int
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ExpiresAt            time.Time
	AppliedAt            time.Time
	RolledBackAt         time.Time
}

type TransactionKind string

const (
	TransactionKindApply    TransactionKind = "apply"
	TransactionKindRollback TransactionKind = "rollback"
)

type TransactionStage string

const (
	TransactionStageApplyingWrite         TransactionStage = "applying_write"
	TransactionStageApplyingRestart       TransactionStage = "applying_restart"
	TransactionStageApplyRecoveryRollback TransactionStage = "apply_recovery_rollback"
	TransactionStageApplyRecoveryRestart  TransactionStage = "apply_recovery_restart"
	TransactionStageRollbackWrite         TransactionStage = "rollback_write"
	TransactionStageRollbackRestart       TransactionStage = "rollback_restart"
)

type RestartResult struct {
	Accepted bool
	Error    string
	Problem  *agentproto.ErrorInfo
}

type Transaction struct {
	ID               string
	FlowID           string
	RequestID        string
	Kind             TransactionKind
	InstanceID       string
	InitiatorSurface string
	InitiatorUserID  string
	ThreadID         string
	PatchID          string
	Stage            TransactionStage
	RestartCommandID string
	RestartDeadline  time.Time
	PausedSurfaceIDs map[string]bool
	StartedAt        time.Time
	UpdatedAt        time.Time
}

type State struct {
	Storage     *codexstate.TurnPatchStorage
	ActiveFlows map[string]*FlowRecord
	ActiveTx    map[string]*Transaction
	NextFlowSeq int64
	NextTxSeq   int64
}

func NewState() State {
	return State{
		ActiveFlows: map[string]*FlowRecord{},
		ActiveTx:    map[string]*Transaction{},
	}
}
