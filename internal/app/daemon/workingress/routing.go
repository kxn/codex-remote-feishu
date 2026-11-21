package workingress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func opaque(prefix string, values ...string) string {
	data, _ := json.Marshal(values)
	sum := sha256.Sum256(data)
	return prefix + ":" + hex.EncodeToString(sum[:])
}
func (i *Ingress) resolveScope(tx *workstore.Tx, record work.IngressRecord, principal work.Principal, leader string) (work.WorkScope, error) {
	env := record.Envelope
	if record.Route.Kind == work.ScopeProject {
		scope, err := tx.GetScope(record.Route.ScopeID)
		if err != nil {
			return scope, err
		}
		if scope.Kind != work.ScopeProject || scope.ProjectID != record.Route.ProjectID {
			return scope, workstore.ErrScopeDenied
		}
		return scope, nil
	}
	if record.Route.Kind == work.ScopePersonal {
		if record.Route.ScopeID != "" {
			scope, e := tx.GetScope(record.Route.ScopeID)
			if e != nil {
				return scope, e
			}
			if scope.Kind != work.ScopePersonal || scope.PrincipalID != principal.ID || scope.RoleID != record.Gateway.RoleID {
				return scope, workstore.ErrScopeDenied
			}
			return scope, nil
		}
		scope, err := tx.FindPersonalScope(principal.ID, record.Gateway.RoleID)
		if err == nil {
			return scope, nil
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return scope, err
		}
		scope = work.WorkScope{ID: opaque("personal", principal.ID, record.Gateway.RoleID), Kind: work.ScopePersonal, PrincipalID: principal.ID, RoleID: record.Gateway.RoleID, Revision: 1, CreatedAt: i.config.Now().UTC()}
		err = tx.PutScope(scope, 0)
		return scope, err
	}
	scope, err := tx.FindBootstrapScope(env.Tenant, env.ChatID, record.Route.BindingGeneration)
	if err == nil {
		return scope, nil
	}
	if !errors.Is(err, workstore.ErrNotFound) {
		return scope, err
	}
	if leader == "" {
		return scope, fmt.Errorf("no unambiguous configured leader for project establishment")
	}
	if _, err = tx.GetRoleBotBinding(leader); err != nil {
		return scope, err
	}
	first, err := tx.FindFirstEstablishmentIngress(env.Tenant, env.ChatID, record.Route.BindingGeneration)
	if err != nil {
		return scope, err
	}
	owner := principal
	if first.ID != record.ID {
		owner, _, err = tx.ResolvePrincipal(work.IdentityEvidence{Tenant: first.Envelope.Tenant, AppID: first.Envelope.AppID, Generation: first.Gateway.BotGeneration, UnionDomain: first.Gateway.UnionDomain, IDs: first.Envelope.SenderIDs, ObservationRef: first.ID})
		if err != nil {
			return scope, err
		}
	}
	operationID := opaque("bootstrap", first.ID)
	scope = work.WorkScope{ID: opaque("bootstrap-scope", operationID), Kind: work.ScopeBootstrap, RoleID: leader, PrincipalID: owner.ID, Tenant: env.Tenant, ChatID: env.ChatID, BootstrapOperationID: operationID, BindingGeneration: record.Route.BindingGeneration, Revision: 1, CreatedAt: i.config.Now().UTC()}
	if err = tx.PutScope(scope, 0); err != nil {
		return scope, err
	}
	data, _ := json.Marshal(first.Envelope)
	content := work.NewContent(opaque("bootstrap-request", first.ID), scope.ID, "application/json", data, i.config.Now().UTC())
	if err = tx.PutContent(content); err != nil {
		return scope, err
	}
	operation := work.Operation{ID: operationID, ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: owner.ID}, Kind: "project.bootstrap", TargetID: env.ChatID, TargetGeneration: scope.BindingGeneration + 1, IdempotencyKey: operationID, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: i.config.Now().UTC(), UpdatedAt: i.config.Now().UTC()}
	_, _, err = tx.PutOperation(operation, 0)
	if err == nil {
		err = tx.WakeEstablishmentIngress(env.Tenant, env.ChatID, record.Route.BindingGeneration, record.ID)
	}
	return scope, err
}

type routeDecision struct {
	RoleID        string
	TaskID        string
	RelatedTaskID string
	RootMessageID string
	Note          string
}

func (i *Ingress) recipient(tx *workstore.Tx, scope work.WorkScope, env work.RawIngressEnvelope, mentions []string) (routeDecision, error) {
	result := routeDecision{RoleID: scope.RoleID, RootMessageID: env.MessageID}
	if env.RootID != "" {
		result.RootMessageID = env.RootID
	}
	if scope.Kind != work.ScopeProject {
		return result, nil
	}
	project, err := tx.GetProject(scope.ProjectID)
	if err != nil {
		return result, err
	}
	result.RoleID = project.LeaderRoleID
	var mapped *work.PlatformMessageMapping
	for _, id := range []string{env.ParentID, env.RootID, env.ThreadID} {
		if id == "" {
			continue
		}
		value, e := tx.GetPlatformMessageMapping(env.Tenant, env.ChatID, id)
		if errors.Is(e, workstore.ErrNotFound) {
			continue
		}
		if e != nil {
			return result, e
		}
		if value.ScopeID != scope.ID {
			result.Note = "引用话题属于另一个项目；当前未采纳其上下文，请先明确归属。"
			break
		}
		mapped = &value
		result.RootMessageID = value.RootMessageID
		break
	}
	if mapped != nil && mapped.TaskID == "" {
		conversation, e := tx.FindTaskConversationByRoot(scope.ID, env.Tenant, env.ChatID, mapped.RootMessageID)
		if e == nil {
			mapped.TaskID = conversation.TaskID
		} else if errors.Is(e, workstore.ErrAmbiguousConversation) {
			result.Note = "话题关联多个独立任务，请负责人明确协调对象。"
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return result, e
		}
	}
	if mapped != nil && mapped.TaskID != "" {
		task, e := tx.GetTask(mapped.TaskID)
		if e != nil {
			return result, e
		}
		result.RoleID = task.OwnerRoleID
		if task.State == work.TaskDone || task.State == work.TaskCancelled || task.State == work.TaskWaiting {
			result.RelatedTaskID = task.ID
			result.Note = "这是既有任务的协调或后续输入，不自动解除依赖或重开已结束任务。"
		} else {
			result.TaskID = task.ID
		}
	}
	unique := map[string]bool{}
	for _, role := range mentions {
		if role != "" {
			unique[role] = true
		}
	}
	if len(unique) > 1 {
		result.RoleID = project.LeaderRoleID
		result.coordinateTask()
		result.Note = "消息提及多个同事，请负责人明确协调对象。"
	} else {
		for role := range unique {
			eligible, e := recipientIsMember(tx, scope, role)
			if e != nil {
				return result, e
			}
			if !eligible {
				result.Note = "提及的同事尚未加入当前项目，请负责人明确安排。"
				result.RoleID = project.LeaderRoleID
				result.coordinateTask()
				break
			}
			if result.TaskID != "" && role != result.RoleID {
				result.coordinateTask()
			}
			result.RoleID = role
		}
	}
	eligible, err := recipientIsMember(tx, scope, result.RoleID)
	if err != nil {
		return result, err
	}
	if !eligible {
		leaderEligible, e := recipientIsMember(tx, scope, project.LeaderRoleID)
		if e != nil {
			return result, e
		}
		if !leaderEligible {
			return result, fmt.Errorf("project has no eligible leader to coordinate this input")
		}
		result.coordinateTask()
		result.RoleID = project.LeaderRoleID
		result.Note = "原任务负责人已退出项目或停用团队身份；此输入交给当前负责人协调，保留原任务关联，不自动重新指派或重开任务。"
	}
	return result, nil
}

type LeaderSelectionError struct {
	Candidates []string
	Cause      error
}

func (e *LeaderSelectionError) Error() string {
	return "需要明确选择本群负责人：" + e.Cause.Error()
}
func (i *Ingress) directoryTargets(ctx context.Context, record work.IngressRecord) (string, []string, error) {
	roles := []string{}
	unique := map[string]bool{}
	for _, mention := range record.Envelope.Mentions {
		if mention.Type != "bot" && mention.Type != "app" {
			continue
		}
		role, e := i.config.Directory.ResolveMention(ctx, record.Gateway, mention)
		if e != nil {
			return "", nil, e
		}
		if role != "" && !unique[role] {
			roles = append(roles, role)
			unique[role] = true
		}
	}
	if record.Route.Kind != work.ScopeBootstrap {
		return "", roles, nil
	}
	var existing work.WorkScope
	lookup := i.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		existing, e = tx.FindBootstrapScope(record.Envelope.Tenant, record.Envelope.ChatID, record.Route.BindingGeneration)
		return e
	})
	if lookup == nil {
		return existing.RoleID, roles, nil
	}
	if !errors.Is(lookup, workstore.ErrNotFound) {
		return "", nil, lookup
	}
	var selection workstore.BootstrapLeaderSelection
	selectionErr := i.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		selection, e = tx.GetBootstrapLeaderSelection(record.Envelope.Tenant, record.Envelope.ChatID, record.Route.BindingGeneration)
		return e
	})
	if selectionErr == nil {
		return selection.RoleID, roles, nil
	}
	if !errors.Is(selectionErr, workstore.ErrNotFound) {
		return "", nil, selectionErr
	}
	if len(roles) == 1 {
		return roles[0], roles, nil
	}
	leader, err := i.config.Directory.ResolveLeader(ctx, record.Envelope.Tenant, record.Envelope.ChatID, record.Gateway.RoleID)
	if err != nil || leader == "" {
		if err == nil {
			err = fmt.Errorf("没有唯一已选负责人")
		}
		return "", roles, &LeaderSelectionError{Candidates: roles, Cause: err}
	}
	return leader, roles, nil
}
