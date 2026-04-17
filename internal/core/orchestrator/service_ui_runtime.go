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

type activeTargetPickerRecord struct {
	PickerID             string
	OwnerUserID          string
	Source               control.TargetPickerRequestSource
	SelectedMode         control.FeishuTargetPickerMode
	SelectedSource       control.FeishuTargetPickerSourceKind
	SelectedWorkspaceKey string
	SelectedSessionValue string
	LocalDirectoryPath   string
	GitParentDir         string
	GitRepoURL           string
	GitDirectoryName     string
	CreatedAt            time.Time
	ExpiresAt            time.Time
}

type activeThreadHistoryRecord struct {
	PickerID    string
	OwnerUserID string
	ThreadID    string
	MessageID   string
	ViewMode    control.FeishuThreadHistoryViewMode
	Page        int
	TurnID      string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type activePathPickerRecord struct {
	PickerID     string
	OwnerUserID  string
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
	ActiveTargetPicker  *activeTargetPickerRecord
	ActiveThreadHistory *activeThreadHistoryRecord
	ActivePathPicker    *activePathPickerRecord
}

type SurfaceUIRuntimeSummary struct {
	ActiveTargetPickerID  string
	ActiveThreadHistoryID string
	ActivePathPickerID    string
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
	if runtime.ActiveTargetPicker != nil {
		summary.ActiveTargetPickerID = strings.TrimSpace(runtime.ActiveTargetPicker.PickerID)
	}
	if runtime.ActiveThreadHistory != nil {
		summary.ActiveThreadHistoryID = strings.TrimSpace(runtime.ActiveThreadHistory.PickerID)
	}
	if runtime.ActivePathPicker != nil {
		summary.ActivePathPickerID = strings.TrimSpace(runtime.ActivePathPicker.PickerID)
	}
	return summary
}
