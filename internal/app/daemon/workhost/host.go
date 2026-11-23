// Package workhost prepares native carriers from durable work ownership. It
// neither creates Git worktrees nor borrows a chat surface as a session owner.
package workhost

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"path/filepath"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type LaunchSettings struct {
	ConnectionIdentity  string
	NativeBinaryPath    string
	NativeBinaryVersion string
	ReadOnly            bool
	PreferencesRevision int64
	Backend             work.Backend
	Location            work.ExecutionLocation
	ProfileID           string
	Environment         []string // ephemeral secrets; deliberately absent from LaunchRecord
	LaunchArgs          []string // host process configuration; never model-visible content
	Overrides           agentproto.PromptOverrides
	CodexResume         *agentproto.CodexResumePolicy
}
type SettingsResolver interface {
	ResolveLaunch(context.Context, work.WorkScope, work.Role, work.WorkContext) (LaunchSettings, error)
}
type CarrierRegistry interface {
	RegisterCarrier(context.Context, workstore.CarrierRegistration) error
}
type Config struct {
	SettingsAdmission            workengine.SettingsAdmission
	ResolveConfiguredCodexBinary func(context.Context) (string, error)
	ControlReservedCarriers      int
	MaxLiveCarriers              int // includes prepared and uncertain launches until exact exit
	MaxConcurrentLaunches        int // bounds concurrent preparation; engine bounds admitted carriers

	Runtime      workruntime.Driver
	StopCarrier  func(context.Context, work.WorkerSession) error
	Paths        relayruntime.Paths
	BinaryPath   string
	ConfigPath   string
	HostID       string
	ToolEndpoint string
	Start        func(relayruntime.HeadlessLaunchOptions) (int, error)
	Settings     SettingsResolver
	Registry     CarrierRegistry
	Now          func() time.Time
}
type Host struct {
	preparations preparationLocks
	launchSlots  chan struct{}
	store        *workstore.Store
	service      *workservice.Service
	signer       *workauth.Signer
	config       Config
}

type LaunchRecord struct {
	ConnectionIdentity     string                           `json:"connection_identity,omitempty"`
	PreferencesRevision    int64                            `json:"preferences_revision,omitempty"`
	WorkerID               string                           `json:"worker_id"`
	ScopeID                string                           `json:"scope_id"`
	Generation             int64                            `json:"generation"`
	RoleDescriptionVersion int64                            `json:"role_description_version"`
	SettingsRevision       int64                            `json:"settings_revision"`
	ProfileID              string                           `json:"profile_id"`
	Backend                work.Backend                     `json:"backend"`
	Snapshot               agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
	Overrides              agentproto.PromptOverrides       `json:"overrides"`
	CodexResume            *agentproto.CodexResumePolicy    `json:"codex_resume,omitempty"`
}

func New(store *workstore.Store, service *workservice.Service, signer *workauth.Signer, config Config) (*Host, error) {
	if store == nil || service == nil || signer == nil || config.Start == nil || config.Settings == nil || config.Registry == nil || config.HostID == "" || config.BinaryPath == "" || config.ToolEndpoint == "" || !filepath.IsAbs(config.Paths.StateDir) {
		return nil, fmt.Errorf("work host requires durable store, explicit local paths, launch settings, carrier registry and tool endpoint")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MaxLiveCarriers <= 0 {
		config.MaxLiveCarriers = 8
	}
	if config.ControlReservedCarriers <= 0 {
		config.ControlReservedCarriers = 1
	}
	if config.ControlReservedCarriers >= config.MaxLiveCarriers {
		return nil, fmt.Errorf("native carrier pool requires capacity beyond management reserve")
	}
	if config.MaxConcurrentLaunches <= 0 {
		config.MaxConcurrentLaunches = 4
	}
	return &Host{store: store, service: service, signer: signer, config: config, launchSlots: make(chan struct{}, config.MaxConcurrentLaunches)}, nil
}
func idFor(prefix string, parts ...string) string {
	data, _ := json.Marshal(parts)
	hash := sha256.Sum256(data)
	return prefix + "_" + hex.EncodeToString(hash[:20])
}
func opaqueID(prefix string) (string, error) {
	data := make([]byte, 20)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(data), nil
}
func launchID(worker work.WorkerSession) string {
	return idFor("launch", worker.ID, fmt.Sprint(worker.Generation))
}

func (h *Host) readLaunch(tx *workstore.Tx, worker work.WorkerSession) (LaunchRecord, error) {
	var record LaunchRecord
	op, err := tx.GetOperation(launchID(worker))
	if err != nil {
		return record, err
	}
	if op.Kind != "worker.launch" || op.TargetID != worker.ID || op.TargetGeneration != worker.Generation {
		return record, fmt.Errorf("worker launch ownership mismatch")
	}
	body, err := tx.GetContent(worker.ScopeID, op.RequestRef)
	if err != nil {
		return record, err
	}
	if err = json.Unmarshal(body.Data, &record); err != nil {
		return record, err
	}
	owner := record.Snapshot.Owner
	if record.WorkerID != worker.ID || record.ScopeID != worker.ScopeID || record.Generation != worker.Generation || owner.WorkerSessionID != worker.ID || owner.ControllerGeneration != worker.ControllerGeneration || record.Snapshot.CarrierIncarnation != worker.CarrierIncarnation {
		return record, fmt.Errorf("stale worker launch snapshot")
	}
	return record, nil
}

func (h *Host) LocalHostID() string { return h.config.HostID }
