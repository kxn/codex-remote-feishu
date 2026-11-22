package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type runtimeRecoveryPlatform struct {
	creates, patches int
	unknown          bool
}

func (p *runtimeRecoveryPlatform) Resolve(context.Context, work.RoleBotBinding) (workpublish.Sender, error) {
	return p, nil
}
func (p *runtimeRecoveryPlatform) PublishText(context.Context, feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	panic("expected recovery card")
}
func (p *runtimeRecoveryPlatform) PublishCard(_ context.Context, r feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	p.creates++
	if p.unknown {
		return feishu.ProjectPublishReceipt{}, errors.New("unknown send")
	}
	return feishu.ProjectPublishReceipt{MessageID: "runtime-message", ChatID: r.ChatID}, nil
}
func (p *runtimeRecoveryPlatform) PatchCard(_ context.Context, r feishu.ProjectCardPatchRequest) (feishu.ProjectPublishReceipt, error) {
	p.patches++
	if r.MessageID != "runtime-message" {
		return feishu.ProjectPublishReceipt{}, errors.New("wrong original message")
	}
	return feishu.ProjectPublishReceipt{MessageID: r.MessageID, ChatID: r.ChatID}, nil
}

func runtimeRecoveryFixture(t *testing.T) (*App, work.WorkerSession) {
	t.Helper()
	a := teamProjectViewFixture(t)
	worker := work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if err := tx.PutContext(work.WorkContext{ID: worker.ContextID, ScopeID: worker.ScopeID, RoleID: worker.RoleID, Workstream: "main", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutWorker(worker, 0)
	})
	return a, worker
}

func TestRuntimeRecoveryDefersOnlyTransientReconciliation(t *testing.T) {
	for _, tt := range []struct {
		name, reason, recovery string
		age                    time.Duration
		visible                bool
	}{
		{"routine", "native_reconciliation", "", time.Second, false},
		{"persistent", "native_reconciliation", "", 31 * time.Second, true},
		{"underlying failure", "native_reconciliation", "missing_processing_report", time.Second, true},
		{"budget", "native_goal_budget_exhausted", "", time.Second, true},
		{"error", "native_resume_rejected", "", time.Second, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, worker := runtimeRecoveryFixture(t)
			created := time.Now().Add(-tt.age)
			pickerTx(t, a, func(tx *workstore.Tx) error {
				gate := workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: tt.reason, RecoveryReason: tt.recovery, CreatedAt: created}
				if err := tx.PutRuntimeGate(gate); err != nil {
					return err
				}
				gate.CreatedAt = time.Now()
				if err := tx.PutRuntimeGate(gate); err != nil {
					return err
				}
				saved, err := tx.GetRuntimeGate(worker.ID)
				if err == nil && !saved.CreatedAt.Equal(created) {
					t.Fatal("repeated gate reset grace timer")
				}
				return err
			})
			if err := a.runTeamRuntimeViews(t.Context()); err != nil {
				t.Fatal(err)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				views, err := tx.ListUIViews(worker.ScopeID)
				if (len(views) > 0) != tt.visible {
					t.Fatalf("visible=%v want=%v", len(views) > 0, tt.visible)
				}
				for _, v := range views {
					body, e := tx.GetContent(v.ScopeID, v.BodyRef)
					if e != nil {
						return e
					}
					if strings.Contains(string(body.Data), "runtime.observe") {
						t.Fatal("gate with no attempt offered impossible observe")
					}
				}
				return err
			})
		})
	}
}

func TestRuntimeRecoveryTransitionsPatchOneConfirmedMessage(t *testing.T) {
	a, worker := runtimeRecoveryFixture(t)
	platform := &runtimeRecoveryPlatform{}
	publisher, err := workpublish.New(a.teamRuntime.store, platform, nil)
	if err != nil {
		t.Fatal(err)
	}
	putGate := func(reason string) {
		pickerTx(t, a, func(tx *workstore.Tx) error {
			return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: "attempt", Reason: reason, CreatedAt: time.Now().Add(-time.Minute)})
		})
	}
	publish := func() {
		t.Helper()
		if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 1 {
			t.Fatalf("published=%d %v", n, e)
		}
	}
	active := func() workstore.UIView {
		t.Helper()
		var result workstore.UIView
		pickerTx(t, a, func(tx *workstore.Tx) error {
			views, e := tx.ListUIViews(worker.ScopeID)
			for _, view := range views {
				if !view.Invalidated {
					if result.ID != "" {
						t.Fatal("multiple active recovery views")
					}
					result = view
				}
			}
			return e
		})
		return result
	}
	putGate("missing_processing_report")
	if err = a.runTeamRuntimeViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	publish()
	first := active()
	click := workstore.HumanInteraction{Gateway: work.IngressGateway{GatewayRef: "gateway", AppID: first.Binding.AppID, RoleID: "leader", BotGeneration: 1}, Envelope: work.HumanInteractionEnvelope{Tenant: first.Tenant, ChatID: first.ChatID, MessageID: first.ExternalMessageID}}
	pickerTx(t, a, func(tx *workstore.Tx) error { return tx.ValidateViewInteraction(first, click) })
	putGate("carrier_disconnected")
	if err = a.runTeamRuntimeViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	publish()
	second := active()
	if first.ExternalMessageID != "runtime-message" || second.ExternalMessageID != first.ExternalMessageID || second.ReplacesViewID != first.ID {
		t.Fatalf("recovery changed message: first=%+v second=%+v", first, second)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		old, e := tx.GetUIView(first.ID)
		if e != nil {
			return e
		}
		if e = tx.ValidateViewInteraction(old, click); e == nil {
			t.Fatal("old recovery buttons still active")
		}
		return nil
	})
	// The same projection path used after a successful observe reports the real
	// still-blocked state without a second generic success card.
	pickerTx(t, a, func(tx *workstore.Tx) error { return a.refreshTeamRecoveryView(tx, worker) })
	if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 0 {
		t.Fatalf("unchanged observe appended notice: %d %v", n, e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if e := tx.ClearRuntimeGate(worker.ID, "carrier_disconnected"); e != nil {
			return e
		}
		return a.refreshTeamRecoveryView(tx, worker)
	})
	publish()
	retired := active()
	if retired.ExternalMessageID != first.ExternalMessageID || retired.Kind != "runtime.status" {
		t.Fatalf("retirement changed message: %+v", retired)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews(worker.ScopeID)
		if e != nil {
			return e
		}
		for _, v := range views {
			if v.Kind == "runtime.notice" {
				t.Fatal("generic success notice appended")
			}
		}
		body, e := tx.GetContent(retired.ScopeID, retired.BodyRef)
		if e != nil {
			return e
		}
		var projection workservice.MessageProjection
		if e = json.Unmarshal(body.Data, &projection); e != nil {
			return e
		}
		if strings.Contains(string(projection.Card), "runtime.observe") || strings.Contains(string(projection.Card), "工作已完成") {
			t.Fatal("retired view retained controls or fabricated completion")
		}
		return nil
	})
	if platform.creates != 1 || platform.patches != 2 {
		t.Fatalf("creates=%d patches=%d", platform.creates, platform.patches)
	}
}

func TestRuntimeRecoveryDoesNotPatchUnknownOriginalSend(t *testing.T) {
	a, worker := runtimeRecoveryFixture(t)
	platform := &runtimeRecoveryPlatform{unknown: true}
	publisher, err := workpublish.New(a.teamRuntime.store, platform, nil)
	if err != nil {
		t.Fatal(err)
	}
	putGate := func(reason string) {
		pickerTx(t, a, func(tx *workstore.Tx) error {
			return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: reason, CreatedAt: time.Now().Add(-time.Minute)})
		})
		if e := a.runTeamRuntimeViews(t.Context()); e != nil {
			t.Fatal(e)
		}
	}
	putGate("missing_processing_report")
	if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 1 {
		t.Fatalf("unknown publication=%d %v", n, e)
	}
	var original workstore.UIView
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews(worker.ScopeID)
		if e == nil {
			original = views[0]
		}
		return e
	})
	platform.unknown = false
	putGate("carrier_disconnected")
	if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 1 {
		t.Fatalf("new publication=%d %v", n, e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		operation, e := tx.GetOperation(original.OperationID)
		if e != nil {
			return e
		}
		if operation.State != work.OperationUnknown {
			t.Fatalf("unknown original changed state: %s", operation.State)
		}
		views, e := tx.ListUIViews(worker.ScopeID)
		if e != nil {
			return e
		}
		for _, view := range views {
			if !view.Invalidated && view.ReplacesViewID != "" {
				t.Fatal("unknown original reused as confirmed message")
			}
		}
		return nil
	})
	if platform.creates != 2 || platform.patches != 0 {
		t.Fatalf("unknown send replayed or patched: %d creates %d patches", platform.creates, platform.patches)
	}
}
