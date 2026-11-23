package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type privateProjectReceipt struct {
	Version  int
	EffectID string
	Command  control.DaemonCommand
	Page     control.FeishuPageView
}
type privateProjectHostReducer struct{}

func (privateProjectHostReducer) ReduceHostReceipt(_ *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var receipt privateProjectReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != effect.ID || original.DaemonCommand == nil {
		return nil, privatechat.ErrJournalConflict
	}
	a, _ := json.Marshal(original.DaemonCommand)
	b, _ := json.Marshal(receipt.Command)
	if string(a) != string(b) {
		return nil, privatechat.ErrJournalConflict
	}
	var action control.Action
	if json.Unmarshal([]byte(receipt.Command.Text), &action) != nil {
		return nil, privatechat.ErrJournalConflict
	}
	return []eventcontract.Event{projectPageEvent(action, receipt.Page)}, nil
}
func (f privateProjectFacade) privateOwner(ctx context.Context, o privatechat.Owner, epoch int64) (ingressowner.Owner, error) {
	gateway, e := (teamDirectory{app: f.app, store: f.app.teamRuntime.store}).ResolveGateway(ctx, o.GatewayID, o.AppID)
	if e != nil {
		return ingressowner.Owner{}, e
	}
	b, e := f.router.PrivateBindingForOperator(ctx, o.TenantID, o.AppID, o.GatewayID, gateway.BotGeneration, o.ChatID, o.UserID)
	if e != nil {
		return ingressowner.Owner{}, e
	}
	if b.Owner.Epoch != epoch {
		return ingressowner.Owner{}, workstore.ErrScopeDenied
	}
	return b.Owner, nil
}
func runPrivateProjectEffect(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || event.SurfaceSessionID != o.SurfaceID || cmd.SurfaceSessionID != o.SurfaceID || (cmd.Kind != control.DaemonCommandPrivateProjectQuery && cmd.Kind != control.DaemonCommandPrivateProjectShare) {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateProjectHostReducer{})
	}
	if record.State != "prepared" && record.State != "unknown" && record.State != "sending" {
		return nil, privatechat.ErrJournalState
	}
	var action control.Action
	if json.Unmarshal([]byte(cmd.Text), &action) != nil || action.GatewayID != o.GatewayID || action.SurfaceSessionID != o.SurfaceID || action.ChatID != o.ChatID || action.ActorUserID != o.UserID {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	facade := privateProjectFacade{app: host, router: host.applications.router, journal: j}
	owner, err := facade.privateOwner(ctx, o, generation)
	var authority privateProjectAuthority
	if err == nil {
		authority, err = facade.authority(ctx, o, generation, owner)
	}
	if errors.Is(err, privatechat.ErrDeferred) {
		return nil, err
	}
	var page control.FeishuPageView
	if err != nil {
		page = pageUnavailable()
		err = nil
	} else if cmd.Kind == control.DaemonCommandPrivateProjectQuery {
		page, err = facade.renderPage(ctx, authority, action, id)
	} else {
		page, err = executePrivateProjectShare(ctx, facade, p, j, authority, owner, id, action)
	}
	if err != nil {
		return nil, err
	}
	result := privateProjectReceipt{Version: 1, EffectID: id, Command: *cmd, Page: page}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(saveCtx, o, generation, id, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(saveCtx, o, generation, id, privateProjectHostReducer{})
}

func executePrivateProjectShare(ctx context.Context, facade privateProjectFacade, p *privatechat.Processor, j *privatechat.Journal, a privateProjectAuthority, owner ingressowner.Owner, effectID string, action control.Action) (control.FeishuPageView, error) {
	args := strings.Fields(action.Text)
	if len(args) != 4 || args[0] != "/project" || args[1] != "confirm" || action.Inbound == nil || !action.Inbound.CardCallback || action.Inbound.OpenMessageID == "" {
		return control.FeishuPageView{}, workstore.ErrScopeDenied
	}
	draft, err := j.ProjectShareDraft(ctx, a.Owner, a.Epoch, args[2])
	if err != nil {
		return pageUnavailable(), nil
	}
	hash, err := privatechat.ProjectShareDraftDigest(draft)
	if err != nil || hash != args[3] {
		return pageUnavailable(), nil
	}
	entry, err := j.IngressForEffect(ctx, a.Owner, a.Epoch, effectID)
	if err != nil {
		return control.FeishuPageView{}, err
	}
	record, err := facade.router.Get(ctx, ingressowner.Key{Tenant: a.Owner.TenantID, AppID: a.Owner.AppID, EventID: entry.Ingress.EventID})
	if err != nil {
		return control.FeishuPageView{}, err
	}
	target := record.Owner
	if record.DeliveryTarget != nil {
		target = record.DeliveryTarget
	}
	if record.State != ingressowner.Accepted || record.Event.Kind != ingressowner.Card || record.Event.CardKind != "private_view" || target == nil || *target != owner || record.Event.MessageID != action.Inbound.OpenMessageID || record.Event.Operator != a.Binding.Operator {
		return control.FeishuPageView{}, workstore.ErrScopeDenied
	}
	canonical, err := (privateApplicationAdmitter{router: facade.router}).BuildPrivateIngress(ctx, record)
	if err != nil {
		return control.FeishuPageView{}, err
	}
	var actual control.Action
	if json.Unmarshal(canonical.Payload, &actual) != nil || actual.Kind != control.ActionPrivateProject || actual.Text != action.Text || actual.ActorUserID != a.Owner.UserID || actual.Inbound == nil || !actual.Inbound.CardCallback || actual.Inbound.OpenMessageID != action.Inbound.OpenMessageID {
		return control.FeishuPageView{}, workstore.ErrScopeDenied
	}
	factHash, err := ingressowner.EventFactHash(record.Event)
	if err != nil {
		return control.FeishuPageView{}, err
	}
	// Recheck authority immediately before the receiving transaction; network
	// membership refresh, when needed, stays outside all SQLite transactions.
	err = facade.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { _, e := facade.authorizedProject(tx, a, draft.ProjectID); return e })
	if err != nil {
		if e := facade.app.verifyWorkMembership(ctx, a.Principal.ID, projectScopeForShare(ctx, facade, draft.ProjectID), a.Owner.TenantID, a.IDs, a.Gateway); e != nil {
			return pageUnavailable(), nil
		}
	}
	text := draft.Text
	if draft.MemoryID != "" {
		m, e := j.ReadMemory(ctx, a.Owner, a.Epoch, draft.MemoryID, draft.MemoryRevision)
		if e != nil || m.Content != draft.Text {
			return projectPage("分享已失效", "私人记忆已变更，请重新准备。"), nil
		}
	}
	var resourceHash string
	if draft.ResourceID != "" {
		v, e := facade.readVersion(ctx, a, draft.ProjectID, draft.ResourceID, draft.VersionID)
		if e != nil || !v.Saved || v.ContentHash == "" {
			return pageUnavailable(), nil
		}
		resourceHash = v.ContentHash
		text += fmt.Sprintf("\n\n固定资料版本：%s", resourceHash)
	}
	if strings.TrimSpace(text) == "" {
		return pageUnavailable(), nil
	}
	share := workstore.PrivateProjectShare{ID: draft.ID, ProjectID: draft.ProjectID, PrincipalID: a.Principal.ID, SourceRoleID: a.Gateway.RoleID, SourceBotGeneration: a.Gateway.BotGeneration, PrivateOwnerID: owner.ID, PrivateEpoch: owner.Epoch, DraftHash: hash, ConsentEventID: record.Event.Key.EventID, ConsentFactHash: factHash, Text: text, ResourceID: draft.ResourceID, VersionID: draft.VersionID, ResourceHash: resourceHash, CreatedAt: record.Event.Source.ReceivedAt}
	var result workstore.PrivateProjectShareReceipt
	err = p.InspectPendingHostIntent(ctx, a.Owner, a.Epoch, effectID, func(_ *privatechat.App, current privatechat.EffectRecord) error {
		if current.Effect.ID != effectID {
			return privatechat.ErrJournalConflict
		}
		return facade.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; result, e = tx.AdmitPrivateProjectShare(share); return e })
	})
	if err != nil {
		return control.FeishuPageView{}, err
	}
	receipt, err := json.Marshal(result)
	if err != nil {
		return control.FeishuPageView{}, err
	}
	if err = j.RecordProjectShareReceipt(ctx, a.Owner, a.Epoch, draft.ID, receipt); err != nil {
		return control.FeishuPageView{}, err
	}
	facade.app.teamRuntime.wakeLane("team-inbox")
	return projectPage("已分享", "指定内容已登记，等待项目负责人处理。"), nil
}
func projectScopeForShare(ctx context.Context, f privateProjectFacade, id string) string {
	var scope string
	_ = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		p, e := tx.GetProject(id)
		if e == nil {
			scope = p.ScopeID
		}
		return e
	})
	return scope
}
