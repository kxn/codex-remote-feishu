package workstore

import (
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestNewSourceCaptureInvalidatesOnlyExactObsoleteMemoryAndKeepsHistory(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		resource := work.Resource{ID: "source-resource", ScopeID: "s", CollectionID: "specs", ConnectorIdentity: "feishu", SourceIdentity: "doc", CanonicalURI: "https://example.invalid/doc", Purpose: "requirements", Revision: 1}
		if e := tx.PutResource(resource, 0); e != nil {
			return e
		}
		body, e := tx.GetContent("s", "body")
		if e != nil {
			return e
		}
		now := time.Now().UTC()
		old := work.ResourceVersion{ID: "source-v1", ResourceID: resource.ID, ScopeID: "s", Kind: work.VersionImmutableSnapshot, Source: work.SourceRef{Kind: "feishu_docx", ID: "doc", Version: "1", EvidenceRef: body.ID}, ContentRef: body.ID, ContentHash: body.SHA256, ObservationRange: "document revision 1", Consistency: work.ConsistencyAtomic, Complete: true, Saved: true, StartedAt: now, CapturedAt: now}
		if e = tx.PutResourceVersion(old); e != nil {
			return e
		}
		note := memory("old-source-summary", work.MemoryScope{Kind: work.MemoryProject, ProjectID: "project"})
		note.Sources = []work.SourceRef{old.Source}
		if e = tx.PutMemory(note, 0); e != nil {
			return e
		}
		unrelated := memory("unrelated-summary", work.MemoryScope{Kind: work.MemoryProject, ProjectID: "project"})
		if e = tx.PutMemory(unrelated, 0); e != nil {
			return e
		}
		next := old
		next.ID = "source-v2"
		next.Source.Version = "2"
		next.ObservationRange = "document revision 2"
		next.StartedAt = now.Add(time.Second)
		next.CapturedAt = now.Add(2 * time.Second)
		if e = tx.PutResourceVersion(next); e != nil {
			return e
		}
		if _, _, e = tx.ReadMemory(work.ReadScope{ScopeID: "s", RoleID: "r"}, note.ID); e != ErrNotFound {
			t.Fatalf("source changed but obsolete summary stayed queryable: %v", e)
		}
		if _, _, e = tx.ReadMemory(work.ReadScope{ScopeID: "s", RoleID: "r"}, unrelated.ID); e != nil {
			return e
		}
		revived := note
		revived.ID = "revived-stale-summary"
		if e = tx.PutMemory(revived, 0); e == nil {
			t.Fatal("old source silently revived a current summary after supersession")
		}
		if _, e = tx.GetResourceVersion("s", old.ID); e != nil {
			return e
		}
		if _, e = tx.GetMessage("memory-correction:" + note.ID + ":2:c-r"); e != nil {
			return e
		}
		// A separately observed partial range must not invalidate a summary of a
		// different range, even when its source identity and later timestamp match.
		rangeVersion := old
		rangeVersion.ID = "range-a"
		rangeVersion.Source.Kind = "feishu_sheets_observation"
		rangeVersion.Source.Version = "hash-a"
		rangeVersion.ObservationRange = "sheet!A1:B2"
		if e = tx.PutResourceVersion(rangeVersion); e != nil {
			return e
		}
		rangeNote := memory("range-summary", work.MemoryScope{Kind: work.MemoryProject, ProjectID: "project"})
		rangeNote.Sources = []work.SourceRef{rangeVersion.Source}
		if e = tx.PutMemory(rangeNote, 0); e != nil {
			return e
		}
		rangeVersion.ID = "range-c"
		rangeVersion.Source.Version = "hash-c"
		rangeVersion.ObservationRange = "sheet!C1:D2"
		rangeVersion.StartedAt = now.Add(3 * time.Second)
		rangeVersion.CapturedAt = now.Add(4 * time.Second)
		if e = tx.PutResourceVersion(rangeVersion); e != nil {
			return e
		}
		if _, _, e = tx.ReadMemory(work.ReadScope{ScopeID: "s", RoleID: "r"}, rangeNote.ID); e != nil {
			return e
		}
		return nil
	})
}

func TestFeishuResourceAliasesInvalidateMemoryOnlyWithinProjectAndKeepHistory(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedOtherProject(t, s)
	requireTx(t, s, func(tx *Tx) error {
		old := work.Resource{ID: "old-bot-doc", ScopeID: "s", ConnectorIdentity: "feishu:legacy:old-app:1", SourceIdentity: "docx:shared",
			CanonicalURI: "https://a.feishu.cn/wiki/previous-alias", CollectionID: "materials", Purpose: "Project source", Revision: 1}
		canonical := old
		canonical.ID, canonical.ConnectorIdentity, canonical.CanonicalURI = "project-doc", "feishu:current:other-app:1", "https://a.feishu.cn/docx/shared"
		unrelated := canonical
		unrelated.ID, unrelated.ConnectorIdentity = "other-connector", "test:external"
		foreign := canonical
		foreign.ID, foreign.ScopeID = "other-project-doc", "s2"
		for _, resource := range []work.Resource{old, canonical, unrelated, foreign} {
			if err := tx.PutResource(resource, 0); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		capture := func(resource work.Resource, revision string, captured time.Time) (work.ResourceVersion, error) {
			body := work.NewContent(resource.ID+":body:"+revision, resource.ScopeID, "text/plain", []byte("same body across versions"), captured)
			if err := tx.PutContent(body); err != nil {
				return work.ResourceVersion{}, err
			}
			version := work.ResourceVersion{ID: resource.ID + ":" + revision, ResourceID: resource.ID, ScopeID: resource.ScopeID, Kind: work.VersionImmutableSnapshot,
				Source:     work.SourceRef{Kind: "feishu_docx", ID: resource.SourceIdentity, Version: revision, EvidenceRef: body.ID},
				ContentRef: body.ID, ContentHash: body.SHA256, ObservationRange: "document revision " + revision, Consistency: work.ConsistencyAtomic,
				Complete: true, Saved: true, StartedAt: captured, CapturedAt: captured}
			return version, tx.PutResourceVersion(version)
		}
		var previous []work.MemoryEntry
		for _, resource := range []work.Resource{old, unrelated, foreign} {
			version, err := capture(resource, "1", now)
			if err != nil {
				return err
			}
			scope, err := tx.GetScope(resource.ScopeID)
			if err != nil {
				return err
			}
			note := memory(resource.ID+"-memory", work.MemoryScope{Kind: work.MemoryProject, ProjectID: scope.ProjectID})
			note.StorageScopeID, note.ContentRef = scope.ID, version.ContentRef
			note.Sources = []work.SourceRef{version.Source}
			if err = tx.PutMemory(note, 0); err != nil {
				return err
			}
			previous = append(previous, note)
		}
		updated, err := capture(canonical, "2", now.Add(time.Second))
		if err != nil {
			return err
		}
		read := work.ReadScope{ScopeID: "s", RoleID: "r"}
		if _, _, err = tx.ReadMemory(read, previous[0].ID); err != ErrNotFound {
			return fmt.Errorf("legacy alias's obsolete memory remained visible: %v", err)
		}
		for _, note := range previous[1:] {
			if _, _, err = tx.ReadMemory(work.ReadScope{ScopeID: note.StorageScopeID, RoleID: "r"}, note.ID); err != nil {
				return fmt.Errorf("another connector or project's memory was invalidated: %w", err)
			}
		}
		stale := previous[0]
		stale.ID = "stale-alias-republication"
		if err = tx.PutMemory(stale, 0); err == nil {
			return fmt.Errorf("old alias was accepted without applying the newer observation")
		}
		stale.Sources = append(stale.Sources, updated.Source)
		if err = tx.PutMemory(stale, 0); err != nil {
			return err
		}
		versions, err := tx.ListResourceVersions("s", old.ID)
		if err != nil || len(versions) != 1 || versions[0].ResourceID != old.ID || versions[0].Source.Version != "1" {
			return fmt.Errorf("historical alias or version was rewritten: %+v %v", versions, err)
		}
		return nil
	})
}
