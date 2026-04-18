package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type pathPickerMode string

const (
	pathPickerModeDirectory pathPickerMode = "directory"
	pathPickerModeFile      pathPickerMode = "file"
)

type ownerCardFlowKind string

const (
	ownerCardFlowKindThreadHistory ownerCardFlowKind = "thread_history"
	ownerCardFlowKindTargetPicker  ownerCardFlowKind = "target_picker"
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
	FlowID      string
	Kind        ownerCardFlowKind
	OwnerUserID string
	MessageID   string
	Revision    int
	Phase       ownerCardFlowPhase
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type activeTargetPickerRecord struct {
	PickerID             string
	OwnerUserID          string
	Source               control.TargetPickerRequestSource
	Stage                control.FeishuTargetPickerStage
	StatusTitle          string
	StatusText           string
	StatusSections       []control.FeishuCardTextSection
	StatusFooter         string
	Messages             []control.FeishuTargetPickerMessage
	PendingKind          targetPickerPendingKind
	PendingWorkspaceKey  string
	PendingThreadID      string
	SelectedMode         control.FeishuTargetPickerMode
	SelectedSource       control.FeishuTargetPickerSourceKind
	SelectedWorkspaceKey string
	SelectedSessionValue string
	LocalDirectoryPath   string
	GitParentDir         string
	GitRepoURL           string
	GitDirectoryName     string
	GitFinalPath         string
	CreatedAt            time.Time
	ExpiresAt            time.Time
}

type activeThreadHistoryRecord struct {
	ThreadID string
	ViewMode control.FeishuThreadHistoryViewMode
	Page     int
	TurnID   string
}

type activePathPickerRecord struct {
	PickerID     string
	MessageID    string
	OwnerUserID  string
	OwnerFlowID  string
	Mode         pathPickerMode
	Title        string
	RootPath     string
	CurrentPath  string
	SelectedPath string
	Hint         string
	ConfirmLabel string
	CancelLabel  string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ConsumerKind string
	ConsumerMeta map[string]string
}

type surfaceUIRuntimeRecord struct {
	ActiveOwnerCardFlow *activeOwnerCardFlowRecord
	ActiveTargetPicker  *activeTargetPickerRecord
	ActiveThreadHistory *activeThreadHistoryRecord
	ActivePathPicker    *activePathPickerRecord
}

type SurfaceUIRuntimeSummary struct {
	ActiveOwnerCardFlowID    string
	ActiveOwnerCardFlowKind  string
	ActiveOwnerCardFlowPhase string
	ActiveOwnerCardRevision  int
	ActiveTargetPickerID     string
	ActiveThreadHistoryID    string
	ActivePathPickerID       string
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

func (s *Service) SurfaceUIRuntimeSummary(surfaceID string) SurfaceUIRuntimeSummary {
	runtime := s.surfaceUIRuntimeByID(surfaceID)
	if runtime == nil {
		return SurfaceUIRuntimeSummary{}
	}
	summary := SurfaceUIRuntimeSummary{}
	if runtime.ActiveOwnerCardFlow != nil {
		summary.ActiveOwnerCardFlowID = strings.TrimSpace(runtime.ActiveOwnerCardFlow.FlowID)
		summary.ActiveOwnerCardFlowKind = strings.TrimSpace(string(runtime.ActiveOwnerCardFlow.Kind))
		summary.ActiveOwnerCardFlowPhase = strings.TrimSpace(string(runtime.ActiveOwnerCardFlow.Phase))
		summary.ActiveOwnerCardRevision = runtime.ActiveOwnerCardFlow.Revision
	}
	if runtime.ActiveTargetPicker != nil {
		summary.ActiveTargetPickerID = strings.TrimSpace(runtime.ActiveTargetPicker.PickerID)
	}
	if runtime.ActiveThreadHistory != nil {
		summary.ActiveThreadHistoryID = strings.TrimSpace(summary.ActiveOwnerCardFlowID)
	}
	if runtime.ActivePathPicker != nil {
		summary.ActivePathPickerID = strings.TrimSpace(runtime.ActivePathPicker.PickerID)
	}
	return summary
}

func newOwnerCardFlowRecord(kind ownerCardFlowKind, flowID, ownerUserID string, createdAt time.Time, ttl time.Duration, phase ownerCardFlowPhase) *activeOwnerCardFlowRecord {
	flow := &activeOwnerCardFlowRecord{
		FlowID:      strings.TrimSpace(flowID),
		Kind:        kind,
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

func (s *Service) requireActiveOwnerCardFlow(surface *state.SurfaceConsoleRecord, kind ownerCardFlowKind, flowID, actorUserID, expiredText, unauthorizedText string) (*activeOwnerCardFlowRecord, []control.UIEvent) {
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
}
