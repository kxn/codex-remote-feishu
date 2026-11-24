package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateCleanupTestGateway struct {
	recordingGateway
	base string
}

func (g *privateCleanupTestGateway) PrivateStagingBase(_, _ string) (string, error) {
	return g.base, nil
}

func TestPrivateAttachedFileStagingPersistsBeforeQueueAndRecovers(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	baseDir := t.TempDir()
	owner := privatestaging.Owner{Tenant: "tenant", AppID: "bot", Gateway: "g", EventID: "file-event", MessageID: "file-message"}
	store, err := privatestaging.Open(baseDir, owner)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Create(strings.NewReader("reference data"), "")
	if err != nil {
		t.Fatal(err)
	}
	host := New(":0", ":0", &privateCleanupTestGateway{base: baseDir}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "db")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, privateSettingsNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: "carrier", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, WorkspaceRoot: workspace, WorkspaceKey: workspace, Source: "headless", Managed: true}}
	if err = p.RegisterNativeHello(ctx, lease, hello); err != nil {
		t.Fatal(err)
	}
	admit := func(id, lane string, a control.Action, sources []string) int64 {
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: sources})
		if e != nil {
			t.Fatal(e)
		}
		return seq
	}
	base := control.Action{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	attach := base
	attach.Kind = control.ActionAttachInstance
	attach.InstanceID = "instance"
	if _, err = p.ApplyIngress(ctx, o, 1, admit("attach", "control", attach, nil)); err != nil {
		t.Fatal(err)
	}
	file := base
	file.Kind = control.ActionFileMessage
	file.MessageID = "file-message"
	file.LocalPath = source
	file.FileName = "reference.txt"
	effects, err := p.ApplyIngress(ctx, o, 1, admit("file-event", "input", file, []string{"file-message"}))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, e := range effects {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateStageInput {
			id = e.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("missing stage intent")
	}
	if len(app.Service().Surface(o.SurfaceID).StagedFiles) != 1 {
		t.Fatal("missing durable staging placeholder")
	}
	for _, file := range app.Service().Surface(o.SurfaceID).StagedFiles {
		if !file.StagePending {
			t.Fatal("placeholder was usable before staging")
		}
	}
	next := base
	next.Kind = control.ActionTextMessage
	next.MessageID = "next"
	next.Text = "next"
	nextSeq := admit("next-event", "input", next, []string{"next"})
	if _, err = p.ApplyIngress(ctx, o, 1, nextSeq); !errors.Is(err, privatechat.ErrDeferred) {
		t.Fatalf("input passed staging: %v", err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	workspaceFile, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := privateDirectoryIdentity(workspaceFile)
	workspaceFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	intent, err := privateBuildStageIntent(workspace, identity, file)
	if err != nil {
		t.Fatal(err)
	}
	intentRaw, _ := json.Marshal(intent)
	if err = j.PrepareLocalIntent(ctx, o, 1, id, intentRaw); err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if err = privateStageOne(root, workspace, intent.Files[0]); err != nil {
		t.Fatal(err)
	}
	root.Close()
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	app = privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	p, err = privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	lease, err = j.OpenNativeLease(ctx, privateSettingsNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, hello); err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateStageInput(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatal(err)
	}
	cleanup, err := j.PendingStagingCleanup(ctx, 10)
	if err != nil || len(cleanup) != 1 || cleanup[0].EventID != "file-event" || cleanup[0].MessageID != "file-message" || cleanup[0].Path != source {
		t.Fatalf("durable cleanup evidence: %#v %v", cleanup, err)
	}
	if err = runPrivateStagingCleanup(ctx, host, j); err != nil {
		t.Fatal(err)
	}
	cleanup, err = j.PendingStagingCleanup(ctx, 10)
	if err != nil || len(cleanup) != 0 {
		t.Fatalf("cleanup recovery: %#v %v", cleanup, err)
	}
	checkpoint, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil || len(checkpoint.Core.StagedFiles) != 1 {
		t.Fatalf("file not committed: %v", err)
	}
	for _, f := range checkpoint.Core.StagedFiles {
		data, e := os.ReadFile(f.LocalPath)
		if e != nil || string(data) != "reference data" {
			t.Fatalf("staged bytes: %s %v", data, e)
		}
	}
	if _, err = p.ApplyIngress(ctx, o, 1, nextSeq); err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateStageInput(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app = privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	p, err = privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateStageInput(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatalf("receipt replay after restart: %v", err)
	}
}

func TestPrivateStageRejectsDifferentTargetAndSymlink(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("ours"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, size, err := privateSourceHash(source)
	if err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(workspace, "inbox")
	if err = os.Mkdir(targetDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(targetDir, "file"), []byte("someone else"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	item := privateStagedFile{Source: source, Target: filepath.Join("inbox", "file"), SHA256: hash, Size: size}
	if err = privateStageOne(root, workspace, item); err == nil {
		t.Fatal("different target overwritten")
	}
	data, err := os.ReadFile(filepath.Join(targetDir, "file"))
	if err != nil || string(data) != "someone else" {
		t.Fatal("existing file changed")
	}
	if err = os.Symlink(t.TempDir(), filepath.Join(workspace, "link")); err != nil {
		t.Skip(err)
	}
	item.Target = filepath.Join("link", "file")
	if err = privateStageOne(root, workspace, item); err == nil {
		t.Fatal("symlink directory accepted")
	}
}

func TestPrivateStageCancelledBeforeFilesystemAccess(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	baseDir := t.TempDir()
	owner := privatestaging.Owner{Tenant: "tenant", AppID: "bot", Gateway: "g", EventID: "file-event", MessageID: "file-message"}
	store, err := privatestaging.Open(baseDir, owner)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Create(strings.NewReader("reference data"), "")
	if err != nil {
		t.Fatal(err)
	}
	host := New(":0", ":0", &privateCleanupTestGateway{base: baseDir}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "db")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, privateSettingsNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: "carrier", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, WorkspaceRoot: workspace, WorkspaceKey: workspace, Source: "headless", Managed: true}}
	if err = p.RegisterNativeHello(ctx, lease, hello); err != nil {
		t.Fatal(err)
	}
	admit := func(id, lane string, a control.Action, sources []string) int64 {
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: sources})
		if e != nil {
			t.Fatal(e)
		}
		return seq
	}
	base := control.Action{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	attach := base
	attach.Kind = control.ActionAttachInstance
	attach.InstanceID = "instance"
	if _, err = p.ApplyIngress(ctx, o, 1, admit("attach", "control", attach, nil)); err != nil {
		t.Fatal(err)
	}
	file := base
	file.Kind = control.ActionFileMessage
	file.MessageID = "file-message"
	file.LocalPath = source
	file.FileName = "reference.txt"
	effects, err := p.ApplyIngress(ctx, o, 1, admit("file-event", "input", file, []string{"file-message"}))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, e := range effects {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateStageInput {
			id = e.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("missing stage effect")
	}
	detach := base
	detach.Kind = control.ActionDetach
	if _, err = p.ApplyIngress(ctx, o, 1, admit("detach", "control", detach, nil)); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateStageInput(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatalf("cancelled stage attempted filesystem: %v", err)
	}
	record, err := j.LoadEffect(ctx, o, 1, id)
	if err != nil || record.State != "confirmed" {
		t.Fatalf("cancel not settled: %s %v", record.State, err)
	}
	if len(app.Service().Surface(o.SurfaceID).StagedFiles) != 0 {
		t.Fatal("cancelled file restored")
	}
	if _, err = runPrivateStageInput(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatalf("cancel replay: %v", err)
	}
	j.Close()
}

func TestPrivateStageMissingSourceSettlesFailure(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	baseDir := t.TempDir()
	owner := privatestaging.Owner{Tenant: "tenant", AppID: "bot", Gateway: "g", EventID: "file-event", MessageID: "file-message"}
	store, err := privatestaging.Open(baseDir, owner)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Create(strings.NewReader("reference data"), "")
	if err != nil {
		t.Fatal(err)
	}
	host := New(":0", ":0", &privateCleanupTestGateway{base: baseDir}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "db")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, privateSettingsNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: "carrier", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, WorkspaceRoot: workspace, WorkspaceKey: workspace, Source: "headless", Managed: true}}
	if err = p.RegisterNativeHello(ctx, lease, hello); err != nil {
		t.Fatal(err)
	}
	admit := func(id, lane string, a control.Action, sources []string) int64 {
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: sources})
		if e != nil {
			t.Fatal(e)
		}
		return seq
	}
	base := control.Action{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	attach := base
	attach.Kind = control.ActionAttachInstance
	attach.InstanceID = "instance"
	if _, err = p.ApplyIngress(ctx, o, 1, admit("attach", "control", attach, nil)); err != nil {
		t.Fatal(err)
	}
	file := base
	file.Kind = control.ActionFileMessage
	file.MessageID = "file-message"
	file.LocalPath = source
	file.FileName = "reference.txt"
	effects, err := p.ApplyIngress(ctx, o, 1, admit("file-event", "input", file, []string{"file-message"}))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, e := range effects {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateStageInput {
			id = e.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("missing stage effect")
	}
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateStageInput(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatalf("failure did not settle: %v", err)
	}
	record, err := j.LoadEffect(ctx, o, 1, id)
	if err != nil || record.State != "confirmed" {
		t.Fatalf("failure not settled: %s %v", record.State, err)
	}
	var receipt privateStageReceipt
	if err = json.Unmarshal(record.Receipt, &receipt); err != nil || !receipt.Failed {
		t.Fatalf("failure receipt: %+v %v", receipt, err)
	}
	next := base
	next.Kind = control.ActionTextMessage
	next.MessageID = "next"
	next.Text = "continue"
	if _, err = p.ApplyIngress(ctx, o, 1, admit("next-event", "input", next, []string{"next"})); err != nil {
		t.Fatalf("failure blocked next input: %v", err)
	}
	j.Close()
}

func TestPrivateStageCopiedBeforeDetachNeverRestoresDraft(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	baseDir := t.TempDir()
	owner := privatestaging.Owner{Tenant: "tenant", AppID: "bot", Gateway: "g", EventID: "file-event", MessageID: "file-message"}
	store, err := privatestaging.Open(baseDir, owner)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Create(strings.NewReader("reference data"), "")
	if err != nil {
		t.Fatal(err)
	}
	host := New(":0", ":0", &privateCleanupTestGateway{base: baseDir}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "db")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, privateSettingsNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: "carrier", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, WorkspaceRoot: workspace, WorkspaceKey: workspace, Source: "headless", Managed: true}}
	if err = p.RegisterNativeHello(ctx, lease, hello); err != nil {
		t.Fatal(err)
	}
	admit := func(id, lane string, a control.Action, sources []string) int64 {
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: sources})
		if e != nil {
			t.Fatal(e)
		}
		return seq
	}
	base := control.Action{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	attach := base
	attach.Kind = control.ActionAttachInstance
	attach.InstanceID = "instance"
	if _, err = p.ApplyIngress(ctx, o, 1, admit("attach", "control", attach, nil)); err != nil {
		t.Fatal(err)
	}
	file := base
	file.Kind = control.ActionFileMessage
	file.MessageID = "file-message"
	file.LocalPath = source
	file.FileName = "reference.txt"
	effects, err := p.ApplyIngress(ctx, o, 1, admit("file-event", "input", file, []string{"file-message"}))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, e := range effects {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateStageInput {
			id = e.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("missing stage effect")
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	workspaceFile, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := privateDirectoryIdentity(workspaceFile)
	workspaceFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	intent, err := privateBuildStageIntent(workspace, identity, file)
	if err != nil {
		t.Fatal(err)
	}
	rawIntent, _ := json.Marshal(intent)
	if err = j.PrepareLocalIntent(ctx, o, 1, id, rawIntent); err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if err = privateStageOne(root, workspace, intent.Files[0]); err != nil {
		t.Fatal(err)
	}
	root.Close()
	detach := base
	detach.Kind = control.ActionDetach
	if _, err = p.ApplyIngress(ctx, o, 1, admit("detach", "control", detach, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err = runPrivateStageInput(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatalf("late copied stage: %v", err)
	}
	record, err := j.LoadEffect(ctx, o, 1, id)
	if err != nil || record.State != "confirmed" {
		t.Fatalf("not settled: %s %v", record.State, err)
	}
	var receipt privateStageReceipt
	if err = json.Unmarshal(record.Receipt, &receipt); err != nil || !receipt.Cancelled {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	if len(app.Service().Surface(o.SurfaceID).StagedFiles) != 0 {
		t.Fatal("late stage restored draft")
	}
	cleanup, e := j.PendingStagingCleanup(ctx, 10)
	if e != nil || len(cleanup) != 1 {
		t.Fatalf("cancel cleanup missing: %+v %v", cleanup, e)
	}
	if e = runPrivateStagingCleanup(ctx, host, j); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(source); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("owned staging source retained: %v", e)
	}
	data, err := os.ReadFile(filepath.Join(workspace, intent.Files[0].Target))
	if err != nil || string(data) != "reference data" {
		t.Fatalf("copied evidence lost: %v", err)
	}
	j.Close()
}
