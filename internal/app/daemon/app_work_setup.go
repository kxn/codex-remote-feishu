package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhttp"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpractices"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workrequests"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workteamhome"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

// configureWorkRuntime runs before Run and gateway activation. Cutover chooses
// when raw ingress is attached; constructing the runtime does not create a
// second writer for a legacy chat or start any model process.
func (a *App) configureWorkRuntime(ctx context.Context, commands workingress.PrivateCommandHandler, authorizeControl func(context.Context, work.Actor, string, string) error) error {
	if a.teamRuntime != nil {
		return fmt.Errorf("team runtime is already configured")
	}
	if commands == nil || authorizeControl == nil {
		return fmt.Errorf("private commands and scoped control authorization are required")
	}
	paths := a.headlessRuntime.Paths
	store, err := workstore.Open(ctx, filepath.Join(paths.StateDir, "team-work.db"))
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			store.Close()
		}
	}()
	signer, err := workauth.LoadOrCreate(filepath.Join(paths.StateDir, "team-work.key"), nil)
	if err != nil {
		return err
	}
	if err = workpractices.Install(ctx, store); err != nil {
		return err
	}
	if err = a.syncWorkRoles(ctx, store); err != nil {
		return err
	}
	directory := teamDirectory{app: a, store: store}
	service, err := workservice.NewWithResourceConnectorResolver(store, signer, nil, teamResourceResolver{directory: directory})
	if err != nil {
		return err
	}
	service = service.WithManagementAuthorizer(teamManagementAuthorizer{app: a})
	service = service.WithBootstrapPlatform(workservice.BootstrapPlatform{
		ConfiguredApps:      a.teamBootstrapConfiguredApps,
		RegisterLocations:   a.registerTeamLocationCandidates,
		QueueLocationPicker: a.queueBootstrapLocationPicker,
		VerifyMembership:    a.verifyWorkMembership,
	})
	driver, err := worktransport.New(store, a.relay)
	if err != nil {
		return err
	}
	service = service.WithNativeDispatcher(driver)
	r := &teamRuntime{app: a, store: store, service: service, driver: driver, reconcileWake: make(chan struct{}, 1), pending: make(map[string]bool), signer: signer}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	hostID := teamID("host", hostname+":"+paths.StateDir)
	service.WithArtifactHost(hostID)
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return err
	}
	r.coordinator = workruntime.New(store, workruntime.Options{DefaultGoalTokenBudget: loaded.Config.Work.DefaultGoalTokenBudget, StateDir: paths.StateDir, HostID: hostID, Driver: driver, SendReceipt: a.relay.SendEventReceipt, EnqueueReconcile: r.enqueueReconcile, AuthorizeControl: authorizeControl, ApplyNativeRequest: workrequests.Apply})
	driver.BindRegistrar(r.coordinator)
	r.stops = workcontrol.NewStops(store, driver)
	_, port, err := net.SplitHostPort(a.apiServer.Addr)
	if err != nil {
		return err
	}
	settings := teamSettingsResolver{app: a, store: store, hostID: hostID}
	settingsAdmission := a.workSettingsAdmission(store)
	resolveCodexBinary := func(context.Context) (string, error) {
		current, e := a.loadAdminConfig()
		if e != nil {
			return "", e
		}
		binary, _ := resolvedCodexRealBinarySetting(current)
		return binary, nil
	}
	r.host, err = workhost.New(store, service, signer, workhost.Config{SettingsAdmission: settingsAdmission, MaxLiveCarriers: 8, MaxConcurrentLaunches: 4, ControlReservedCarriers: 1, Paths: paths, BinaryPath: a.headlessRuntime.BinaryPath, ConfigPath: a.headlessRuntime.ConfigPath, HostID: hostID, ToolEndpoint: "http://" + net.JoinHostPort("127.0.0.1", port), Start: a.startHeadless, Settings: settings, Registry: r.coordinator, Runtime: driver, StopCarrier: a.stopWorkCarrier, ResolveConfiguredCodexBinary: resolveCodexBinary})
	if err != nil {
		return err
	}
	codexBinary, _ := resolvedCodexRealBinarySetting(loaded)
	r.codexUpgradeHostID = hostID
	r.codexUpgrader, err = workupgrade.New(store, workupgrade.Config{HostID: hostID, StateDir: paths.StateDir, ConfiguredBinary: codexBinary, Drain: a.shutdownWorkCarrier, Restart: r.host.PrepareAfterUpgrade, ResolveConfiguredBinary: resolveCodexBinary})
	if err != nil {
		return err
	}
	r.engine, err = workengine.New(store, workengine.Config{SettingsAdmission: settingsAdmission, MaxConcurrent: 8, ControlReservedSlots: 1, MaxInputsPerAttempt: 16, Provisioner: r.host}, r.host, driver)
	if err != nil {
		return err
	}
	r.ingress, err = workingress.New(store, workingress.Config{Directory: directory, Expander: teamMessageExpander{directory: directory}, PrivateCommands: commands, QueuePrivateExecution: workdisplay.QueueIfBusy})
	if err != nil {
		return err
	}
	r.publisher, err = workpublish.New(store, teamPublisherResolver{directory: directory}, nil)
	if err != nil {
		return err
	}
	r.provision, err = workprovision.New(store, workprovision.Options{Resolver: workprovision.GatewayResolver(directory.gateway), Execution: teamExecutionPreparation{store: store, settings: settings}})
	if err != nil {
		return err
	}
	r.requests, err = workrequests.New(store, driver, a.authorizeNativeRequest)
	if err != nil {
		return err
	}
	r.handler = workhttp.NewHandler(service)
	a.teamRuntime = r
	r.teamMaterials = workteamhome.New(store, a.teamMaterialsResolver)
	ok = true
	return nil
}

func (a *App) handleTeamExecute(w http.ResponseWriter, r *http.Request) {
	if a.teamRuntime == nil || a.teamRuntime.handler == nil || !a.teamRuntime.active.Load() {
		http.Error(w, "Team runtime is unavailable", http.StatusServiceUnavailable)
		return
	}
	a.teamRuntime.handler.ServeHTTP(w, r)
}

func (a *App) registerTeamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+workcmd.ExecutePath, a.handleTeamExecute)
}
