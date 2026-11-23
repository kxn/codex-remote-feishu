package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPrivateReactionActionsV1 = `CREATE TABLE IF NOT EXISTS team_private_reaction_actions(id TEXT PRIMARY KEY,state TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)));`

type PrivateReactionAction struct {
	RawIngressID       string                      `json:"raw_ingress_id,omitempty"`
	DeferredCheckedAt  time.Time                   `json:"deferred_checked_at,omitempty"`
	InputIDs           []string                    `json:"input_ids"`
	Execution          agentproto.ExecutionBinding `json:"execution"`
	Incarnation        string                      `json:"incarnation"`
	SessionID          string                      `json:"session_id"`
	TurnID             string                      `json:"turn_id"`
	ID                 string                      `json:"id"`
	ScopeID            string                      `json:"scope_id"`
	ContextID          string                      `json:"context_id"`
	InputID            string                      `json:"input_id"`
	PrincipalID        string                      `json:"principal_id"`
	Binding            work.RoleBotBinding         `json:"binding"`
	NavigationRevision int64                       `json:"navigation_revision"`
	Emoji              string                      `json:"emoji"`
	State              string                      `json:"state"`
	LastError          string                      `json:"last_error,omitempty"`
	Revision           int64                       `json:"revision"`
}
type privateReactionFacts struct {
	AppID          string         `json:"app_id"`
	GatewayRef     string         `json:"gateway_ref"`
	MessageID      string         `json:"message_id"`
	Tenant         string         `json:"tenant"`
	ChatID         string         `json:"chat_id"`
	OperatorType   string         `json:"operator_type"`
	OperatorTenant string         `json:"operator_tenant"`
	OperatorIDs    work.SenderIDs `json:"operator_ids"`
	Emoji          string         `json:"emoji"`
}

func (t *Tx) GetPlatformObservation(id string) (v PlatformObservation, err error) {
	var raw string
	err = t.tx.QueryRowContext(t.ctx, "SELECT payload FROM team_platform_observations WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return
}
func (t *Tx) GetPrivateReactionAction(id string) (v PrivateReactionAction, err error) {
	var raw string
	err = t.tx.QueryRowContext(t.ctx, "SELECT payload FROM team_private_reaction_actions WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return
}
func (t *Tx) PutPrivateReactionAction(v PrivateReactionAction, expected int64) error {
	if v.ID == "" || (v.InputID == "" && v.RawIngressID == "") || v.ContextID == "" || v.Revision != expected+1 {
		return fmt.Errorf("reaction action requires exact pending source")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = t.tx.ExecContext(t.ctx, "INSERT INTO team_private_reaction_actions(id,state,payload) VALUES(?,?,?)", v.ID, v.State, string(raw))
		return err
	}
	old, err := t.GetPrivateReactionAction(v.ID)
	if err != nil {
		return err
	}
	if old.Revision != expected {
		return ErrConflict
	}
	resolving := old.State == "waiting_source" && v.State == "prepared" && old.RawIngressID != "" && (old.InputID == "" || old.InputID == v.InputID)
	if old.RawIngressID != v.RawIngressID || (!resolving && (!reflect.DeepEqual(old.InputIDs, v.InputIDs) || old.InputID != v.InputID)) || old.Execution != v.Execution || old.Incarnation != v.Incarnation || old.SessionID != v.SessionID || old.TurnID != v.TurnID || old.ScopeID != v.ScopeID || old.ContextID != v.ContextID || old.Binding != v.Binding || old.PrincipalID != v.PrincipalID || old.Emoji != v.Emoji || old.NavigationRevision != v.NavigationRevision {
		return ErrIdempotencyConflict
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE team_private_reaction_actions SET state=?,payload=? WHERE id=? AND json_extract(payload,'$.revision')=?", v.State, string(raw), v.ID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func reactionEmoji(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "THUMBSUP":
		return "THUMBSUP"
	case "APPLAUSE":
		return "APPLAUSE"
	}
	return ""
}
func (t *Tx) RegisterPrivateReactionAction(observationID string) error {
	if _, err := t.GetPrivateReactionAction(observationID); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	observation, err := t.GetPlatformObservation(observationID)
	if err != nil {
		return err
	}
	var facts privateReactionFacts
	if err = json.Unmarshal(observation.Facts, &facts); err != nil {
		return err
	}
	emoji := reactionEmoji(facts.Emoji)
	if observation.Kind != "reaction.created" || facts.OperatorType != "user" || emoji == "" || facts.AppID != observation.Gateway.AppID || facts.GatewayRef != observation.Gateway.GatewayRef || facts.MessageID != observation.MessageID || facts.Tenant != observation.Tenant || (facts.OperatorTenant != "" && facts.OperatorTenant != facts.Tenant) {
		return nil
	}
	// A reaction cannot manufacture new human identity aliases. Only the actual
	// operator already verified through this bot's SDK message observation counts.
	evidence := work.IdentityEvidence{Tenant: observation.Tenant, AppID: observation.Gateway.AppID, Generation: observation.Gateway.BotGeneration, IDs: facts.OperatorIDs}
	principal := ""
	for _, alias := range identityAliases(evidence) {
		identity, e := t.GetExternalIdentity(alias)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}
		if principal != "" && principal != identity.PrincipalID {
			return nil
		}
		principal = identity.PrincipalID
	}
	if principal == "" {
		return nil
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT i.payload FROM inbox i JOIN messages m ON m.id=json_extract(i.payload,'$.message_id') JOIN scopes s ON s.id=json_extract(i.payload,'$.scope_id') WHERE json_extract(s.payload,'$.kind')='personal' AND json_extract(s.payload,'$.principal_id')=? AND json_extract(s.payload,'$.role_id')=? AND json_extract(m.payload,'$.platform_key.tenant')=? AND json_extract(m.payload,'$.platform_key.message_id')=? AND json_extract(i.payload,'$.state')='pending' AND coalesce(json_extract(i.payload,'$.control_of_input_id'),'')='' AND coalesce(json_extract(i.payload,'$.session_goal_intent_id'),'')='' LIMIT 2`, principal, observation.Gateway.RoleID, observation.Tenant, observation.MessageID)
	if err != nil {
		return err
	}
	var inputs []work.Inbox
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var input work.Inbox
		if err = json.Unmarshal([]byte(raw), &input); err != nil {
			rows.Close()
			return err
		}
		inputs = append(inputs, input)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return t.deferPrivateReactionSource(observation, facts, principal)
	}
	if len(inputs) != 1 {
		return nil
	}
	input := inputs[0]
	if input.ContentState == work.InputContentPending {
		return t.deferPrivateReactionSource(observation, facts, principal)
	}
	if input.RecipientContextID == "" || input.AwaitingText {
		return nil
	}
	binding, err := t.GetRoleBotBinding(input.RecipientRoleID)
	if err != nil {
		return err
	}
	cluster, err := t.PendingInputMediaCluster(input)
	if err != nil {
		return err
	}
	var frozenIDs []string
	for _, member := range cluster {
		frozenIDs = append(frozenIDs, member.ID)
	}
	action := PrivateReactionAction{InputIDs: frozenIDs, ID: observationID, ScopeID: input.ScopeID, ContextID: input.RecipientContextID, InputID: input.ID, PrincipalID: principal, Binding: binding, Emoji: emoji, State: "prepared", Revision: 1}
	nav, err := t.GetPrivateNavigation(input.ScopeID)
	if err != nil {
		return err
	}
	action.NavigationRevision = nav.Revision
	if nav.FrontContextID != input.RecipientContextID {
		return nil
	}
	if err = t.freezePrivateReactionTarget(&action); err != nil {
		return nil
	}
	if err = t.checkPrivateReactionAction(action, observation, facts); err != nil {
		return nil
	}
	return t.PutPrivateReactionAction(action, 0)
}
func (t *Tx) checkPrivateReactionAction(action PrivateReactionAction, observation PlatformObservation, facts privateReactionFacts) error {
	input, err := t.GetInbox(action.InputID)
	if err != nil {
		return err
	}
	message, err := t.GetMessage(input.MessageID)
	if err != nil {
		return err
	}
	scope, err := t.GetScope(action.ScopeID)
	if err != nil {
		return err
	}
	binding, err := t.GetRoleBotBinding(scope.RoleID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal || scope.PrincipalID != action.PrincipalID || scope.RoleID != input.RecipientRoleID || binding != action.Binding || binding.AppID != observation.Gateway.AppID || binding.IdentityGeneration != observation.Gateway.BotGeneration || binding.GatewayRef != observation.Gateway.GatewayRef || facts.OperatorType != "user" || reactionEmoji(facts.Emoji) != action.Emoji || message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) || message.PlatformKey == nil || message.PlatformKey.Tenant != observation.Tenant || message.PlatformKey.MessageID != observation.MessageID || (facts.ChatID != "" && facts.ChatID != message.PlatformKey.ChatID) || input.ScopeID != action.ScopeID || input.RecipientContextID != action.ContextID {
		return ErrScopeDenied
	}
	observed := false
	for _, o := range message.Observations {
		if o.AppID == binding.AppID && o.IdentityGeneration == binding.IdentityGeneration {
			observed = true
		}
	}
	if !observed {
		return ErrScopeDenied
	}
	recalled, err := t.IsPlatformMessageRecalled(message.PlatformKey.Tenant, message.PlatformKey.ChatID, message.PlatformKey.MessageID)
	if err != nil {
		return err
	}
	if recalled || message.RecalledEvidenceRef != "" {
		return ErrScopeDenied
	}
	nav, err := t.GetPrivateNavigation(scope.ID)
	if err != nil {
		return err
	}
	if nav.Revision != action.NavigationRevision || nav.FrontContextID != action.ContextID {
		return ErrConflict
	}
	return t.validatePrivateReactionTarget(action)
}
func (t *Tx) ValidatePrivateReactionAction(id string) (work.WorkScope, work.SourceRef, error) {
	action, err := t.GetPrivateReactionAction(id)
	if err != nil {
		return work.WorkScope{}, work.SourceRef{}, err
	}
	scope, err := t.GetScope(action.ScopeID)
	if err != nil {
		return scope, work.SourceRef{}, err
	}
	observation, err := t.GetPlatformObservation(id)
	if err != nil {
		return scope, work.SourceRef{}, err
	}
	var facts privateReactionFacts
	if err = json.Unmarshal(observation.Facts, &facts); err != nil {
		return scope, work.SourceRef{}, err
	}
	if err = t.checkPrivateReactionAction(action, observation, facts); err != nil {
		return scope, work.SourceRef{}, err
	}
	return scope, work.SourceRef{Kind: "private_reaction", ID: id, Version: strconv.FormatInt(action.Revision, 10), EvidenceRef: id}, nil
}
func (t *Tx) ListPreparedPrivateReactionActions(limit int) ([]PrivateReactionAction, error) {
	if err := t.resolveDeferredPrivateReactionSources(limit); err != nil {
		return nil, err
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM team_private_reaction_actions WHERE state='prepared' ORDER BY id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PrivateReactionAction
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var value PrivateReactionAction
		if err = json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func (t *Tx) FinishPrivateReactionAction(id, reason string) error {
	v, err := t.GetPrivateReactionAction(id)
	if err != nil {
		return err
	}
	if v.State != "prepared" {
		return nil
	}
	previous := v.Revision
	v.Revision++
	v.State = "handled"
	v.LastError = reason
	return t.PutPrivateReactionAction(v, previous)
}
