// Package workui projects durable business objects into Feishu. View records
// own presentation and callback freshness, never tasks or native sessions.
package workui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Button struct {
	Label, Action string
	Values        map[string]any
	URL           string
}
type InputForm struct {
	Name, Placeholder string
	Submit            Button
}

type Panel struct {
	// PrivateMenu opts navigation pages into the menu layout. Business cards
	// retain their existing arrangement and intrinsic button widths.
	PrivateMenu bool
	Breadcrumb  string
	Navigation  []Button
	Footer      []Button
	Title       string
	Paragraphs  []string
	Forms       []InputForm
	Buttons     []Button
}

func ID(parts ...string) string {
	body, _ := json.Marshal(parts)
	sum := sha256.Sum256(body)
	return "view_" + hex.EncodeToString(sum[:20])
}

func Card(viewID string, panel Panel) (json.RawMessage, error) {
	if panel.Title == "" {
		return nil, fmt.Errorf("view title required")
	}
	elements := []any{}
	buttonElement := func(button Button) map[string]any {
		var element map[string]any
		if button.URL != "" {
			element = map[string]any{"tag": "button", "text": cardkit.PlainText(button.Label), "behaviors": []any{map[string]any{"type": "open_url", "default_url": button.URL}}}
		} else {
			element = callbackButton(viewID, button)
		}
		if panel.PrivateMenu {
			element["width"] = "fill"
		}
		return element
	}
	if panel.PrivateMenu {
		if panel.Breadcrumb != "" {
			elements = append(elements, cardkit.PlainTextBlockElement(panel.Breadcrumb))
		}
		for _, button := range panel.Navigation {
			elements = append(elements, buttonElement(button))
		}
	}
	for _, text := range panel.Paragraphs {
		if block := cardkit.PlainTextBlockElement(text); block != nil {
			elements = append(elements, block)
		}
	}
	for _, form := range panel.Forms {
		field := map[string]any{"tag": "input", "name": form.Name, "placeholder": cardkit.PlainText(form.Placeholder), "width": "fill"}
		submit := callbackButton(viewID, form.Submit)
		if panel.PrivateMenu {
			submit["width"] = "fill"
		}
		submit["name"] = form.Name + "_submit"
		submit["form_action_type"] = "submit"
		elements = append(elements, map[string]any{"tag": "form", "name": form.Name + "_form", "elements": []any{field, submit}})
	}
	for _, button := range panel.Buttons {
		elements = append(elements, buttonElement(button))
	}
	if panel.PrivateMenu && len(panel.Footer) > 0 {
		elements = append(elements, map[string]any{"tag": "hr"})
		for _, button := range panel.Footer {
			elements = append(elements, buttonElement(button))
		}
	}
	if len(elements) > 150 {
		return nil, fmt.Errorf("view needs pagination")
	}
	return json.Marshal(map[string]any{"schema": "2.0", "config": map[string]any{"update_multi": true}, "header": map[string]any{"title": cardkit.PlainText(panel.Title)}, "body": map[string]any{"elements": elements}})
}

func callbackButton(viewID string, button Button) map[string]any {
	value := cardkit.CloneMap(button.Values)
	if value == nil {
		value = map[string]any{}
	}
	value["team_view"] = viewID
	value["action"] = button.Action
	return map[string]any{"tag": "button", "text": cardkit.PlainText(button.Label), "behaviors": []any{map[string]any{"type": "callback", "value": value}}}
}

// Queue is atomic with its caller's business transaction. Failed publication
// uses the same outbox, UUID, binding checks and recovery as role messages.
func Queue(tx *workstore.Tx, view workstore.UIView, card json.RawMessage) (workstore.UIView, error) {
	if view.ID == "" || len(card) == 0 || len(card) > 28*1024 || !json.Valid(card) {
		return view, fmt.Errorf("invalid view body")
	}
	projection := workservice.MessageProjection{RoleID: view.Binding.RoleID, GatewayRef: view.Binding.GatewayRef, AppID: view.Binding.AppID, BotGeneration: view.Binding.IdentityGeneration, Tenant: view.Tenant, ChatID: view.ChatID, ChannelGeneration: view.ChannelGeneration, ReplyToMessageID: view.ReplyToMessageID, Card: card, UIViewID: view.ID}
	if view.ReplacesViewID != "" {
		old, e := tx.GetUIView(view.ReplacesViewID)
		if e != nil {
			return view, e
		}
		op, e := tx.GetOperation(old.OperationID)
		if e != nil {
			return view, e
		}
		existing, lookupErr := tx.GetUIView(view.ID)
		replay := lookupErr == nil && existing.ReplacesViewID == view.ReplacesViewID
		if lookupErr != nil && !errors.Is(lookupErr, workstore.ErrNotFound) {
			return view, lookupErr
		}
		snapshotAllowed := false
		if old.Invalidated && !replay {
			snapshotAllowed, e = SnapshotReplacementAllowed(tx, old, view)
			if e != nil {
				return view, e
			}
		}
		if (old.Invalidated && !replay && !snapshotAllowed) || old.ExternalMessageID == "" || op.State != work.OperationConfirmed ||
			old.ScopeID != view.ScopeID || old.SurfaceKey != view.SurfaceKey || old.Binding != view.Binding ||
			old.ChatID != view.ChatID || old.Tenant != view.Tenant || old.PrincipalID != view.PrincipalID {
			return view, fmt.Errorf("replacement no longer owns the displayed message")
		}
		projection.ReplaceMessageID = old.ExternalMessageID
		projection.ReplyToMessageID = ""
	}
	body, err := json.Marshal(projection)
	if err != nil {
		return view, err
	}
	content := work.NewContent(view.ID+":body", view.ScopeID, "application/json", body, time.Now().UTC())
	if old, e := tx.GetUIView(view.ID); e == nil {
		stored, e := tx.GetContent(old.ScopeID, old.BodyRef)
		if e != nil {
			return view, e
		}
		if stored.SHA256 != content.SHA256 {
			return view, workstore.ErrIdempotencyConflict
		}
		return old, nil
	} else if !errors.Is(e, workstore.ErrNotFound) {
		return view, e
	}
	if err = tx.PutContent(content); err != nil {
		return view, err
	}
	view.BodyRef = content.ID
	view.OperationID = view.ID + ":publish"
	view.Revision = 1
	view.CreatedAt = time.Now().UTC()
	op := work.Operation{ID: view.OperationID, ScopeID: view.ScopeID, ContextID: view.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "team-ui"}, Kind: "feishu.publish", TargetID: view.ChatID, TargetGeneration: view.Binding.IdentityGeneration, IdempotencyKey: view.OperationID, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: view.CreatedAt, UpdatedAt: view.CreatedAt}
	if _, _, err = tx.PutOperation(op, 0); err != nil {
		return view, err
	}
	if err = tx.PutOutbox(work.Outbox{ID: view.ID + ":outbox", ScopeID: view.ScopeID, OperationID: op.ID, Target: view.ChatID, EventID: op.ID, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}, 0); err != nil {
		return view, err
	}
	previous, err := tx.ListUIViews(view.ScopeID)
	if err != nil {
		return view, err
	}
	for _, old := range previous {
		if !old.Invalidated && (old.SurfaceKey == view.SurfaceKey || legacyPrivateNavigationOwner(old, view)) {
			revision := old.Revision
			old.Revision++
			old.Invalidated = true
			if err = tx.PutUIView(old, revision); err != nil {
				return view, err
			}
		}
	}
	err = tx.PutUIView(view, 0)
	return view, err
}
