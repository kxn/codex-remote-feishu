package workservice

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type MemoryView struct {
	Entry   work.MemoryEntry `json:"entry"`
	Content ContentPage      `json:"content"`
	SHA256  string           `json:"sha256"`
}
type MemoryWriteRequest struct {
	Key                    string               `json:"key"`
	ID                     string               `json:"id,omitempty"`
	ExpectedRevision       int64                `json:"expected_revision"`
	Kind                   work.MemoryScopeKind `json:"kind,omitempty"`
	Content                string               `json:"content"`
	Applicability          string               `json:"applicability"`
	Sources                []work.SourceRef     `json:"sources"`
	SharingSourceMessageID string               `json:"sharing_source_message_id,omitempty"`
}
type MemoryWriteResult struct {
	Result
	Owner          work.MemoryScope `json:"owner"`
	SharingGrantID string           `json:"sharing_grant_id,omitempty"`
}
type MemoryInvalidateRequest struct {
	Key              string         `json:"key"`
	ID               string         `json:"id"`
	ExpectedRevision int64          `json:"expected_revision"`
	Source           work.SourceRef `json:"source"`
}

func (s *Service) MemoryList(ctx context.Context, token, runRef string, req KnowledgeListRequest) (KnowledgePage[work.MemoryEntry], error) {
	var result KnowledgePage[work.MemoryEntry]
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		items, err := tx.ListMemory(callerRead(c))
		if err != nil {
			return err
		}
		result, err = knowledgePage(items, req, func(v work.MemoryEntry) string { return v.ID })
		return err
	})
	return result, err
}
func (s *Service) MemoryRead(ctx context.Context, token, runRef string, req KnowledgeObjectRequest) (MemoryView, error) {
	var result MemoryView
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		entry, content, err := tx.ReadMemory(callerRead(c), req.ID)
		if err != nil {
			return err
		}
		page, err := knowledgeContentPage(content, req.Offset, req.Limit)
		if err != nil {
			return err
		}
		result = MemoryView{Entry: entry, Content: page, SHA256: content.SHA256}
		return nil
	})
	return result, err
}

func memoryOwner(scope work.WorkScope, c Caller, kind work.MemoryScopeKind) (work.MemoryScope, error) {
	if kind == "" {
		if scope.Kind == work.ScopePersonal {
			kind = work.MemoryPersonal
		} else {
			kind = work.MemoryRoleProject
		}
	}
	switch kind {
	case work.MemoryPersonal:
		if scope.Kind == work.ScopePersonal {
			return work.MemoryScope{Kind: kind, RoleID: c.Worker.RoleID, PrincipalID: scope.PrincipalID}, nil
		}
	case work.MemoryProject:
		if scope.Kind == work.ScopeProject {
			return work.MemoryScope{Kind: kind, ProjectID: scope.ProjectID}, nil
		}
	case work.MemoryRoleProject:
		if scope.Kind == work.ScopeProject {
			return work.MemoryScope{Kind: kind, ProjectID: scope.ProjectID, RoleID: c.Worker.RoleID}, nil
		}
	case work.MemoryRole:
		return work.MemoryScope{Kind: kind, RoleID: c.Worker.RoleID}, nil
	case work.MemoryTeam:
		if scope.Kind == work.ScopeProject {
			return work.MemoryScope{Kind: kind, TeamID: scope.TeamID}, nil
		}
	case work.MemoryContext:
		return work.MemoryScope{Kind: kind, ContextID: c.Worker.ContextID}, nil
	case work.MemoryTask:
		if c.Attempt.TaskID != "" {
			return work.MemoryScope{Kind: kind, TaskID: c.Attempt.TaskID}, nil
		}
	}
	return work.MemoryScope{}, workauth.ErrUnauthorized
}

func (s *Service) MemoryWrite(ctx context.Context, token, runRef string, req MemoryWriteRequest) (MemoryWriteResult, error) {
	var result MemoryWriteResult
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		owner, err := memoryOwner(scope, c, req.Kind)
		if err != nil {
			return err
		}
		result.Owner = owner
		id := req.ID
		if id == "" {
			id = stableID("memory", scope.ID, c.Worker.RoleID, req.Key)
		}
		if owner.Kind == work.MemoryRole || owner.Kind == work.MemoryTeam {
			result.SharingGrantID = stableID("share", id, strconv.FormatInt(req.ExpectedRevision+1, 10), req.SharingSourceMessageID)
		}
		result.Result, err = s.mutate(tx, c, "memory.write", req.Key, id, struct {
			Request MemoryWriteRequest
			Owner   work.MemoryScope
		}{req, owner}, func() (string, int64, error) {
			if strings.TrimSpace(req.Content) == "" || strings.TrimSpace(req.Applicability) == "" || len(req.Sources) == 0 {
				return "", 0, fmt.Errorf("memory needs content, applicability and source references")
			}
			if req.ExpectedRevision > 0 {
				old, _, err := tx.ReadMemory(callerRead(c), id)
				if err != nil {
					return "", 0, err
				}
				if old.StorageScopeID != scope.ID || old.Author != (work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}) {
					return "", 0, workauth.ErrUnauthorized
				}
			}
			revision := req.ExpectedRevision + 1
			content := work.NewContent(stableID("body", id, strconv.FormatInt(revision, 10)), scope.ID, "text/plain; charset=utf-8", []byte(req.Content), s.now())
			if err := tx.PutContent(content); err != nil {
				return "", 0, err
			}
			entry := work.MemoryEntry{ID: id, StorageScopeID: scope.ID, Scope: owner, Author: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Sources: req.Sources, ContentRef: content.ID, Applicability: req.Applicability, State: work.MemoryValid, Revision: revision, UpdatedAt: s.now()}
			if owner.Kind == work.MemoryRole || owner.Kind == work.MemoryTeam {
				message, input, err := currentHumanInstruction(tx, c, req.SharingSourceMessageID)
				if err != nil {
					return "", 0, err
				}
				grantID := stableID("share", id, strconv.FormatInt(revision, 10), message.ID)
				grant := work.SharingGrant{ID: grantID, SourceScopeID: scope.ID, ContentRef: content.ID, ContentSHA256: content.SHA256, Target: work.SharingTarget{Kind: work.SharingMemory, ObjectID: id, ObjectVersion: strconv.FormatInt(revision, 10), MemoryScope: &owner}, AuthorizedBy: message.Source, AuthorizingRunRef: c.Attempt.RunRef, SourceInboxID: input.ID, SourceMessageID: message.ID, State: work.SharingActive, Revision: 1, CreatedAt: s.now()}
				if err := tx.PutSharingGrant(grant, 0); err != nil {
					return "", 0, err
				}
				entry.SharingGrantID = grantID
				result.SharingGrantID = grantID
			} else if req.SharingSourceMessageID != "" {
				return "", 0, fmt.Errorf("sharing source requires an explicit shared role or team target")
			}
			if err := tx.PutMemory(entry, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return id, revision, nil
		})
		return err
	})
	return result, err
}
func (s *Service) MemoryInvalidate(ctx context.Context, token, runRef string, req MemoryInvalidateRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, "memory.invalidate", req.Key, req.ID, req, func() (string, int64, error) {
			if err := req.Source.Validate(); err != nil {
				return "", 0, err
			}
			entry, _, err := tx.ReadMemory(callerRead(c), req.ID)
			if err != nil {
				return "", 0, err
			}
			if entry.StorageScopeID != c.Worker.ScopeID || entry.Author != (work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}) {
				return "", 0, workauth.ErrUnauthorized
			}
			entry.State = work.MemoryInvalidated
			entry.Revision++
			entry.UpdatedAt = s.now()
			entry.Sources = append(entry.Sources, req.Source)
			if err := tx.PutMemory(entry, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return entry.ID, entry.Revision, nil
		})
		return err
	})
	return result, err
}
