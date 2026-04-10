package issueworkflow

import "time"

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
	PrepareStatusReady                  PrepareStatus = "ready"
	PrepareStatusBlockedDirtyWorktree   PrepareStatus = "blocked_dirty_worktree"
	PrepareStatusBlockedProcessingClaim PrepareStatus = "blocked_processing_claim"
)

type ProcessingAction string

const (
	ProcessingActionNone    ProcessingAction = "none"
	ProcessingActionClaimed ProcessingAction = "claimed"
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
	CurrentRecordedState string        `json:"currentRecordedState"`
	RequiredMissing      []string      `json:"requiredMissing,omitempty"`
	PreferredMissing     []string      `json:"preferredMissing,omitempty"`
	StatusLabels         []string      `json:"statusLabels,omitempty"`
	CategoryLabels       []string      `json:"categoryLabels,omitempty"`
	ScopeLabels          []string      `json:"scopeLabels,omitempty"`
	Findings             []LintFinding `json:"findings,omitempty"`
}

type PrepareOptions struct {
	Repo            Repo
	IssueNumber     int
	CommentsLimit   int
	ClaimProcessing bool
	SnapshotPath    string
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
