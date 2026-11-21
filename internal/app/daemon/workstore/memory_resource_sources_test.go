package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
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
