package workstore

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The observation is already durable. Waiting for expansion never chooses a
// newer destination: freeze its exact accepted raw route and active tuple now.
func (t *Tx) deferPrivateReactionSource(observation PlatformObservation, facts privateReactionFacts, principal string) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM raw_ingress WHERE json_extract(payload,'$.envelope.tenant')=? AND json_extract(payload,'$.envelope.message_id')=? AND json_extract(payload,'$.gateway.app_id')=? AND json_extract(payload,'$.gateway.role_id')=? LIMIT 2`, observation.Tenant, observation.MessageID, observation.Gateway.AppID, observation.Gateway.RoleID)
	if err != nil {
		return err
	}
	var records []work.IngressRecord
	for rows.Next() {
		var raw string
		var record work.IngressRecord
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &record); err != nil {
			rows.Close()
			return err
		}
		records = append(records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(records) != 1 {
		return nil
	}
	record := records[0]
	if record.Gateway.GatewayRef != observation.Gateway.GatewayRef || record.Gateway.BotGeneration != observation.Gateway.BotGeneration || record.Envelope.ChatType != "p2p" || record.Envelope.SenderType != "user" || record.Route.Kind != work.ScopePersonal || record.Route.ScopeID == "" || record.Route.ContextID == "" || record.Route.ReplyRoutingError != "" || (facts.ChatID != "" && facts.ChatID != record.Envelope.ChatID) || (record.State != work.IngressPending && record.State != work.IngressProcessed) {
		return nil
	}
	scope, err := t.GetScope(record.Route.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal || scope.PrincipalID != principal || scope.RoleID != observation.Gateway.RoleID {
		return nil
	}
	binding, err := t.GetRoleBotBinding(scope.RoleID)
	if err != nil {
		return err
	}
	if binding.GatewayRef != observation.Gateway.GatewayRef || binding.AppID != observation.Gateway.AppID || binding.IdentityGeneration != observation.Gateway.BotGeneration {
		return nil
	}
	nav, err := t.GetPrivateNavigation(scope.ID)
	if err != nil {
		return err
	}
	if nav.FrontContextID != record.Route.ContextID {
		return nil
	}
	action := PrivateReactionAction{ID: observation.ID, RawIngressID: record.ID, ScopeID: scope.ID, ContextID: record.Route.ContextID, PrincipalID: principal, Binding: binding, NavigationRevision: nav.Revision, Emoji: reactionEmoji(facts.Emoji), State: "waiting_source", Revision: 1}
	if err = t.freezePrivateReactionTarget(&action); err != nil {
		return nil
	}
	return t.PutPrivateReactionAction(action, 0)
}

// Bounded fair scans reuse the existing action journal. A permanent download
// cannot occupy the first page forever; checked entries rotate behind others.
func (t *Tx) resolveDeferredPrivateReactionSources(limit int) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM team_private_reaction_actions WHERE state='waiting_source' ORDER BY julianday(json_extract(payload,'$.deferred_checked_at')),id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	var actions []PrivateReactionAction
	for rows.Next() {
		var raw string
		var action PrivateReactionAction
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &action); err != nil {
			rows.Close()
			return err
		}
		actions = append(actions, action)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, action := range actions {
		if err = t.resolveDeferredPrivateReactionSource(action); err != nil {
			return err
		}
	}
	return nil
}
func (t *Tx) resolveDeferredPrivateReactionSource(action PrivateReactionAction) error {
	arrivalInputID, arrivalInputIDs := action.InputID, action.InputIDs
	before := action.Revision
	action.Revision++
	action.DeferredCheckedAt = time.Now().UTC()
	refuse := func(reason string) error {
		action.InputID, action.InputIDs = arrivalInputID, arrivalInputIDs
		action.State = "handled"
		action.LastError = reason
		return t.PutPrivateReactionAction(action, before)
	}
	// Check the frozen target before waiting. A later turn may never acquire an
	// old reaction merely because its source finished downloading later.
	if err := t.validatePrivateReactionTarget(action); err != nil {
		return refuse("original execution target changed before source became ready")
	}
	nav, err := t.GetPrivateNavigation(action.ScopeID)
	if err != nil {
		return err
	}
	if nav.Revision != action.NavigationRevision || nav.FrontContextID != action.ContextID {
		return refuse("private navigation changed before source became ready")
	}
	binding, err := t.GetRoleBotBinding(action.Binding.RoleID)
	if err != nil {
		return err
	}
	if binding != action.Binding {
		return refuse("bot identity changed before source became ready")
	}
	record, err := t.GetIngress(action.RawIngressID)
	if err != nil {
		return err
	}
	if record.State == work.IngressPending {
		return t.PutPrivateReactionAction(action, before)
	}
	if record.State != work.IngressProcessed || record.MessageID == "" || record.Route.ScopeID != action.ScopeID || record.Route.ContextID != action.ContextID {
		return refuse("original SDK message did not become runnable user input")
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM inbox WHERE scope_id=? AND context_id=? AND role_id=? AND message_id=? LIMIT 2`, action.ScopeID, action.ContextID, action.Binding.RoleID, record.MessageID)
	if err != nil {
		return err
	}
	var inputs []work.Inbox
	for rows.Next() {
		var raw string
		var input work.Inbox
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
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
	if len(inputs) != 1 {
		return refuse("original message has no unique input")
	}
	input := inputs[0]
	if input.State != work.InboxPending || input.ShellOperationID != "" || input.ControlOfInputID != "" || input.SessionGoalIntentID != "" || input.AutomationEpisodeID != "" || input.AwaitingText {
		return refuse("original input is no longer eligible for reaction delivery")
	}
	if input.ContentState == work.InputContentPending {
		return t.PutPrivateReactionAction(action, before)
	}
	observation, err := t.GetPlatformObservation(action.ID)
	if err != nil {
		return err
	}
	var facts privateReactionFacts
	if err = json.Unmarshal(observation.Facts, &facts); err != nil {
		return err
	}
	cluster, err := t.PendingInputMediaCluster(input)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return refuse("original input cluster changed")
		}
		return err
	}
	action.InputID = input.ID
	action.InputIDs = nil
	for _, member := range cluster {
		action.InputIDs = append(action.InputIDs, member.ID)
	}
	if err = t.checkPrivateReactionAction(action, observation, facts); err != nil {
		return refuse("original source or execution authority no longer matches")
	}
	action.State = "prepared"
	action.LastError = ""
	return t.PutPrivateReactionAction(action, before)
}
