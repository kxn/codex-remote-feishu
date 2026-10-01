package codexstate

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

const (
	defaultCodexSessionsDirName   = "sessions"
	defaultTurnPatchStateDirName  = "codex-remote-feishu"
	defaultTurnPatchStateSubdir   = "current-thread-patches"
	turnPatchLedgerStatusPrepared = "prepared"
	turnPatchLedgerStatusApplied  = "applied"
	turnPatchLedgerStatusFailed   = "failed"
	turnPatchLedgerStatusRolled   = "rolled_back"
)

var (
	ErrTurnPatchThreadRequired        = errors.New("turn patch thread id is required")
	ErrTurnPatchRolloutNotFound       = errors.New("turn patch rollout not found")
	ErrTurnPatchLatestTurnNotFound    = errors.New("turn patch latest assistant turn not found")
	ErrTurnPatchRolloutDigestMismatch = errors.New("turn patch rollout digest mismatch")
	ErrTurnPatchRolloutPathDrift      = errors.New("turn patch rollout path drift")
	ErrTurnPatchDuplicateDrift        = errors.New("turn patch duplicate drift")
	ErrTurnPatchReplacementRequired   = errors.New("turn patch replacement text is required")
	ErrTurnPatchReplacementNotFound   = errors.New("turn patch replacement target not found")
	ErrTurnPatchPatchIDRequired       = errors.New("turn patch patch id is required")
	ErrTurnPatchPatchNotFound         = errors.New("turn patch patch id not found")
	ErrTurnPatchNotLatest             = errors.New("turn patch is not latest for thread")
	ErrTurnPatchRollbackDrift         = errors.New("turn patch rollback drift")
	ErrTurnPatchActorMismatch         = errors.New("turn patch actor mismatch")
)

type PatchThreadTarget struct {
	ThreadID    string
	RolloutPath string
}

type TurnPatchPreview struct {
	ThreadID           string
	RolloutPath        string
	RolloutDigest      string
	TurnID             string
	ReasoningLineCount int
	Messages           []TurnPatchPreviewMessage
}

type TurnPatchPreviewMessage struct {
	MessageKey string
	Phase      string
	Text       string
	IsFinal    bool
}

type TurnPatchReplacement struct {
	MessageKey string
	NewText    string
}

type ApplyLatestTurnPatchRequest struct {
	ThreadID              string
	ExpectedTurnID        string
	ExpectedRolloutDigest string
	Replacements          []TurnPatchReplacement
	ActorUserID           string
	SurfaceSessionID      string
}

type ApplyLatestTurnPatchResult struct {
	PatchID              string
	ThreadID             string
	TurnID               string
	RolloutPath          string
	BackupPath           string
	RolloutBeforeDigest  string
	RolloutAfterDigest   string
	ReplacedMessageCount int
	RemovedReasoningLine int
	AppliedAt            time.Time
}

type RollbackLatestTurnPatchRequest struct {
	ThreadID    string
	PatchID     string
	ActorUserID string
}

type RollbackLatestTurnPatchResult struct {
	PatchID             string
	ThreadID            string
	TurnID              string
	RolloutPath         string
	BackupPath          string
	RolloutBeforeDigest string
	RolloutAfterDigest  string
	RolledBackAt        time.Time
}

type TurnPatchMetadataReconciler interface {
	ReconcileLatestTurnPatch(ReconcileLatestTurnPatchRequest) error
}

type ReconcileLatestTurnPatchRequest struct {
	ThreadID    string
	TurnID      string
	RolloutPath string
}

type TurnPatchStorageOptions struct {
	SQLiteCatalog      *SQLiteThreadCatalog
	SessionsRoot       string
	PatchStateDir      string
	Logf               func(string, ...any)
	Now                func() time.Time
	MetadataReconciler TurnPatchMetadataReconciler
}

type TurnPatchStorage struct {
	sqliteCatalog      *SQLiteThreadCatalog
	sessionsRoot       string
	patchStateDir      string
	logf               func(string, ...any)
	now                func() time.Time
	metadataReconciler TurnPatchMetadataReconciler
}

func NewDefaultTurnPatchStorage(opts TurnPatchStorageOptions) (*TurnPatchStorage, error) {
	codexHome, err := defaultCodexHomeDir()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.SessionsRoot) == "" {
		opts.SessionsRoot = filepath.Join(codexHome, defaultCodexSessionsDirName)
	}
	if strings.TrimSpace(opts.PatchStateDir) == "" {
		opts.PatchStateDir = filepath.Join(codexHome, defaultTurnPatchStateDirName, defaultTurnPatchStateSubdir)
	}
	return NewTurnPatchStorage(opts)
}

func NewTurnPatchStorage(opts TurnPatchStorageOptions) (*TurnPatchStorage, error) {
	logf := opts.Logf
	if logf == nil {
		logf = log.Printf
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sessionsRoot := strings.TrimSpace(opts.SessionsRoot)
	patchStateDir := strings.TrimSpace(opts.PatchStateDir)
	if sessionsRoot == "" && opts.SQLiteCatalog == nil {
		return nil, fmt.Errorf("turn patch storage requires sessions root or sqlite catalog")
	}
	if patchStateDir == "" {
		return nil, fmt.Errorf("turn patch state dir is required")
	}
	reconciler := opts.MetadataReconciler
	if reconciler == nil {
		reconciler = noopTurnPatchMetadataReconciler{}
	}
	return &TurnPatchStorage{
		sqliteCatalog:      opts.SQLiteCatalog,
		sessionsRoot:       sessionsRoot,
		patchStateDir:      patchStateDir,
		logf:               logf,
		now:                now,
		metadataReconciler: reconciler,
	}, nil
}

func defaultCodexHomeDir() (string, error) {
	return config.ResolveCodexHomeDir(os.Environ())
}

type noopTurnPatchMetadataReconciler struct{}

func (noopTurnPatchMetadataReconciler) ReconcileLatestTurnPatch(ReconcileLatestTurnPatchRequest) error {
	return nil
}
