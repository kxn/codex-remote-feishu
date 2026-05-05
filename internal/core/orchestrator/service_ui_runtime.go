package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type pathPickerMode string

const (
	pathPickerModeDirectory pathPickerMode = "directory"
	pathPickerModeFile      pathPickerMode = "file"
)

type ownerCardFlowKind string

const (
	ownerCardFlowKindCommandMenu   ownerCardFlowKind = "command_menu"
	ownerCardFlowKindThreadHistory ownerCardFlowKind = "thread_history"
	ownerCardFlowKindReviewPicker  ownerCardFlowKind = "review_picker"
	ownerCardFlowKindTargetPicker  ownerCardFlowKind = "target_picker"
	ownerCardFlowKindCompact       ownerCardFlowKind = "compact"
	ownerCardFlowKindPlanProposal  ownerCardFlowKind = "plan_proposal"
	ownerCardFlowKindWorkspacePage ownerCardFlowKind = "workspace_page"
)

type frontstageFlowRole string

const (
	frontstageFlowRoleLauncher frontstageFlowRole = "launcher"
	frontstageFlowRoleOwner    frontstageFlowRole = "owner"
)

type ownerCardFlowPhase string

const (
	ownerCardFlowPhaseLoading   ownerCardFlowPhase = "loading"
	ownerCardFlowPhaseResolved  ownerCardFlowPhase = "resolved"
	ownerCardFlowPhaseError     ownerCardFlowPhase = "error"
	ownerCardFlowPhaseEditing   ownerCardFlowPhase = "editing"
	ownerCardFlowPhaseRunning   ownerCardFlowPhase = "running"
	ownerCardFlowPhaseCompleted ownerCardFlowPhase = "completed"
	ownerCardFlowPhaseCancelled ownerCardFlowPhase = "cancelled"
)

type activeOwnerCardFlowRecord struct {
	FlowID          string
	Kind            ownerCardFlowKind
	Role            frontstageFlowRole
	OwnerUserID     string
	MessageID       string
	Revision        int
	Phase           ownerCardFlowPhase
	LauncherPhase   string
	CommandID       string
	OriginMenuNode  string
	CurrentMenuNode string
	BackTarget      string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type activeTargetPickerRecord struct {
	PickerID              string
	OwnerUserID           string
	Source                control.TargetPickerRequestSource
	CatalogFamilyID       string
	CatalogVariantID      string
	CatalogBackend        agentproto.Backend
	Stage                 control.FeishuTargetPickerStage
	StatusTitle           string
	StatusText            string
	StatusSections        []control.FeishuCardTextSection
	StatusFooter          string
	Messages              []control.FeishuTargetPickerMessage
	PendingKind           targetPickerPendingKind
	PendingWorkspaceKey   string
	PendingThreadID       string
	Page                  control.FeishuTargetPickerPage
	BackValue             map[string]any
	LockedWorkspaceKey    string
	AllowNewThread        bool
	WorkspaceCursor       int
	SessionCursor         int
	SelectedWorkspaceKey  string
	SelectedSessionValue  string
	LocalDirectoryPath    string
	GitParentDir          string
	GitRepoURL            string
	GitDirectoryName      string
	GitFinalPath          string
	WorktreeBranchName    string
	WorktreeDirectoryName string
	WorktreeFinalPath     string
	CreatedAt             time.Time
	ExpiresAt             time.Time
}

type activeThreadHistoryRecord struct {
	ThreadID string
	ViewMode control.FeishuThreadHistoryViewMode
	Page     int
	TurnID   string
}

type activeReviewPickerRecord struct {
	InstanceID     string
	ParentThreadID string
	ThreadCWD      string
	RecentCommits  []gitmeta.CommitSummary
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

type activePlanProposalRecord struct {
	ProposalID            string
	InstanceID            string
	ThreadID              string
	TurnID                string
	ThreadCWD             string
	PlanText              string
	TemporarySessionLabel string
	CreatedAt             time.Time
	ExpiresAt             time.Time
}

type activeWorkspacePageRecord struct {
	FlowID      string
	CommandID   string
	OwnerUserID string
	MessageID   string
	FromMenu    bool
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type activePathPickerRecord struct {
	PickerID        string
	MessageID       string
	OwnerUserID     string
	OwnerFlowID     string
	Mode            pathPickerMode
	Title           string
	StageLabel      string
	Question        string
	RootPath        string
	CurrentPath     string
	SelectedPath    string
	DirectoryCursor int
	FileCursor      int
	StatusTitle     string
	StatusText      string
	StatusSections  []control.FeishuCardTextSection
	StatusFooter    string
	Hint            string
	ConfirmLabel    string
	CancelLabel     string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	ConsumerKind    string
	ConsumerMeta    map[string]string
	EntryFilterKind string
	EntryFilterMeta map[string]string
}

type surfaceUIRuntimeRecord struct {
	ActiveOwnerCardFlow *activeOwnerCardFlowRecord
	ActiveTargetPicker  *activeTargetPickerRecord
	ActiveThreadHistory *activeThreadHistoryRecord
	ActiveReviewPicker  *activeReviewPickerRecord
	ActivePathPicker    *activePathPickerRecord
	ActivePlanProposal  *activePlanProposalRecord
	ActiveWorkspacePage *activeWorkspacePageRecord
}

type SurfaceUIRuntimeSummary struct {
	ActiveOwnerCardFlowID    string
	ActiveOwnerCardFlowKind  string
	ActiveOwnerCardFlowRole  string
	ActiveOwnerCardFlowPhase string
	ActiveOwnerCardRevision  int
	ActiveTargetPickerID     string
	ActiveThreadHistoryID    string
	ActiveReviewPicker       bool
	ActivePathPickerID       string
	ActivePlanProposalID     string
	ActiveWorkspacePageID    string
}

func (s *Service) surfaceUIRuntimeState(surface *state.SurfaceConsoleRecord) *surfaceUIRuntimeRecord {
	if s == nil || surface == nil {
		return nil
	}
	return s.surfaceUIRuntimeByID(surface.SurfaceSessionID)
}

func (s *Service) surfaceUIRuntimeByID(surfaceID string) *surfaceUIRuntimeRecord {
	if s == nil {
		return nil
	}
	return s.surfaceUIRuntime[strings.TrimSpace(surfaceID)]
}

func (s *Service) ensureSurfaceUIRuntime(surface *state.SurfaceConsoleRecord) *surfaceUIRuntimeRecord {
	if s == nil || surface == nil {
		return nil
	}
	surfaceID := strings.TrimSpace(surface.SurfaceSessionID)
	if surfaceID == "" {
		return nil
	}
	record := s.surfaceUIRuntime[surfaceID]
	if record != nil {
		return record
	}
	record = &surfaceUIRuntimeRecord{}
	s.surfaceUIRuntime[surfaceID] = record
	return record
}

func (s *Service) activeOwnerCardFlow(surface *state.SurfaceConsoleRecord) *activeOwnerCardFlowRecord {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return nil
	}
	return runtime.ActiveOwnerCardFlow
}

func (s *Service) setActiveOwnerCardFlow(surface *state.SurfaceConsoleRecord, record *activeOwnerCardFlowRecord) {
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveOwnerCardFlow = record
}

func (s *Service) clearSurfaceOwnerCardFlow(surface *state.SurfaceConsoleRecord) {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveOwnerCardFlow = nil
}

func (s *Service) activeTargetPicker(surface *state.SurfaceConsoleRecord) *activeTargetPickerRecord {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return nil
	}
	return runtime.ActiveTargetPicker
}

func (s *Service) setActiveTargetPicker(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) {
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveTargetPicker = record
}

func (s *Service) clearSurfaceTargetPicker(surface *state.SurfaceConsoleRecord) {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveTargetPicker = nil
}

func (s *Service) activeThreadHistory(surface *state.SurfaceConsoleRecord) *activeThreadHistoryRecord {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return nil
	}
	return runtime.ActiveThreadHistory
}

func (s *Service) setActiveThreadHistory(surface *state.SurfaceConsoleRecord, record *activeThreadHistoryRecord) {
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveThreadHistory = record
}

func (s *Service) clearSurfaceThreadHistory(surface *state.SurfaceConsoleRecord) {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveThreadHistory = nil
}

func (s *Service) activeReviewPicker(surface *state.SurfaceConsoleRecord) *activeReviewPickerRecord {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return nil
	}
	return runtime.ActiveReviewPicker
}

func (s *Service) setActiveReviewPicker(surface *state.SurfaceConsoleRecord, record *activeReviewPickerRecord) {
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveReviewPicker = record
}

func (s *Service) clearSurfaceReviewPicker(surface *state.SurfaceConsoleRecord) {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveReviewPicker = nil
}

func (s *Service) activePathPicker(surface *state.SurfaceConsoleRecord) *activePathPickerRecord {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return nil
	}
	return runtime.ActivePathPicker
}

func (s *Service) setActivePathPicker(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord) {
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime == nil {
		return
	}
	runtime.ActivePathPicker = record
}

func (s *Service) clearSurfacePathPicker(surface *state.SurfaceConsoleRecord) {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return
	}
	runtime.ActivePathPicker = nil
}

func (s *Service) activePlanProposal(surface *state.SurfaceConsoleRecord) *activePlanProposalRecord {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return nil
	}
	return runtime.ActivePlanProposal
}

func (s *Service) setActivePlanProposal(surface *state.SurfaceConsoleRecord, record *activePlanProposalRecord) {
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime == nil {
		return
	}
	runtime.ActivePlanProposal = record
}

func (s *Service) clearSurfacePlanProposal(surface *state.SurfaceConsoleRecord) {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return
	}
	runtime.ActivePlanProposal = nil
	if runtime.ActiveOwnerCardFlow != nil && runtime.ActiveOwnerCardFlow.Kind == ownerCardFlowKindPlanProposal {
		runtime.ActiveOwnerCardFlow = nil
	}
}

func (s *Service) activeWorkspacePage(surface *state.SurfaceConsoleRecord) *activeWorkspacePageRecord {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return nil
	}
	return runtime.ActiveWorkspacePage
}

func (s *Service) setActiveWorkspacePage(surface *state.SurfaceConsoleRecord, record *activeWorkspacePageRecord) {
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveWorkspacePage = record
}

func (s *Service) clearSurfaceWorkspacePage(surface *state.SurfaceConsoleRecord) {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil {
		return
	}
	runtime.ActiveWorkspacePage = nil
	if runtime.ActiveOwnerCardFlow != nil && runtime.ActiveOwnerCardFlow.Kind == ownerCardFlowKindWorkspacePage {
		runtime.ActiveOwnerCardFlow = nil
	}
}

func (s *Service) SurfaceUIRuntimeSummary(surfaceID string) SurfaceUIRuntimeSummary {
	runtime := s.surfaceUIRuntimeByID(surfaceID)
	if runtime == nil {
		return SurfaceUIRuntimeSummary{}
	}
	summary := SurfaceUIRuntimeSummary{}
	if runtime.ActiveOwnerCardFlow != nil {
		summary.ActiveOwnerCardFlowID = strings.TrimSpace(runtime.ActiveOwnerCardFlow.FlowID)
		summary.ActiveOwnerCardFlowKind = strings.TrimSpace(string(runtime.ActiveOwnerCardFlow.Kind))
		summary.ActiveOwnerCardFlowRole = strings.TrimSpace(string(runtime.ActiveOwnerCardFlow.Role))
		summary.ActiveOwnerCardFlowPhase = strings.TrimSpace(string(runtime.ActiveOwnerCardFlow.Phase))
		summary.ActiveOwnerCardRevision = runtime.ActiveOwnerCardFlow.Revision
	}
	if runtime.ActiveTargetPicker != nil {
		summary.ActiveTargetPickerID = strings.TrimSpace(runtime.ActiveTargetPicker.PickerID)
	}
	if runtime.ActiveThreadHistory != nil {
		summary.ActiveThreadHistoryID = strings.TrimSpace(summary.ActiveOwnerCardFlowID)
	}
	if runtime.ActiveReviewPicker != nil {
		summary.ActiveReviewPicker = true
	}
	if runtime.ActivePathPicker != nil {
		summary.ActivePathPickerID = strings.TrimSpace(runtime.ActivePathPicker.PickerID)
	}
	if runtime.ActivePlanProposal != nil {
		summary.ActivePlanProposalID = strings.TrimSpace(runtime.ActivePlanProposal.ProposalID)
	}
	if runtime.ActiveWorkspacePage != nil {
		summary.ActiveWorkspacePageID = strings.TrimSpace(runtime.ActiveWorkspacePage.CommandID)
	}
	return summary
}

func newOwnerCardFlowRecord(kind ownerCardFlowKind, flowID, ownerUserID string, createdAt time.Time, ttl time.Duration, phase ownerCardFlowPhase) *activeOwnerCardFlowRecord {
	flow := &activeOwnerCardFlowRecord{
		FlowID:      strings.TrimSpace(flowID),
		Kind:        kind,
		Role:        frontstageFlowRoleOwner,
		OwnerUserID: strings.TrimSpace(ownerUserID),
		Revision:    1,
		Phase:       phase,
		CreatedAt:   createdAt,
		ExpiresAt:   createdAt.Add(ttl),
	}
	if flow.Revision <= 0 {
		flow.Revision = 1
	}
	return flow
}

func bumpOwnerCardFlowRevision(flow *activeOwnerCardFlowRecord) {
	if flow == nil {
		return
	}
	flow.Revision++
	if flow.Revision <= 0 {
		flow.Revision = 1
	}
}

func refreshOwnerCardFlow(flow *activeOwnerCardFlowRecord, phase ownerCardFlowPhase, now time.Time, ttl time.Duration) {
	if flow == nil {
		return
	}
	flow.Phase = phase
	flow.CreatedAt = now
	flow.ExpiresAt = now.Add(ttl)
	bumpOwnerCardFlowRevision(flow)
}

func (s *Service) requireActiveOwnerCardFlow(surface *state.SurfaceConsoleRecord, kind ownerCardFlowKind, flowID, actorUserID, expiredText, unauthorizedText string) (*activeOwnerCardFlowRecord, []eventcontract.Event) {
	if surface == nil || s.activeOwnerCardFlow(surface) == nil {
		return nil, notice(surface, "owner_card_expired", strings.TrimSpace(expiredText))
	}
	flow := s.activeOwnerCardFlow(surface)
	if flow.Kind != kind {
		return nil, notice(surface, "owner_card_expired", strings.TrimSpace(expiredText))
	}
	if !flow.ExpiresAt.IsZero() && !flow.ExpiresAt.After(s.now()) {
		s.clearSurfaceOwnerCardFlow(surface)
		return nil, notice(surface, "owner_card_expired", strings.TrimSpace(expiredText))
	}
	if strings.TrimSpace(flowID) == "" || strings.TrimSpace(flow.FlowID) != strings.TrimSpace(flowID) {
		return nil, notice(surface, "owner_card_expired", strings.TrimSpace(expiredText))
	}
	actorUserID = strings.TrimSpace(firstNonEmpty(actorUserID, surface.ActorUserID))
	if ownerUserID := strings.TrimSpace(flow.OwnerUserID); ownerUserID != "" && actorUserID != "" && ownerUserID != actorUserID {
		return nil, notice(surface, "owner_card_unauthorized", strings.TrimSpace(unauthorizedText))
	}
	return flow, nil
}

func (s *Service) RecordOwnerCardFlowMessage(surfaceID, flowID, messageID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	flow := s.activeOwnerCardFlow(surface)
	if surface == nil || flow == nil {
		return
	}
	if strings.TrimSpace(flow.FlowID) != strings.TrimSpace(flowID) {
		return
	}
	flow.MessageID = strings.TrimSpace(messageID)
	if page := s.activeWorkspacePage(surface); page != nil && strings.TrimSpace(page.FlowID) == strings.TrimSpace(flowID) {
		page.MessageID = strings.TrimSpace(messageID)
	}
}
