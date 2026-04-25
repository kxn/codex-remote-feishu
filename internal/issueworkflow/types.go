package issueworkflow

import "time"

type WorkflowMode string

const (
	WorkflowModeFull WorkflowMode = "full"
	WorkflowModeFast WorkflowMode = "fast"
)

type Repo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

func (r Repo) String() string {
	if r.Owner == "" || r.Name == "" {
		return ""
	}
	return r.Owner + "/" + r.Name
}

type Issue struct {
	Number    int            `json:"number"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	State     string         `json:"state"`
	URL       string         `json:"url,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Labels    []string       `json:"labels,omitempty"`
	Comments  []IssueComment `json:"comments,omitempty"`
}

type IssueComment struct {
	Author      string    `json:"author,omitempty"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"publishedAt"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
	URL         string    `json:"url,omitempty"`
}

type PrepareStatus string

const (
	PrepareStatusReady                   PrepareStatus = "ready"
	PrepareStatusBlockedDirtyWorktree    PrepareStatus = "blocked_dirty_worktree"
	PrepareStatusBlockedProcessingClaim  PrepareStatus = "blocked_processing_claim"
	PrepareStatusBlockedWorkflowContract PrepareStatus = "blocked_workflow_contract"
)

type ProcessingAction string

const (
	ProcessingActionNone           ProcessingAction = "none"
	ProcessingActionClaimed        ProcessingAction = "claimed"
	ProcessingActionReclaimedStale ProcessingAction = "reclaimed_stale"
)

type LintSeverity string

const (
	LintSeverityError   LintSeverity = "error"
	LintSeverityWarning LintSeverity = "warning"
	LintSeverityInfo    LintSeverity = "info"
)

type LintFinding struct {
	Severity LintSeverity `json:"severity"`
	Code     string       `json:"code"`
	Message  string       `json:"message"`
}

type LintReport struct {
	WorkflowMode         WorkflowMode       `json:"workflowMode,omitempty"`
	CurrentRecordedState string             `json:"currentRecordedState"`
	RequiredMissing      []string           `json:"requiredMissing,omitempty"`
	PreferredMissing     []string           `json:"preferredMissing,omitempty"`
	StatusLabels         []string           `json:"statusLabels,omitempty"`
	CategoryLabels       []string           `json:"categoryLabels,omitempty"`
	ScopeLabels          []string           `json:"scopeLabels,omitempty"`
	WorkflowGuardrails   WorkflowGuardrails `json:"workflowGuardrails,omitempty"`
	Findings             []LintFinding      `json:"findings,omitempty"`
}

type WorkflowGuardrails struct {
	ImplementableNow              bool     `json:"implementableNow,omitempty"`
	ExecutionDecisionRequired     bool     `json:"executionDecisionRequired,omitempty"`
	ExecutionDecisionSectionFound bool     `json:"executionDecisionSectionFound,omitempty"`
	ExecutionDecisionMissingItems []string `json:"executionDecisionMissingItems,omitempty"`
	SnapshotRequired              bool     `json:"snapshotRequired,omitempty"`
	SnapshotMissingFields         []string `json:"snapshotMissingFields,omitempty"`
	CloseoutTailOnly              bool     `json:"closeoutTailOnly,omitempty"`
	SnapshotContradictions        []string `json:"snapshotContradictions,omitempty"`
}

type PrepareOptions struct {
	Repo                   Repo
	IssueNumber            int
	CommentsLimit          int
	ClaimProcessing        bool
	ReclaimStaleProcessing bool
	StaleProcessingAfter   time.Duration
	SnapshotPath           string
	WorkflowMode           WorkflowMode
}

type PrepareResult struct {
	Status            PrepareStatus    `json:"status"`
	Repo              string           `json:"repo"`
	Branch            string           `json:"branch,omitempty"`
	Head              string           `json:"head,omitempty"`
	IssueNumber       int              `json:"issueNumber"`
	DirtyTrackedFiles []string         `json:"dirtyTrackedFiles,omitempty"`
	ProcessingAction  ProcessingAction `json:"processingAction,omitempty"`
	SnapshotPath      string           `json:"snapshotPath,omitempty"`
	Issue             *Issue           `json:"issue,omitempty"`
	Lint              LintReport       `json:"lint"`
}

type LintOptions struct {
	Repo          Repo
	IssueNumber   int
	CommentsLimit int
	WorkflowMode  WorkflowMode
}

type LintResult struct {
	Repo        string     `json:"repo"`
	IssueNumber int        `json:"issueNumber"`
	Issue       *Issue     `json:"issue,omitempty"`
	Lint        LintReport `json:"lint"`
}

type CheckStatus string

const (
	CheckStatusPass    CheckStatus = "pass"
	CheckStatusFail    CheckStatus = "fail"
	CheckStatusWarning CheckStatus = "warning"
)

type CheckResult struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Message string      `json:"message"`
}

type FinishOptions struct {
	Repo              Repo
	IssueNumber       int
	CommentFile       string
	CloseIssue        bool
	ReleaseProcessing bool
	SkipChecks        bool
	WorkflowMode      WorkflowMode
}

type FinishResult struct {
	Repo               string        `json:"repo"`
	IssueNumber        int           `json:"issueNumber"`
	ChangedFiles       []string      `json:"changedFiles,omitempty"`
	Checks             []CheckResult `json:"checks,omitempty"`
	CommentPosted      bool          `json:"commentPosted,omitempty"`
	IssueClosed        bool          `json:"issueClosed,omitempty"`
	ProcessingReleased bool          `json:"processingReleased,omitempty"`
}

type ClosePlanOptions struct {
	Repo         Repo
	IssueNumber  int
	WorkflowMode WorkflowMode
}

type ClosePlanAction struct {
	Code          string `json:"code"`
	BlockingCheck string `json:"blockingCheck,omitempty"`
	Summary       string `json:"summary"`
	Command       string `json:"command,omitempty"`
}

type ClosePlanResult struct {
	Repo         string            `json:"repo"`
	IssueNumber  int               `json:"issueNumber"`
	WorkflowMode WorkflowMode      `json:"workflowMode"`
	CloseReady   bool              `json:"closeReady"`
	Issue        *Issue            `json:"issue,omitempty"`
	Lint         LintReport        `json:"lint"`
	Checks       []CheckResult     `json:"checks,omitempty"`
	NextActions  []ClosePlanAction `json:"nextActions,omitempty"`
}
