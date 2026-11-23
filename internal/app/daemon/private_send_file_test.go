package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateFileTestGateway struct {
	messageIDAssigningGateway
	files []feishu.IMFileSendRequest
	fail  bool
}

func (g *privateFileTestGateway) SendIMFile(_ context.Context, request feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
	g.files = append(g.files, request)
	if g.fail {
		return feishu.IMFileSendResult{}, errors.New("connection ended after file submission")
	}
	return feishu.IMFileSendResult{GatewayID: request.GatewayID, SurfaceSessionID: request.SurfaceSessionID, MessageID: "sent-file", FileName: filepath.Base(request.Path)}, nil
}

func TestPrivateFilePublicationReceiptPreventsResendAndPreservesUnknownFeedback(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "unknown"}[unknown], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private.db")
			file := filepath.Join(t.TempDir(), "result.txt")
			if err := os.WriteFile(file, []byte("result"), 0600); err != nil {
				t.Fatal(err)
			}
			o := privatechat.Owner{TenantID: "tenant", AppID: "bot", GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user"}
			seed := privatechat.New(nil, orchestrator.Config{})
			if err := seed.RegisterOwner(o); err != nil {
				t.Fatal(err)
			}
			core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			cmd := control.DaemonCommand{Kind: control.DaemonCommandPrivateSendFile, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, LocalPath: file}
			raw, err := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, DaemonCommand: &cmd})
			if err != nil {
				t.Fatal(err)
			}
			gateway := &privateFileTestGateway{fail: unknown}
			open := func() (*privatechat.Journal, *App, *privatechat.Processor) {
				j, e := privatechat.OpenJournal(path)
				if e != nil {
					t.Fatal(e)
				}
				host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
				app := privatechat.New(nil, orchestrator.Config{})
				host.service = app.Service()
				p, e := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
				if e != nil {
					t.Fatal(e)
				}
				return j, host, p
			}
			j, host, p := open()
			if err = j.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, []privatechat.Effect{{ID: "file", Lane: "background", Body: raw}}); err != nil {
				t.Fatal(err)
			}
			feedback, err := runPrivateSendFile(t.Context(), host, p, j, o, 1, "file")
			if err != nil || len(feedback) != 1 {
				t.Fatalf("feedback=%+v err=%v", feedback, err)
			}
			if len(gateway.files) != 1 || gateway.files[0].ChatID != o.ChatID || gateway.files[0].IdempotencyKey == "" {
				t.Fatal("file request lost actual owner or stable SDK identity")
			}
			if unknown {
				e, e2 := privatechat.DecodeEffectEvent(feedback[0].Effect.Body)
				if e2 != nil || e.Notice == nil || e.Notice.Code != "send_file_result_unknown" || !strings.Contains(e.Notice.Text, "connection ended") {
					t.Fatalf("missing actual uncertain outcome: %+v %v", e.Notice, e2)
				}
			}
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			j, host, p = open()
			defer j.Close()
			if _, err = runPrivateSendFile(t.Context(), host, p, j, o, 1, "file"); err != nil {
				t.Fatal(err)
			}
			if len(gateway.files) != 1 {
				t.Fatal("file resent after restart")
			}
		})
	}
}

func TestPrivateFileChangedSnapshotNeverReachesSender(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(file, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{TenantID: "tenant", AppID: "bot", GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user"}
	app := privatechat.New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	cmd := control.DaemonCommand{Kind: control.DaemonCommandPrivateSendFile, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, LocalPath: file, FileSHA256: strings.Repeat("0", 64)}
	raw, err := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, DaemonCommand: &cmd})
	if err != nil {
		t.Fatal(err)
	}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	g := &privateFileTestGateway{}
	app = privatechat.New(nil, orchestrator.Config{})
	host := New(":0", ":0", g, agentproto.ServerIdentity{})
	host.service = app.Service()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, []privatechat.Effect{{ID: "file", Lane: "background", Body: raw}}); err != nil {
		t.Fatal(err)
	}
	feedback, err := runPrivateSendFile(t.Context(), host, p, j, o, 1, "file")
	if err != nil || len(g.files) != 0 || len(feedback) != 1 {
		t.Fatalf("sender calls=%d feedback=%+v err=%v", len(g.files), feedback, err)
	}
	e, err := privatechat.DecodeEffectEvent(feedback[0].Effect.Body)
	if err != nil || e.Notice == nil || e.Notice.Code != "send_file_failed" {
		t.Fatalf("mismatch feedback=%+v err=%v", e.Notice, err)
	}
}

func TestPrivateSendFileCleanupRequiresAppliedKnownReceipt(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "unknown"}[unknown], func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(t.TempDir(), "report.txt")
			if err := os.WriteFile(source, []byte("content"), 0600); err != nil {
				t.Fatal(err)
			}
			snapshot, _, hash, err := snapshotPrivateSendFile(t.Context(), root, "prepare", source)
			if err != nil {
				t.Fatal(err)
			}
			o := privatechat.Owner{TenantID: "tenant", AppID: "bot", GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user"}
			seed := privatechat.New(nil, orchestrator.Config{})
			if err = seed.RegisterOwner(o); err != nil {
				t.Fatal(err)
			}
			core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			cmd := control.DaemonCommand{Kind: control.DaemonCommandPrivateSendFile, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, LocalPath: snapshot, FileSHA256: hash}
			raw, err := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, DaemonCommand: &cmd})
			if err != nil {
				t.Fatal(err)
			}
			j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			app := privatechat.New(nil, orchestrator.Config{})
			g := &privateFileTestGateway{fail: unknown}
			host := New(":0", ":0", g, agentproto.ServerIdentity{})
			host.service = app.Service()
			host.headlessRuntime.Paths.StateDir = root
			p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
			if err != nil {
				t.Fatal(err)
			}
			effects := []privatechat.Effect{{ID: "file", Lane: "background", Body: raw}}
			if !unknown {
				effects = append(effects, privatechat.Effect{ID: "other-reference", Lane: "background", Body: raw})
			}
			if err = j.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
				t.Fatal(err)
			}
			if err = cleanupCompletedPrivateFileSnapshots(t.Context(), root, j); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(snapshot); err != nil {
				t.Fatalf("unresolved reference removed: %v", err)
			}
			if _, err = runPrivateSendFile(t.Context(), host, p, j, o, 1, "file"); err != nil {
				t.Fatal(err)
			}
			if err = cleanupCompletedPrivateFileSnapshots(t.Context(), root, j); err != nil {
				t.Fatal(err)
			}
			if !unknown {
				if _, err = os.Stat(snapshot); err != nil {
					t.Fatalf("pending second reference removed: %v", err)
				}
				if _, err = runPrivateSendFile(t.Context(), host, p, j, o, 1, "other-reference"); err != nil {
					t.Fatal(err)
				}
				if err = cleanupCompletedPrivateFileSnapshots(t.Context(), root, j); err != nil {
					t.Fatal(err)
				}
			}
			_, err = os.Stat(snapshot)
			if unknown && err != nil || !unknown && !os.IsNotExist(err) {
				t.Fatalf("unknown=%v stat=%v", unknown, err)
			}
		})
	}
}
