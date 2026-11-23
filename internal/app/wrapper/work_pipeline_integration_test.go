package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhttp"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type pipelineLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (p *pipelineLog) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.b.Write(b)
}
func (p *pipelineLog) String() string { p.mu.Lock(); defer p.mu.Unlock(); return p.b.String() }

type pipelineDirectory struct{}

func (pipelineDirectory) ResolveGateway(_ context.Context, gateway, app string) (work.IngressGateway, error) {
	if gateway == "pipeline-gateway-expert" {
		return work.IngressGateway{GatewayRef: gateway, AppID: app, RoleID: "pipeline-expert", BotGeneration: 1}, nil
	}
	return work.IngressGateway{GatewayRef: "pipeline-gateway", AppID: "pipeline-app", RoleID: "pipeline-role", BotGeneration: 1}, nil
}
func (pipelineDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return "pipeline-role", nil
}
func (pipelineDirectory) ResolveMention(_ context.Context, _ work.IngressGateway, mention work.RawMention) (string, error) {
	if mention.IDs.OpenID == "pipeline-leader-open" {
		return "pipeline-role", nil
	}
	return "", fmt.Errorf("unexpected mention")
}

type pipelineSettings struct {
	backend   work.Backend
	location  work.ExecutionLocation
	locations map[string]work.ExecutionLocation
}

func (s pipelineSettings) ResolveLaunch(_ context.Context, _ work.WorkScope, role work.Role, _ work.WorkContext) (workhost.LaunchSettings, error) {
	location := s.location
	if selected, ok := s.locations[role.ID]; ok {
		location = selected
	}
	return workhost.LaunchSettings{Backend: s.backend, Location: location, ProfileID: "controlled-profile", CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumeApplyTargetProfile, ModelProviderID: "openai"}}, nil
}

type pipelinePublisher struct {
	mu       sync.Mutex
	requests []feishu.ProjectPublishRequest
}

func (p *pipelinePublisher) Resolve(context.Context, work.RoleBotBinding) (workpublish.Sender, error) {
	return p, nil
}
func (p *pipelinePublisher) PublishText(_ context.Context, r feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, r)
	return feishu.ProjectPublishReceipt{MessageID: fmt.Sprintf("pipeline-source-receipt-%d", len(p.requests)), ChatID: r.ChatID, RootID: r.ReplyToMessageID}, nil
}

func TestSDKEngineHostDriverWrapperNativePipePipeline(t *testing.T) {
	binary := buildPipelineTeamCLI(t)
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) { runSDKNativePipeline(t, binary, backend) })
	}
}

func buildPipelineTeamCLI(t *testing.T) string {
	t.Helper()
	name := "installed team client"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	if output, err := execlaunch.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", binary, "../../../cmd/codex-remote").CombinedOutput(); err != nil {
		t.Fatalf("build actual CLI executable: %v\n%s", err, output)
	}
	return binary
}

func runSDKNativePipeline(t *testing.T, binary string, backend work.Backend, groupCapacity ...int) {
	group := len(groupCapacity) > 0
	t.Setenv("FSC_NATIVE_PIPELINE_AUTO_PRIVATE", strconv.FormatBool(!group))
	collaboration := os.Getenv("FSC_NATIVE_PIPELINE_COLLABORATION") != ""
	dependency := os.Getenv("FSC_NATIVE_PIPELINE_DEPENDENCY") != ""
	capacity, reserve := 2, 1
	if group {
		capacity = groupCapacity[0]
		reserve = 0
		t.Setenv("FSC_NATIVE_PIPELINE_GROUP", strconv.Itoa(capacity))
	}
	t.Helper()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	t.Setenv("FSC_NATIVE_PIPELINE_CHILD", string(backend))
	t.Setenv(config.ClaudeBinaryEnv, os.Args[0])
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root, cwd := t.TempDir(), t.TempDir()
	store, err := workstore.Open(ctx, filepath.Join(root, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx := func(fn func(*workstore.Tx) error) {
		t.Helper()
		if err := store.Within(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	tx(func(tx *workstore.Tx) error {
		if err := tx.PutRole(work.Role{ID: "pipeline-role", Name: "Pipeline", Description: "Handle this human's explicit work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "pipeline-role", GatewayRef: "pipeline-gateway", AppID: "pipeline-app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if group {
			return pipelineSeedGroup(tx, collaboration)
		}
		return nil
	})
	var ingress *workingress.Ingress
	ingressConfig := workingress.Config{Directory: pipelineDirectory{}}
	var steerCommands pipelineSteerCommandHandler
	if os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") == "true" {
		ingressConfig.PrivateCommands = &steerCommands
	}
	var sendMediaSDK func(work.RawIngressEnvelope)
	if os.Getenv("FSC_NATIVE_PIPELINE_IMAGES") == "true" || os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") == "true" {
		ingressConfig.Expander, sendMediaSDK = pipelineMediaSDK(t, ctx, func(c context.Context, r work.RawIngressEnvelope) error { _, e := ingress.Receive(c, r); return e })
	}
	ingress, err = workingress.New(store, ingressConfig)
	if err != nil {
		t.Fatal(err)
	}
	raw := work.RawIngressEnvelope{GatewayRef: "pipeline-gateway", AppID: "pipeline-app", EventID: "sdk-event", Tenant: "pipeline-tenant", ChatID: "pipeline-private-chat", ChatType: "p2p", MessageID: "sdk-human-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "pipeline-human"}, MessageType: "text", RawContent: `{"text":"Complete my authorized work and reply here."}`, ReceivedAt: time.Now().UTC()}
	if group {
		raw.ChatID = "pipeline-group-chat"
		raw.ChatType = "group"
	}
	if collaboration {
		raw.RawContent = `{"text":"Add CSV and Excel export. Ask the reviewer to check the result."}`
	}
	if sendMediaSDK != nil && os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") == "true" {
		sendMediaSDK(raw)
	} else if sendMediaSDK != nil {
		mode := os.Getenv("FSC_NATIVE_PIPELINE_MEDIA_POST")
		if mode == "captioned-post" || mode == "image-only-post" {
			post := raw
			post.MessageType = "post"
			post.EventID = "sdk-post"
			post.MessageID = "sdk-post"
			caption := ""
			if mode == "captioned-post" {
				caption = `{"tag":"text","text":"Complete my authorized work and reply here."},`
			}
			post.RawContent = `{"zh_cn":{"title":"","content":[[` + caption + `{"tag":"img","image_key":"image-one"},{"tag":"img","image_key":"image-two"}]]}}`
			sendMediaSDK(post)
			if mode == "image-only-post" {
				sendMediaSDK(raw)
			}
		} else {
			for i, name := range []string{"image-one", "image-two"} {
				picture := raw
				picture.EventID = name
				picture.MessageID = name
				picture.MessageType = "image"
				picture.RawContent = fmt.Sprintf(`{"image_key":%q}`, name)
				picture.ReceivedAt = raw.ReceivedAt.Add(time.Duration(i) * time.Millisecond)
				sendMediaSDK(picture)
				if i == 0 {
					redelivery := picture
					redelivery.EventID += "-redelivery"
					sendMediaSDK(redelivery)
				}
			}
			sendMediaSDK(raw)
		}
	} else if _, err = ingress.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	processed, err := ingress.ProcessPending(ctx, 16)
	if err != nil || len(processed.Failures) != 0 {
		t.Fatalf("SDK processing: %+v %v", processed, err)
	}
	var scope work.WorkScope
	var original work.Inbox
	tx(func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		for _, candidate := range scopes {
			if (!group && candidate.Kind == work.ScopePersonal) || (group && candidate.Kind == work.ScopeProject) {
				scope = candidate
			}
		}
		inputs, e := tx.ListPendingInputs(scope.ID, "pipeline-role")
		if e != nil {
			return e
		}
		wantInputs := 1
		if sendMediaSDK != nil && os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") != "true" {
			wantInputs = 3
			if os.Getenv("FSC_NATIVE_PIPELINE_MEDIA_POST") == "captioned-post" {
				wantInputs = 1
			}
			if os.Getenv("FSC_NATIVE_PIPELINE_MEDIA_POST") == "image-only-post" {
				wantInputs = 2
			}
		}
		if len(inputs) != wantInputs {
			return fmt.Errorf("SDK produced %d inputs, need %d", len(inputs), wantInputs)
		}
		original = inputs[0]
		for _, input := range inputs {
			if !input.AwaitingText {
				original = input
			}
		}
		return nil
	})
	principalID := scope.PrincipalID
	if group {
		principalID, err = pipelineGroupSourcePrincipal(ctx, store, original)
		if err != nil {
			t.Fatal(err)
		}
	}
	location := work.ExecutionLocation{ID: "pipeline-location", HostID: "pipeline-host", Cwd: cwd, Generation: 1, Revision: 1}
	locations := map[string]work.ExecutionLocation{"pipeline-role": location}
	selectedLocations := []string{location.ID}
	if group {
		expert := work.ExecutionLocation{ID: "pipeline-expert-location", HostID: "pipeline-host", Cwd: t.TempDir(), Generation: 1, Revision: 1}
		locations["pipeline-expert"] = expert
		selectedLocations = append(selectedLocations, expert.ID)
	}
	if collaboration {
		reviewer := work.ExecutionLocation{ID: "pipeline-reviewer-location", HostID: "pipeline-host", Cwd: t.TempDir(), Generation: 1, Revision: 1}
		locations["pipeline-reviewer"] = reviewer
		selectedLocations = append(selectedLocations, reviewer.ID)
	}
	tx(func(tx *workstore.Tx) error {
		for role, extra := range locations {
			if role != "pipeline-role" {
				if e := tx.PutLocation(extra, 0); e != nil {
					return e
				}
			}
		}
		if e := tx.PutLocation(location, 0); e != nil {
			return e
		}
		body := work.NewContent("pipeline-human-directory-choice", scope.ID, "application/json", []byte(`{"source":"actual human fixture selection"}`), time.Now())
		if e := tx.PutContent(body); e != nil {
			return e
		}
		return tx.PutExecutionPreference(workstore.ExecutionPreference{ScopeID: scope.ID, SelectedLocationIDs: selectedLocations, DefaultLocationID: location.ID, SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: principalID}, SourceRef: body.ID, Revision: 1}, 0)
	})
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	overlap := &pipelineOverlapEvidence{}
	mux := http.NewServeMux()
	mux.Handle("/", workhttp.NewHandler(service))
	mux.Handle("/pipeline/overlap", overlap.handler(store, scope.ID))
	cliServer := httptest.NewServer(mux)
	defer cliServer.Close()
	errorsCh := make(chan error, 64)
	report := func(err error) {
		if err != nil {
			select {
			case errorsCh <- err:
			default:
			}
		}
	}
	reconcile := make(chan string, 64)
	var coordinator *workruntime.Coordinator
	var transportDriver *worktransport.Driver
	var relay *relayws.Server
	relay = relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(ctx context.Context, _ relayws.ConnectionMeta, h agentproto.Hello) {
			report(coordinator.HandleHello(ctx, h))
		},
		OnEvents: func(ctx context.Context, _ relayws.ConnectionMeta, instance string, events []agentproto.Event) {
			for _, event := range events {
				if e := coordinator.HandleEvent(ctx, instance, event); e != nil {
					report(e)
					continue
				}
				if event.Kind == agentproto.EventRuntimeInspectionCompleted {
					report(transportDriver.DeliverInspection(ctx, event))
				}
			}
		},
		OnCommandAck: func(ctx context.Context, _ relayws.ConnectionMeta, instance string, ack agentproto.CommandAck) {
			report(coordinator.HandleAck(ctx, instance, ack))
		},
	})
	relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "pipeline-fixture", BuildFingerprint: "pipeline-fingerprint"}})
	relayServer := httptest.NewServer(relay)
	defer relayServer.Close()
	defer relay.Close()
	wsURL := "ws" + strings.TrimPrefix(relayServer.URL, "http")
	actualDriver, e := worktransport.New(store, relay)
	if e != nil {
		t.Fatal(e)
	}
	transportDriver = actualDriver
	coordinator = workruntime.New(store, workruntime.Options{StateDir: root, HostID: "pipeline-host", Driver: actualDriver, SendReceipt: relay.SendEventReceipt, EnqueueReconcile: func(id string) {
		select {
		case reconcile <- id:
		default:
		}
	}})
	actualDriver.BindRegistrar(coordinator)
	wrapperDone := make(chan error, 4)
	var wrapperWG sync.WaitGroup
	var startMu sync.Mutex
	childLog := &pipelineLog{}
	defer func() {
		if t.Failed() {
			t.Logf("controlled native stderr: %s", childLog.String())
		}
	}()
	start := func(options relayruntime.HeadlessLaunchOptions) (int, error) {
		startMu.Lock()
		defer startMu.Unlock()
		var snapshot agentproto.RuntimeLaunchSnapshot
		for _, entry := range options.Env {
			key, value, _ := strings.Cut(entry, "=")
			if key == config.WorkLaunchSnapshotEnv {
				body, e := os.ReadFile(value)
				if e != nil {
					return 0, e
				}
				if e = json.Unmarshal(body, &snapshot); e != nil {
					return 0, e
				}
			}
			if key == config.WorkSessionTokenEnv || key == config.WorkTeamBinaryEnv {
				t.Setenv(key, value)
			}
		}
		app := New(Config{Backend: agentproto.Backend(backend), Managed: true, Source: "headless", Lifetime: string(lifetimeDaemonOwned), InstanceID: options.InstanceID, WorkspaceRoot: options.WorkDir, RuntimePaths: relayruntime.Paths{StateDir: root}, RelayServerURL: wsURL, CodexRealBinary: os.Args[0], Args: []string{"app-server"}, WorkLaunchSnapshot: &snapshot, Version: "pipeline-fixture", BuildFingerprint: "pipeline-fingerprint"})
		reader, writer := io.Pipe()
		go func() { <-ctx.Done(); _ = writer.Close() }()
		childCtx := context.WithValue(ctx, preparedCodexBinaryKey{}, os.Args[0])
		wrapperWG.Add(1)
		go func() {
			defer wrapperWG.Done()
			code, e := app.Run(childCtx, reader, io.Discard, childLog)
			if code != 0 {
				report(fmt.Errorf("wrapper exit code %d: %v stderr=%s", code, e, childLog.String()))
			}
			wrapperDone <- e
		}()
		return os.Getpid(), nil
	}
	host, err := workhost.New(store, service, signer, workhost.Config{Paths: relayruntime.Paths{StateDir: root}, HostID: "pipeline-host", BinaryPath: binary, ToolEndpoint: cliServer.URL, Settings: pipelineSettings{backend: backend, location: location, locations: locations}, Registry: coordinator, Start: start, Runtime: actualDriver})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := workengine.New(store, workengine.Config{MaxConcurrent: capacity, ControlReservedSlots: reserve, Provisioner: host}, host, actualDriver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		drainCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = engine.DrainPreparations(drainCtx)
		joined := make(chan struct{})
		go func() { wrapperWG.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-drainCtx.Done():
			t.Error("actual wrapper did not join")
		}
	}()
	var attempt work.ExecutionAttempt
	sender := &pipelinePublisher{}
	var platform *pipelinePrivatePlatform
	var resolver workpublish.Resolver = sender
	if !group {
		platform = newPipelinePrivatePlatform(t, raw.ChatID, raw.MessageID)
		resolver = platform
	}
	publisher, err := workpublish.New(store, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	journey := pipelineCollaborationJourney{store: store, scopeID: scope.ID, ingress: ingress, original: raw}
	steerSent := false
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		tick, e := engine.Tick(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, outcome := range tick.Dispatched {
			if outcome.Err != nil {
				t.Fatalf("actual engine dispatch: %v", outcome.Err)
			}
		}

		select {
		case e := <-errorsCh:
			t.Fatal(e)
		default:
		}
		select {
		case id := <-reconcile:
			report(coordinator.Reconcile(ctx, id))
		default:
		}
		if os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") == "true" && !steerSent {
			var e error
			steerSent, e = pipelineInjectClaudeSteer(t, ctx, store, service, actualDriver, ingress, sendMediaSDK, raw, scope, original, root, &steerCommands)
			if e != nil {
				t.Fatal(e)
			}
		}
		complete := false
		if collaboration {
			if _, e = publisher.RunOnce(ctx, 16); e != nil {
				t.Fatal(e)
			}
			complete, e = journey.advance(ctx)
			if e != nil {
				t.Fatal(e)
			}
		}
		tx(func(tx *workstore.Tx) error {
			attempts, e := tx.ListAttempts(scope.ID)
			if e != nil {
				return e
			}
			finished := 0
			for _, saved := range attempts {
				for _, id := range saved.InputIDs {
					if id == original.ID {
						attempt = saved
					}
				}
				if _, e := tx.GetQuiescenceReceipt(saved.ID); e == nil {
					finished++
				}
			}
			want := 1
			if group {
				want = 3
			}
			if dependency {
				want = 5
			}
			if !collaboration {
				complete = finished >= want
			}

			return nil
		})
		if complete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if attempt.ID == "" {
		t.Fatal("actual engine never admitted the SDK input")
	}
	if os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") == "true" {
		if !steerSent {
			t.Fatal("running turn never received queued images")
		}
		if os.Getenv("FSC_NATIVE_PIPELINE_PRIVATE_SHELL") != "" {
			verifyPipelinePrivateShellSettled(t, store, scope, original)
		} else {
			verifyPipelineClaudeSteerSettled(t, store, scope)
		}
	}
	tx(func(tx *workstore.Tx) error {
		saved, e := tx.GetAttempt(attempt.ID)
		if e != nil {
			return e
		}
		if saved.State != work.AttemptCompleted {
			return fmt.Errorf("actual pipeline ended in %s", saved.State)
		}
		if _, e = tx.GetQuiescenceReceipt(attempt.ID); e != nil {
			return e
		}
		input, e := tx.GetInbox(original.ID)
		if e != nil {
			return e
		}
		if input.State != work.InboxProcessed {
			return fmt.Errorf("SDK input not processed after native settlement")
		}
		if !group {
			report, e := tx.GetProcessingReport(saved.ID)
			if e != nil {
				return e
			}
			if report.WorkDisposition != "unknown" || report.OutcomeRef == "" {
				return fmt.Errorf("ordinary native answer lacks bounded private receipt: %+v", report)
			}
		}
		worker, e := tx.GetWorker(saved.RunRef.WorkerID)
		if e != nil {
			return e
		}
		if worker.NativeSessionID == "" || worker.State != work.WorkerReady {
			return fmt.Errorf("native session/admission not reconciled: %+v", worker)
		}
		native, e := tx.GetNativeCommand(saved.CommandID)
		if e != nil {
			return e
		}
		if native.Ack == nil || native.Ack.RuntimeReceipt == nil {
			return fmt.Errorf("actual wrapper journal ACK was not reconciled")
		}
		return nil
	})
	if group && capacity == 2 && !overlap.observed.Load() {
		t.Fatal("capacity >1 never produced overlapping actual native turns")
	}
	if collaboration {
		journey.check(t, sender)
		return
	}
	if dependency {
		checkPipelineDependency(t, store, scope.ID)
	}
	if platform != nil {
		platform.verify(t, ctx, publisher, backend)
		return
	}
	if n, e := publisher.RunOnce(ctx, 16); e != nil || n != 1 {
		t.Fatalf("actual source reply publication=%d %v", n, e)
	}
	if n, e := publisher.RunOnce(ctx, 16); e != nil || n != 0 {
		t.Fatalf("source reply duplicated=%d %v", n, e)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.requests) != 1 || sender.requests[0].ChatID != raw.ChatID {
		t.Fatalf("source reply crossed conversation: %+v", sender.requests)
	}
}
