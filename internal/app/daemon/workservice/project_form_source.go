package workservice

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Standalone bootstrap credentials are issued by the trusted human setup
// handler. This records that submitted text verbatim before provisioning. A
// model worker cannot enter this path or invent a principal-authored message.
func (s *Service) ensureFormSource(tx *workstore.Tx, claims workauth.Claims, req CreateProjectRequest) error {
	if claims.Purpose != "bootstrap" {
		return workauth.ErrUnauthorized
	}
	if source, err := tx.GetMessage(req.SourceMessageID); err == nil {
		scope, e := tx.GetScope(source.ScopeID)
		if e != nil {
			return e
		}
		if source.Source != (work.Actor{Kind: work.ActorPrincipal, ID: claims.PrincipalID}) || scope.Kind != work.ScopeBootstrap || scope.RoleID != claims.RoleID || scope.PrincipalID != claims.PrincipalID || scope.Tenant != claims.Tenant || scope.ChatID != claims.ChatID || scope.BootstrapOperationID != claims.OperationID {
			return workauth.ErrUnauthorized
		}
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	id := stableID("form_input", claims.Tenant, claims.ChatID, claims.OperationID)
	if req.SourceMessageID != id {
		return workauth.ErrUnauthorized
	}
	scopeID := stableID("bootstrap_form", claims.Tenant, claims.ChatID, claims.OperationID)
	scope := work.WorkScope{ID: scopeID, Kind: work.ScopeBootstrap, RoleID: claims.RoleID, PrincipalID: claims.PrincipalID, Tenant: claims.Tenant, ChatID: claims.ChatID, BootstrapOperationID: claims.OperationID, BindingGeneration: req.ExpectedBindingGeneration, Revision: 1, CreatedAt: s.now()}
	if err := tx.PutScope(scope, 0); err != nil {
		return err
	}
	body := work.NewContent(stableID("form_body", id), scopeID, "text/plain; charset=utf-8", []byte(req.InitialRequest), s.now())
	if err := tx.PutContent(body); err != nil {
		return err
	}
	actor := work.Actor{Kind: work.ActorPrincipal, ID: claims.PrincipalID}
	op := work.Operation{ID: claims.OperationID, ScopeID: scopeID, Actor: actor, Kind: "bootstrap.form", TargetID: scopeID, TargetGeneration: 1, IdempotencyKey: claims.OperationID, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationConfirmed, ExternalReceipt: body.ID, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	if _, _, err := tx.PutOperation(op, 0); err != nil {
		return err
	}
	message := work.Message{ID: id, ScopeID: scopeID, ConversationID: "bootstrap:" + scopeID, Source: actor, BodyRef: body.ID, Revision: 1, CreatedAt: s.now()}
	_, _, err := tx.AcceptMessage(message, nil)
	return err
}
