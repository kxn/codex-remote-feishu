// Package privatechat owns the private conversation orchestration state.
package privatechat

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

var ErrInvalidOwner = errors.New("private chat owner does not match")
var ErrUnsupportedAction = errors.New("action is unavailable in private chat")

// Owner is authenticated by the ingress router; it is never inferred from a card.
type Owner struct{ GatewayID, SurfaceID, ChatID, UserID, TenantID, AppID string }

// App owns one private-only service for all its surfaces. The host serializes
// every call, including calls through Service, using its existing service lock.
// Durable admission and exclusive native carrier registration belong to the host.
type App struct {
	processorMu      sync.Mutex
	processorOwned   bool
	service          *orchestrator.Service
	owners           map[string]Owner
	ownerGenerations map[string]int64
	instances        map[string]string // native instance ID -> surface ID
}

func New(now func() time.Time, config orchestrator.Config) *App {
	if now == nil {
		now = time.Now
	}
	config.PrivateApplication = true
	return &App{service: orchestrator.NewService(now, config, renderer.NewPlanner()), owners: make(map[string]Owner), ownerGenerations: make(map[string]int64), instances: make(map[string]string)}
}

// OwnerGeneration is set only by the serial Processor after validating the
// durable owner epoch. Call only while that Processor owns its lock.
func (a *App) OwnerGeneration(surface string) (Owner, int64, bool) {
	o, ok := a.owners[surface]
	g := a.ownerGenerations[surface]
	return o, g, ok && g > 0
}

// Service is the single service owned by the app; the host uses it for existing
// effect completion, restoration and projection callbacks under its service lock.
func (a *App) Service() *orchestrator.Service { return a.service }

// OwnerForSurface is read only while the owning processor lock is held.
func (a *App) OwnerForSurface(surface string) (Owner, bool) {
	owner, ok := a.owners[surface]
	return owner, ok
}

// RegisterOwner is only called after the trusted router has established a private
// chat owner. Registration never imports the retired global service's state.
func (a *App) RegisterOwner(owner Owner) error {
	owner = Owner{strings.TrimSpace(owner.GatewayID), strings.TrimSpace(owner.SurfaceID), strings.TrimSpace(owner.ChatID), strings.TrimSpace(owner.UserID), strings.TrimSpace(owner.TenantID), strings.TrimSpace(owner.AppID)}
	if owner.GatewayID == "" || owner.SurfaceID == "" || owner.ChatID == "" || owner.UserID == "" || owner.TenantID == "" || owner.AppID == "" {
		return ErrInvalidOwner
	}
	if old, ok := a.owners[owner.SurfaceID]; ok && old != owner {
		return ErrInvalidOwner
	}
	a.owners[owner.SurfaceID] = owner
	a.service.MaterializeSurface(owner.SurfaceID, owner.GatewayID, owner.ChatID, owner.UserID)
	return nil
}

// RegisterInstance follows the host's durable, exclusive carrier binding.
func (a *App) RegisterInstance(surfaceID string, instance *state.InstanceRecord) error {
	if _, ok := a.owners[surfaceID]; !ok || instance == nil || strings.TrimSpace(instance.InstanceID) == "" {
		return ErrInvalidOwner
	}
	if old, ok := a.instances[instance.InstanceID]; ok && old != surfaceID {
		return ErrInvalidOwner
	}
	a.instances[instance.InstanceID] = surfaceID
	a.service.UpsertInstance(instance)
	return nil
}

// ValidateAction is required before any private action enters the old service.
func (a *App) ValidateAction(action control.Action) error {
	owner, ok := a.owners[action.SurfaceSessionID]
	if !ok || action.GatewayID != owner.GatewayID || action.ChatID != owner.ChatID || action.ActorUserID != owner.UserID {
		return ErrInvalidOwner
	}
	switch action.Kind {
	case control.ActionCronCommand, control.ActionVSCodeMigrateCommand, control.ActionVSCodeMigrate, control.ActionFollowLocal, control.ActionFeishuBotAddedToGroup, control.ActionPrimaryCommand, control.ActionCoworkersCommand:
		return ErrUnsupportedAction
	}
	if action.Kind == control.ActionModeCommand {
		fields := strings.Fields(strings.ToLower(action.Text))
		if len(fields) > 1 && fields[1] == "vscode" || strings.EqualFold(strings.TrimSpace(action.OptionID), "vscode") {
			return ErrUnsupportedAction
		}
	}
	if action.InstanceID != "" && a.instances[action.InstanceID] != owner.SurfaceID {
		return ErrInvalidOwner
	}
	return nil
}

// ValidateObservation checks actor-free SDK facts without inventing a user.
// The processor separately proves the target message belongs to this owner.
func (a *App) ValidateObservation(action control.Action) error {
	owner, ok := a.owners[action.SurfaceSessionID]
	if !ok || action.GatewayID != owner.GatewayID || (action.ChatID != "" && action.ChatID != owner.ChatID) || action.TargetMessageID == "" {
		return ErrInvalidOwner
	}
	switch action.Kind {
	case control.ActionMessageRecalled:
		if action.ActorUserID != "" && action.ActorUserID != owner.UserID {
			return ErrInvalidOwner
		}
	case control.ActionReactionCreated:
		if action.ActorUserID != owner.UserID {
			return ErrInvalidOwner
		}
	default:
		return ErrUnsupportedAction
	}
	return nil
}

// Observe is reserved for the processor after source ownership verification.
func (a *App) Observe(action control.Action) ([]eventcontract.Event, error) {
	if err := a.ValidateObservation(action); err != nil {
		return nil, err
	}
	return a.service.ApplySurfaceAction(action), nil
}

// Action and AgentEvent return ordered effects; the host must execute and
// acknowledge them, not silently drop them on failure.
func (a *App) Action(action control.Action) ([]eventcontract.Event, error) {
	if err := a.ValidateAction(action); err != nil {
		return nil, err
	}
	return a.service.ApplySurfaceAction(action), nil
}

func (a *App) AgentEvent(instanceID string, event agentproto.Event) ([]eventcontract.Event, error) {
	if _, ok := a.instances[instanceID]; !ok {
		return nil, ErrInvalidOwner
	}
	return a.service.ApplyAgentEvent(instanceID, event), nil
}
