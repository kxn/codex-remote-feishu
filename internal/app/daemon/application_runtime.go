package daemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// applicationRuntime is composition and lifecycle only. Conversation state is
// owned by privatechat; project work is owned by teamRuntime. Shared transports
// do not choose a business application by trying one and falling back to another.
type applicationRuntime struct {
	teamPublications    *teamPublicationBinding
	privatePublications *privatePublicationBinding
	migrations          *personalMigrationCoordinator
	historical          []ingressowner.OwnerScopeBinding
	core                *privatechat.App
	journal             *privatechat.Journal
	processor           *privatechat.Processor
	native              *privateNativeHost
	registry            *executionowner.Registry
	outbound            *executionowner.Outbound
	teamOwners          *teamExecutionOwnerAdapter
	router              *ingressowner.Router
	ingress             *applicationIngress
	publisher           *privatePublisher
	runner              *privateWorkRunner
	hook                *feishu.RawIngressHook
	controller          *feishu.MultiGatewayController
	lifecycleMu         sync.Mutex
	cancel              context.CancelFunc
	done                chan struct{}
	closeOnce           sync.Once
	closeErr            error
	coverageMu          sync.Mutex
	coveredGateways     map[string]string
	deliveries          applicationDeliveryGate
	platformDeliveries  applicationDeliveryGate
	sharedWorkersJoined func() bool
}

func daemonOrchestratorConfig() orchestrator.Config {
	return orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond, GitAvailable: gitExecutableAvailable()}
}

// Called immediately after New, before configuration/catalog materialization.
// The legacy helper methods share this service only as adapters under Processor;
// no second private service is retained or restored in the same daemon.
func (a *App) prepareApplicationCore() error {
	if a.applications != nil || a.teamRuntime != nil || len(a.service.Surfaces()) != 0 || len(a.service.Instances()) != 0 {
		return fmt.Errorf("application core must be selected before runtime configuration")
	}
	core := privatechat.New(time.Now, daemonOrchestratorConfig())
	core.Service().EnablePrivateThreadPolicy()
	a.applications = &applicationRuntime{core: core, sharedWorkersJoined: a.applicationSharedWorkersJoined}
	a.service = core.Service()
	a.apiServer.Handler = a.applicationRequestGuard(a.apiServer.Handler)
	return nil
}

// Constructed before either runtime starts. The two stop verifiers share one
// registry but are selected by its recorded application, never by errors.
func (r *applicationRuntime) VerifyStop(ctx context.Context, reg executionowner.Registration, evidence string) error {
	switch reg.Owner.Application {
	case executionowner.Private:
		if r.native == nil {
			return executionowner.ErrConflict
		}
		return r.native.VerifyStop(ctx, reg, evidence)
	case executionowner.Team:
		if r.teamOwners == nil {
			return executionowner.ErrConflict
		}
		return r.teamOwners.VerifyStop(ctx, reg, evidence)
	default:
		return executionowner.ErrConflict
	}
}

func (a *App) configureApplicationNative(store *workstore.Store, coordinator *workruntime.Coordinator, hostID string) error {
	r := a.applications
	if r == nil || r.native != nil || r.core == nil || a.service != r.core.Service() {
		return fmt.Errorf("application core unavailable or already configured")
	}
	root := a.headlessRuntime.Paths.StateDir
	var err error
	ok := false
	defer func() {
		if !ok {
			_ = r.close()
		}
	}()
	r.journal, err = privatechat.OpenJournal(filepath.Join(root, "private-chat.db"))
	if err != nil {
		return err
	}
	r.journal.SetReferenceGate(&a.profileReferenceGate)
	r.processor, err = privatechat.NewProcessor(r.core, r.journal, privateActionDecoder{}, privateHostReducer{a})
	if err != nil {
		return err
	}
	hostStore, err := openPrivateNativeHostStore(filepath.Join(root, "private-carriers.db"))
	if err != nil {
		return err
	}
	r.native = &privateNativeHost{store: hostStore, journal: r.journal, processor: r.processor, relay: a.relay, hostID: hostID, stateDir: root, startHeadless: a.startHeadless, connections: map[string]uint64{}, sealedReady: map[string]bool{}}
	r.native.teamHistory = store
	r.teamOwners = &teamExecutionOwnerAdapter{store: store, coordinator: coordinator, stateDir: root, hostID: hostID}
	r.registry, err = executionowner.Open(filepath.Join(root, "execution-owners.db"), r)
	if err != nil {
		return err
	}
	r.outbound, err = executionowner.NewOutbound(r.registry)
	if err != nil {
		return err
	}
	r.native.registry, r.native.outbound = r.registry, r.outbound
	r.teamOwners.registry = r.registry
	r.teamOwners.outbound = r.outbound
	a.privateTools = &privateToolRuntime{host: r.native}
	ok = true
	return nil
}

// Call only after application workers and ingress have drained.
func (r *applicationRuntime) close() error {
	if r == nil {
		return nil
	}
	if r.sharedWorkersJoined != nil && !r.sharedWorkersJoined() {
		return fmt.Errorf("team workers still use shared application stores")
	}
	r.lifecycleMu.Lock()
	done := r.done
	r.lifecycleMu.Unlock()
	if done != nil {
		select {
		case <-done:
		default:
			return fmt.Errorf("application workers have not joined; stores remain open")
		}
	}
	if !r.deliveries.closeReady() {
		return fmt.Errorf("application requests have not drained; stores remain open")
	}
	r.closeOnce.Do(func() {
		var problems []error
		if r.router != nil {
			problems = append(problems, r.router.Close())
		}
		if r.registry != nil {
			problems = append(problems, r.registry.Close())
		}
		if r.native != nil && r.native.store != nil {
			problems = append(problems, r.native.store.Close())
		}
		if r.journal != nil {
			problems = append(problems, r.journal.Close())
		}
		r.closeErr = errors.Join(problems...)
	})
	return r.closeErr
}
