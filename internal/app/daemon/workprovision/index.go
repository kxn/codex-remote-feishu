package workprovision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func stableID(prefix string, parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return prefix + "_" + hex.EncodeToString(sum[:])
}
func (s *Service) index(ctx context.Context, state snapshot) (HomeIndex, error) {
	result := HomeIndex{UpdatedAt: s.now(), SyncState: "已同步"}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		project, err := tx.GetProject(state.project.ID)
		if err != nil {
			return err
		}
		result.ProjectName = project.Name
		result.ProjectState = project.State
		members, err := tx.ListProjectMembers(project.ID)
		if err != nil {
			return err
		}
		for _, member := range members {
			if member.State == work.ProjectMemberRemoved {
				continue
			}
			result.TotalMembers++
			if member.State == work.ProjectMemberReady {
				result.ReadyMembers++
			}
		}
		tasks, err := tx.ListTasks(project.ScopeID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			switch task.State {
			case work.TaskTodo:
				result.PendingTasks++
			case work.TaskActive, work.TaskWaiting, work.TaskChecking:
				result.ActiveTasks++
			case work.TaskDone:
				result.DoneTasks++
			}
		}
		resources, err := tx.ListResources(project.ScopeID)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			if resource.ID != state.home.ResourceID {
				result.Resources = append(result.Resources, HomeResource{Label: resource.Purpose, URL: resource.CanonicalURI})
			}
		}
		deliveries, err := tx.ListDeliveries(project.ScopeID)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			item := HomeDelivery{Summary: delivery.Summary, NeededAction: delivery.NeededAction}
			for _, ref := range delivery.References {
				resource, err := tx.GetResource(project.ScopeID, ref.ResourceID)
				if err != nil {
					return err
				}
				version, err := tx.GetResourceVersion(project.ScopeID, ref.VersionID)
				if err != nil {
					return err
				}
				item.Resources = append(item.Resources, HomeResource{Label: resource.Purpose, URL: resource.CanonicalURI, Version: version.Source.Version})
			}
			result.Deliveries = append(result.Deliveries, item)
		}
		return nil
	})
	return result, err
}
func (s *Service) initializeIndex(ctx context.Context, state *snapshot, platform Platform) error {
	if state.home.SyncState == "synced" && state.home.IndexBaseline != "" {
		return nil
	}
	desired, err := s.index(ctx, *state)
	if err != nil {
		return err
	}
	step, appendErr := s.step(ctx, *state, "materials.index", state.home.Identity, desired, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var index HomeIndex
		if err := json.Unmarshal(intent, &index); err != nil {
			return effect{}, err
		}
		ids, err := platform.AppendHomeIndex(ctx, state.home.DocumentID, index, uuidFor("index", state.operation.ID))
		return effect{ID: ids.IndexID, Version: strconv.Itoa(ids.Revision), Data: ids}, err
	})
	ids, parseErr := decodeStep[HomeBlockIDs](step)
	if parseErr != nil {
		if appendErr != nil {
			return appendErr
		}
		return parseErr
	}
	if ids.IndexID != "" || ids.StatusID != "" {
		if err = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) {
			if ids.IndexID != "" {
				home.IndexBlockID = ids.IndexID
			}
			if ids.StatusID != "" {
				home.StatusBlockID = ids.StatusID
			}
		}); err != nil {
			return err
		}
	}
	if ids.IndexID == "" || ids.StatusID == "" {
		return fmt.Errorf("托管索引块创建结果未知；须按实际 block IDs 恢复，不按相同文字认领：%w", appendErr)
	}
	actual, err := platform.ReadHomeBlocks(ctx, state.home.DocumentID, ids.IndexID, ids.StatusID)
	if err != nil {
		return err
	}
	var frozen HomeIndex
	if err = json.Unmarshal(step.Intent, &frozen); err != nil {
		return err
	}
	text, status := renderIndex(frozen)
	if actual.Index.Text != text || actual.Status.Text != status {
		_ = s.homeUpdate(ctx, state, func(home *workstore.ProjectHome) { home.SyncState = "conflict" })
		return fmt.Errorf("首页托管块已被人工修改，保留现场并等待明确恢复")
	}
	if appendErr != nil {
		step, err = s.confirmKnown(ctx, *state, step, effect{ID: ids.IndexID, Version: strconv.Itoa(actual.Revision), Data: ids})
		if err != nil {
			return err
		}
	}
	return s.persistIndex(ctx, state, platform, actual)
}
func (s *Service) persistIndex(ctx context.Context, state *snapshot, platform Platform, actual HomeBlocks) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, *state); err != nil {
			return err
		}
		binding, err := tx.GetRoleBotBinding(state.home.Identity.RoleID)
		if err != nil {
			return err
		}
		if binding != state.home.Identity {
			return ErrStale
		}
		home, err := tx.GetProjectHome(state.project.ID)
		if err != nil {
			return err
		}
		if err = registerMaterialParent(tx, home, platform.ConnectorIdentity()); err != nil {
			return err
		}
		identity := "docx:" + home.DocumentID
		resource := work.Resource{ID: stableID("home-resource", home.ID), ScopeID: home.ScopeID, CollectionID: "project-home", ConnectorIdentity: platform.ConnectorIdentity(), SourceIdentity: identity, CanonicalURI: home.URL, Purpose: "项目固定首页", Revision: 1}
		resources, err := tx.ListResources(home.ScopeID)
		if err != nil {
			return err
		}
		found := false
		for _, existing := range resources {
			if existing.ConnectorIdentity == resource.ConnectorIdentity && existing.SourceIdentity == identity {
				resource = existing
				found = true
				break
			}
		}
		if !found {
			if err = tx.PutResource(resource, 0); err != nil {
				return err
			}
		} else if err = tx.AddResourceCollection(home.ScopeID, resource.ID, "project-home"); err != nil {
			return err
		}
		data, err := json.Marshal(actual)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		content := work.NewContent(stableID("home-snapshot", home.ID, hex.EncodeToString(sum[:])), home.ScopeID, "application/json", data, s.now())
		if err = tx.PutContent(content); err != nil {
			return err
		}
		version := work.ResourceVersion{ID: stableID("home-version", resource.ID, content.SHA256), ResourceID: resource.ID, ScopeID: home.ScopeID, Kind: work.VersionImmutableSnapshot, Source: work.SourceRef{Kind: "feishu_docx", ID: identity, Version: strconv.Itoa(actual.Revision), EvidenceRef: content.ID}, ContentRef: content.ID, ContentHash: content.SHA256, ObservationRange: "managed blocks:" + actual.Index.ID + "," + actual.Status.ID, Consistency: work.ConsistencyAtomic, Complete: true, Saved: true, StartedAt: s.now(), CapturedAt: s.now()}
		if err = tx.PutResourceVersion(version); err != nil {
			return err
		}
		previous := home.Revision
		home.Revision++
		home.ResourceID = resource.ID
		home.DocumentRevision = actual.Revision
		home.IndexBaseline = actual.Index.Fingerprint
		home.StatusBaseline = actual.Status.Fingerprint
		home.SyncState = "synced"
		home.UpdatedAt = s.now()
		if err = tx.PutProjectHome(home, previous); err != nil {
			return err
		}
		state.home = home
		return nil
	})
}
