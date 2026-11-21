// Package workteamhome owns team-level originals. It never creates a synthetic
// project, copies project/private history, or deletes a team's source on archive.
package workteamhome

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Platform interface {
	workprovision.Platform
	ResolveHome(context.Context, string) (workprovision.HomeDocument, error)
	InitializeHome(context.Context, string, string, string, string) error
}
type Resolver func(context.Context, work.RoleBotBinding) (Platform, error)
type Service struct {
	store   *workstore.Store
	resolve Resolver
}

func New(store *workstore.Store, resolve Resolver) *Service {
	return &Service{store: store, resolve: resolve}
}
func key(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "team-materials:" + hex.EncodeToString(sum[:])
}

func (s *Service) Run(ctx context.Context) error {
	var teams []work.Team
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error { var err error; teams, err = tx.ListTeams(); return err }); err != nil {
		return err
	}
	var failures []error
	for _, team := range teams {
		if team.Materials == nil {
			continue
		}
		if err := s.RunTeam(ctx, team.ID); err != nil {
			failures = append(failures, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(failures...)
}
func (s *Service) RunTeam(ctx context.Context, id string) error {
	var team work.Team
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error { var err error; team, err = tx.GetTeam(id); return err }); err != nil {
		return err
	}
	if team.Materials == nil {
		return nil
	}
	home := *team.Materials
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding(home.Identity.RoleID)
		if err != nil {
			return err
		}
		member, err := tx.GetTeamMember(team.ID, home.Identity.RoleID)
		if err != nil {
			return err
		}
		if !member.Active {
			return fmt.Errorf("团队资料机器人已停用，请重新选择现有首页")
		}
		if binding != home.Identity {
			return fmt.Errorf("团队资料的机器人身份已变更，请由团队所有者重新选择现有首页")
		}
		return nil
	}); err != nil {
		return s.problem(ctx, team, home, "unavailable", err)
	}
	platform, err := s.resolve(ctx, home.Identity)
	if err != nil {
		return s.problem(ctx, team, home, "unavailable", err)
	}
	if home.State == "ready" {
		if _, e := platform.ReadDocument(ctx, home.DocumentID); e != nil {
			return s.problem(ctx, team, home, "unavailable", e)
		}
		return s.grantProjectViews(ctx, team, platform)
	}
	if home.Selection.Mode == "existing" {
		doc, e := platform.ResolveHome(ctx, home.Selection.URL)
		if e != nil {
			return s.problem(ctx, team, home, "unavailable", e)
		}
		home.DocumentID, home.URL = doc.ID, doc.URL
	} else {
		if home.Selection.Mode == "managed" && home.FolderID == "" {
			receipt, e := s.step(ctx, team, "folder", true, func() (receipt, error) {
				v, e := platform.CreateFolder(ctx, team.Name+" · 团队资料", "")
				return receipt{FolderID: v.ID, URL: v.URL}, e
			})
			home.FolderID = receipt.FolderID
			if e != nil {
				return s.problem(ctx, team, home, "unknown", e)
			}
		}
		if home.Selection.Mode == "drive" {
			u, _ := url.Parse(home.Selection.URL)
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			home.FolderID = parts[len(parts)-1]
			if e := platform.CheckFolder(ctx, home.FolderID); e != nil {
				return s.problem(ctx, team, home, "unavailable", e)
			}
		}
		if home.FolderID != "" {
			if e := platform.CheckFolder(ctx, home.FolderID); e != nil {
				return s.problem(ctx, team, home, "unavailable", e)
			}
		}
		if home.DocumentID == "" {
			var parent workprovision.WikiParent
			if home.Selection.Mode == "wiki" {
				parent, err = platform.ResolveWikiParent(ctx, home.Selection.URL)
				if err != nil {
					return s.problem(ctx, team, home, "unavailable", err)
				}
			}
			value, e := s.step(ctx, team, "document", true, func() (receipt, error) {
				var doc workprovision.HomeDocument
				var e error
				if home.Selection.Mode == "wiki" {
					doc, e = platform.CreateWikiDocument(ctx, parent, team.Name+" · 团队首页")
				} else {
					doc, e = platform.CreateDocument(ctx, team.Name+" · 团队首页", home.FolderID)
				}
				return receipt{DocumentID: doc.ID, URL: doc.URL}, e
			})
			home.DocumentID, home.URL = value.DocumentID, value.URL
			if e != nil {
				return s.problem(ctx, team, home, "unknown", e)
			}
		}
		if _, err = platform.ReadDocument(ctx, home.DocumentID); err != nil {
			return s.problem(ctx, team, home, "unavailable", err)
		}
		if home.URL == "" {
			home.URL, err = platform.ReadDocumentURL(ctx, home.DocumentID)
			if err != nil {
				return s.problem(ctx, team, home, "unavailable", err)
			}
		}
		if _, err = s.step(ctx, team, "intro", true, func() (receipt, error) {
			return receipt{DocumentID: home.DocumentID, URL: home.URL}, platform.InitializeHome(ctx, home.DocumentID, team.Name, home.Selection.URL, key(team.ID, fmt.Sprint(home.Generation), "intro"))
		}); err != nil {
			return s.problem(ctx, team, home, "unknown", err)
		}
	}
	home.State, home.LastError = "ready", ""
	if err = s.save(ctx, team, home); err != nil {
		return err
	}
	team.Materials = &home
	return s.grantProjectViews(ctx, team, platform)
}
func (s *Service) save(ctx context.Context, team work.Team, home work.TeamMaterials) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetTeam(team.ID)
		if err != nil {
			return err
		}
		if current.Materials == nil || current.Materials.Generation != home.Generation {
			return workstore.ErrConflict
		}
		before := current.Revision
		home.ProjectGrants = current.Materials.ProjectGrants
		current.Materials = &home
		current.Revision++
		return tx.PutTeam(current, before)
	})
}
func (s *Service) problem(ctx context.Context, team work.Team, home work.TeamMaterials, state string, cause error) error {
	home.State, home.LastError = state, cause.Error()
	if team.Materials != nil && reflect.DeepEqual(*team.Materials, home) {
		return cause
	}
	return errors.Join(cause, s.save(ctx, team, home))
}

// Permission is granted only to explicit main channels of projects that use
// this team, not to the whole tenant. Read back before any repeatable grant.
func (s *Service) grantProjectViews(ctx context.Context, team work.Team, platform Platform) error {
	var channels []work.ProjectChannelBinding
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		projects, e := tx.ListProjects()
		if e != nil {
			return e
		}
		for _, p := range projects {
			if p.TeamID != team.ID || p.State == work.ProjectArchived || p.State == work.ProjectArchiving {
				continue
			}
			bindings, e := tx.ListProjectChannelBindings(p.ID)
			if e != nil {
				return e
			}
			for _, b := range bindings {
				if b.Active && b.Purpose == "main" {
					allowed, e := tx.TeamMaterialsProjectAllowed(team, b)
					if e != nil {
						return e
					}
					if allowed {
						channels = append(channels, b)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, b := range channels {
		grant := team.Materials.ProjectGrants[b.ProjectID]
		stage := "grant:" + key(b.ProjectID, b.Tenant, b.ChatID, fmt.Sprint(b.BindingGeneration), grant.Source.Kind, grant.Source.ID, grant.Source.Version)
		allowed, readErr := platform.HasGroupView(ctx, team.Materials.DocumentID, b.ChatID)
		if readErr != nil {
			failures = append(failures, readErr)
			continue
		}
		if !allowed {
			var confirmed bool
			readErr = s.store.Within(ctx, func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(key(team.ID, fmt.Sprint(team.Materials.Generation), stage))
				if errors.Is(e, workstore.ErrNotFound) {
					return nil
				}
				if e != nil {
					return e
				}
				confirmed = op.State == work.OperationConfirmed
				return nil
			})
			if readErr != nil {
				failures = append(failures, readErr)
				continue
			}
			if confirmed {
				failures = append(failures, fmt.Errorf("团队首页的已确认群访问已改变，请由团队所有者明确重新连接当前项目群访问"))
				continue
			}
		}
		_, e := s.step(ctx, team, stage, false, func() (receipt, error) {
			if e := s.store.Within(ctx, func(tx *workstore.Tx) error {
				current, e := tx.GetProjectChannelBinding(b.Tenant, b.ChatID)
				if e != nil {
					return e
				}
				if current != b {
					return workstore.ErrConflict
				}
				fresh, e := tx.GetTeam(team.ID)
				if e != nil {
					return e
				}
				if fresh.Materials == nil || fresh.Materials.Generation != team.Materials.Generation {
					return workstore.ErrConflict
				}
				p, e := tx.GetProject(b.ProjectID)
				if e != nil {
					return e
				}
				if p.State == work.ProjectArchived || p.State == work.ProjectArchiving {
					return workstore.ErrScopeDenied
				}
				allowed, e := tx.TeamMaterialsProjectAllowed(fresh, b)
				if e != nil {
					return e
				}
				if !allowed {
					return workstore.ErrScopeDenied
				}
				return nil
			}); e != nil {
				return receipt{}, e
			}
			ok, e := platform.HasGroupView(ctx, team.Materials.DocumentID, b.ChatID)
			if e != nil {
				return receipt{}, e
			}
			if !ok {
				if e = platform.EnsureGroupView(ctx, team.Materials.DocumentID, b.ChatID); e != nil {
					return receipt{}, e
				}
				ok, e = platform.HasGroupView(ctx, team.Materials.DocumentID, b.ChatID)
				if e != nil {
					return receipt{}, e
				}
				if !ok {
					return receipt{}, fmt.Errorf("团队首页群访问尚未确认")
				}
			}
			return receipt{DocumentID: team.Materials.DocumentID, URL: team.Materials.URL}, nil
		})
		if e != nil {
			failures = append(failures, e)
		}
	}
	problem := errors.Join(failures...)
	home := *team.Materials
	home.LastError = ""
	if problem != nil {
		home.LastError = "团队首页已保留，部分项目群的访问仍待连接：" + problem.Error()
	}
	if home.LastError != team.Materials.LastError {
		if e := s.save(ctx, team, home); e != nil {
			problem = errors.Join(problem, e)
		}
	}
	return problem
}

var now = func() time.Time { return time.Now().UTC() }
