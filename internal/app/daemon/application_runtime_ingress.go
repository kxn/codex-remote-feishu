package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func (r *applicationRuntime) VerifyPlatformReceipt(ctx context.Context, m ingressowner.PlatformMessage, proof []byte) error {
	switch m.Owner.Application {
	case ingressowner.Private:
		if r.privatePublications == nil {
			return ingressowner.ErrInvalid
		}
		return r.privatePublications.VerifyPlatformReceipt(ctx, m, proof)
	case ingressowner.Team:
		if r.teamPublications == nil {
			return ingressowner.ErrInvalid
		}
		return r.teamPublications.VerifyPlatformReceipt(ctx, m, proof)
	default:
		return ingressowner.ErrConflict
	}
}

// Construct before runtime Feishu configs create any gateway worker. Importing
// historical ownership here prevents the first new P2P message from inventing
// a parallel private owner while its old personal scope is still migrating.
func (a *App) configureApplicationIngress(ctx context.Context, controller *feishu.MultiGatewayController, lock *relayruntime.FileLock) error {
	r := a.applications
	if r == nil || r.native == nil || a.teamRuntime == nil || controller == nil || lock == nil || r.router != nil {
		return fmt.Errorf("application ingress requires configured runtimes and the daemon lock")
	}
	assets := filepath.Join(a.headlessRuntime.Paths.StateDir, "private-migration-assets")
	if err := os.MkdirAll(assets, 0700); err != nil {
		return err
	}
	limits := workstore.PersonalExportLimits{MaxRows: 100000, MaxBytes: 512 << 20}
	r.migrations = &personalMigrationCoordinator{app: a, journal: r.journal, processor: r.processor, assetRoot: assets, limits: limits}
	router, err := ingressowner.Open(filepath.Join(a.headlessRuntime.Paths.StateDir, "application-ingress.db"), r, r.migrations)
	if err != nil {
		return err
	}
	r.router = router
	r.migrations.router = router
	r.native.verifyMigrationResume = r.migrations.VerifyMigrationResume
	r.controller = controller
	router.SetOwnerScopeBindingVerifier(teamMigrationOwnerVerifier{store: a.teamRuntime.store})
	router.SetGatewayCoverageVerifier(applicationCoverageVerifier{controller: controller, previous: daemonLockPreviousVerifier{lock: lock}})
	router.SetMigrationVerifier(r.migrations)
	router.SetTargetReadyVerifier(r.migrations)
	history := teamHistoricalMigrationVerifier{app: a, router: router, assetRoot: assets, limits: limits}
	router.SetHistoricalSourceVerifier(history)
	router.SetHistoricalStagingVerifier(history)
	r.privatePublications = &privatePublicationBinding{router: router, journal: r.journal, bindingFor: func(ctx context.Context, o privatechat.Owner, generation int64) (ingressowner.PrivateBinding, error) {
		bot, e := (teamDirectory{app: a, store: a.teamRuntime.store}).ResolveGateway(ctx, o.GatewayID, o.AppID)
		if e != nil {
			return ingressowner.PrivateBinding{}, e
		}
		b, e := router.PrivateBindingForOperator(ctx, o.TenantID, o.AppID, o.GatewayID, bot.BotGeneration, o.ChatID, o.UserID)
		if e == nil && b.Owner.Epoch != generation {
			e = ingressowner.ErrConflict
		}
		return b, e
	}}
	r.teamPublications = &teamPublicationBinding{router: router, store: a.teamRuntime.store}
	r.publisher = &privatePublisher{host: a, processor: r.processor, journal: r.journal, ownership: r.privatePublications}
	r.ingress = &applicationIngress{host: a, router: router, resolver: applicationOwnerResolver{router: router}, team: teamApplicationAdmitter{host: a}, stageWake: make(chan struct{}, 1), stager: controller}
	r.runner = &privateWorkRunner{host: a, journal: r.journal, processor: r.processor, publisher: r.publisher, native: r.native, ingress: r.ingress, wake: make(chan struct{}, 1)}
	r.ingress.private = privateApplicationAdmitter{host: a, router: router, journal: r.journal, processor: r.processor, native: r.native, wake: r.runner.signal}
	hook, err := r.ingress.prepareStartupHook()
	if err != nil {
		return err
	}
	r.hook = &hook
	a.teamRuntime.publisher.SetConfirmedViewHook(func(ctx context.Context, id string) error {
		return r.teamPublications.confirm(ctx, teamPublicationProof{Kind: "team_view", ID: id})
	})
	a.teamRuntime.publisher.SetConfirmedMessageHook(func(ctx context.Context, id string) error {
		return r.teamPublications.confirm(ctx, teamPublicationProof{Kind: "team_message", ID: id})
	})
	if err = a.BindPrivateServiceOwnership(r.core); err != nil {
		return err
	}
	if paths, e := r.processor.CatalogSnapshot(); e == nil {
		a.SetPrivateCatalogSnapshot(paths)
	} else {
		return e
	}
	r.historical, err = r.migrations.PrepareHistoricalBindings(ctx)
	if err != nil {
		return fmt.Errorf("prepare historical private owner bindings: %w", err)
	}
	if err = PrepareHistoricalTeamOwners(ctx, router, a.teamRuntime.store); err != nil {
		return fmt.Errorf("prepare historical team owners: %w", err)
	}
	if err = r.teamPublications.ReconcileConfirmed(ctx); err != nil {
		return fmt.Errorf("reconcile confirmed team publications: %w", err)
	}
	return nil
}
