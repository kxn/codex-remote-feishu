package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

type resourceWriterFake struct {
	*resourceFake
	create  func(context.Context, work.Resource, CreateResourceDocumentRequest) (ResourceWriteReceipt, error)
	inspect func(context.Context, work.Resource, InspectResourceDocumentRequest) (ResourceDocumentInspection, error)
}

func (f *resourceWriterFake) CreateResourceDocument(ctx context.Context, r work.Resource, req CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
	return f.create(ctx, r, req)
}
func (f *resourceWriterFake) InspectResourceDocument(ctx context.Context, r work.Resource, req InspectResourceDocumentRequest) (ResourceDocumentInspection, error) {
	if f.inspect != nil {
		return f.inspect(ctx, r, req)
	}
	return ResourceDocumentInspection{}, nil
}

func (f *resourceWriterFake) AppendResourceDocument(context.Context, work.Resource, AppendResourceDocumentRequest, string) (ResourceWriteReceipt, error) {
	panic("unexpected append")
}
func (f *resourceWriterFake) UpdateResourceDocument(context.Context, work.Resource, UpdateResourceDocumentRequest, string) (ResourceWriteReceipt, error) {
	panic("unexpected update")
}
func (f *resourceWriterFake) ReadResourceSheet(context.Context, work.Resource, ReadResourceSheetRequest) (ResourceSheetInspection, error) {
	return ResourceSheetInspection{}, nil
}
func (f *resourceWriterFake) WriteResourceSheet(context.Context, work.Resource, WriteResourceSheetRequest) (ResourceWriteReceipt, error) {
	panic("unexpected sheet write")
}
func writeServiceFixture(t *testing.T) (*Service, string, string, *resourceWriterFake) {
	t.Helper()
	s, token, run := knowledgeService(t)
	fake := &resourceWriterFake{resourceFake: &resourceFake{}}
	s.connectors = map[string]ResourceConnector{"leader": fake}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutResource(work.Resource{ID: "parent", ScopeID: "scope", ConnectorIdentity: fake.Identity(), SourceIdentity: "folder:actual-folder", CanonicalURI: "https://a.feishu.cn/drive/folder/actual-folder", Purpose: "Project materials", CollectionID: "materials", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	return s, token, run, fake
}
func TestResourceWriteUnknownRetainsPartialIdentityAndNeverReissues(t *testing.T) {
	s, token, run, fake := writeServiceFixture(t)
	calls := 0
	fake.create = func(ctx context.Context, parent work.Resource, req CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
		calls++
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err := s.store.Within(bounded, func(tx *workstore.Tx) error {
			ops, e := tx.ListOperations("scope")
			if e != nil {
				return e
			}
			found := false
			for _, op := range ops {
				if op.Kind == "resource.document.create" {
					found = true
					if op.State != work.OperationSending || op.RequestRef == "" {
						t.Fatal("external write started without durable intent")
					}
					content, e := tx.GetContent("scope", op.RequestRef)
					if e != nil {
						return e
					}
					var intent resourceWriteIntent
					if e = json.Unmarshal(content.Data, &intent); e != nil {
						return e
					}
					if intent.Resource.ID != "parent" || intent.Binding.RoleID != "leader" || intent.Create.Title != "Notes" {
						t.Fatalf("lost frozen request %+v", intent)
					}
				}
			}
			if !found {
				t.Fatal("missing write intent")
			}
			return nil
		})
		if err != nil {
			return ResourceWriteReceipt{}, err
		}
		return ResourceWriteReceipt{Attempted: true, ExternalID: "created-but-response-interrupted", SourceIdentity: "docx:created-but-response-interrupted", CanonicalURI: "https://a.feishu.cn/docx/created-but-response-interrupted"}, errors.New("connection ended after response headers")
	}
	req := CreateResourceDocumentRequest{Key: "once", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: "Notes", Purpose: "Working notes", CollectionID: "materials"}
	first, err := s.ResourceCreateDocument(t.Context(), token, run, req)
	if err != nil || first.Status != "pending_external" || first.Receipt.ExternalID == "" {
		t.Fatalf("partial receipt %+v %v", first, err)
	}
	// Changing registration metadata cannot turn a repeated operation into a new
	// write or erase its actual partial receipt.
	err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		r, e := tx.GetResource("scope", "parent")
		if e != nil {
			return e
		}
		r.Purpose = "Updated purpose"
		r.Revision++
		return tx.PutResource(r, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	s.connectors = nil
	replay, err := s.ResourceCreateDocument(t.Context(), token, run, req)
	if err != nil || !replay.Replay || replay.Receipt.ExternalID != first.Receipt.ExternalID || calls != 1 {
		t.Fatalf("unknown reissued/lost %+v %v calls%d", replay, err, calls)
	}
	req.Title = "different same key"
	if _, err = s.ResourceCreateDocument(t.Context(), token, run, req); !errors.Is(err, workstore.ErrIdempotencyConflict) {
		t.Fatalf("changed request accepted %v", err)
	}
	err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(first.OperationID)
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown || op.ExternalReceipt == "" || op.Steps[0].Receipt == "" {
			t.Fatalf("unknown evidence lost %+v", op)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestResourceCreateRegistersActualObjectAndPreflightCannotWriteOtherScope(t *testing.T) {
	s, token, run, fake := writeServiceFixture(t)
	calls := 0
	fake.create = func(context.Context, work.Resource, CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
		calls++
		return ResourceWriteReceipt{Attempted: true, ExternalID: "real-created-id", SourceIdentity: "docx:real-created-id", CanonicalURI: "https://a.feishu.cn/docx/real-created-id"}, nil
	}
	req := CreateResourceDocumentRequest{Key: "new", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: "Notes", Purpose: "Working notes", CollectionID: "materials"}
	result, err := s.ResourceCreateDocument(t.Context(), token, run, req)
	if err != nil || result.Status != "created" || result.ObjectID == "" || result.SnapshotSaved || result.ReadbackVerified {
		t.Fatalf("result %+v %v", result, err)
	}
	page, err := s.ResourceList(t.Context(), token, run, ResourceListRequest{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("discover created material %+v %v", page, err)
	}
	dirs, err := s.ResourceList(t.Context(), token, run, ResourceListRequest{DirectoriesOnly: true})
	if err != nil || len(dirs.Items) != 1 || dirs.Items[0].ID != "parent" {
		t.Fatalf("directory listing %+v %v", dirs, err)
	}
	replay, err := s.ResourceCreateDocument(t.Context(), token, run, req)
	if err != nil || !replay.Replay || calls != 1 {
		t.Fatal("completed creation repeated")
	}
	req.Key = "foreign"
	req.ParentResourceID = "not-in-this-scope"
	if _, err = s.ResourceCreateDocument(t.Context(), token, run, req); err == nil || calls != 1 {
		t.Fatal("unknown parent wrote external data")
	}
	req.Key = "not-directory"
	req.ParentResourceID = result.ObjectID
	if _, err = s.ResourceCreateDocument(t.Context(), token, run, req); err == nil || calls != 1 {
		t.Fatal("file used as creation directory")
	}
}

func TestResourceObserveKnownPartialObjectDoesNotRewriteUnknownHistory(t *testing.T) {
	s, token, run, fake := writeServiceFixture(t)
	writes, reads := 0, 0
	fake.create = func(context.Context, work.Resource, CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
		writes++
		return ResourceWriteReceipt{Attempted: true, ExternalID: "actual-partial-id", SourceIdentity: "docx:actual-partial-id", CanonicalURI: "https://a.feishu.cn/docx/actual-partial-id"}, errors.New("interrupted")
	}
	fake.inspect = func(ctx context.Context, r work.Resource, req InspectResourceDocumentRequest) (ResourceDocumentInspection, error) {
		reads++
		if r.SourceIdentity != "docx:actual-partial-id" {
			t.Fatalf("guessed object %s", r.SourceIdentity)
		}
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := s.store.Within(bounded, func(tx *workstore.Tx) error {
			ops, err := tx.ListOperations("scope")
			if err != nil {
				return err
			}
			found := false
			for _, op := range ops {
				if op.Kind == "resource.write.observe" && op.State == work.OperationSending {
					found = true
				}
			}
			if !found {
				t.Fatal("read started without durable observation intent")
			}
			return nil
		}); err != nil {
			return ResourceDocumentInspection{}, err
		}
		return ResourceDocumentInspection{DocumentID: "actual-partial-id", ObservedRevision: 3, RequestedRevision: 3}, nil
	}
	write, err := s.ResourceCreateDocument(t.Context(), token, run, CreateResourceDocumentRequest{Key: "partial", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: "Notes", Purpose: "Working notes", CollectionID: "materials"})
	if err != nil {
		t.Fatal(err)
	}
	req := ObserveResourceWriteRequest{Key: "read", OperationID: write.OperationID}
	observed, err := s.ResourceObserveWrite(t.Context(), token, run, req)
	if err != nil || observed.OriginalState != work.OperationUnknown || observed.Document == nil || observed.Document.DocumentID != "actual-partial-id" || observed.ObservationRef == "" {
		t.Fatalf("observation %+v %v", observed, err)
	}
	if _, err = s.ResourceObserveWrite(t.Context(), token, run, req); err != nil || writes != 1 || reads != 1 {
		t.Fatal("cached observation repeated external calls")
	}
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(write.OperationID)
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown {
			t.Fatal("readback fabricated original success")
		}
		_, e = tx.GetContent("scope", observed.ObservationRef)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	req.Key = "next-page"
	req.PageToken = "continuation"
	if _, err = s.ResourceObserveWrite(t.Context(), token, run, req); err == nil || reads != 1 {
		t.Fatal("unfrozen continuation accepted")
	}
}

func TestResourceInvalidCreationMetadataNeverStartsExternalWrite(t *testing.T) {
	s, token, run, fake := writeServiceFixture(t)
	fake.create = func(context.Context, work.Resource, CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
		t.Fatal("invalid creation metadata reached platform")
		return ResourceWriteReceipt{}, nil
	}
	for _, req := range []CreateResourceDocumentRequest{{Key: "empty-title", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: " ", Purpose: "notes", CollectionID: "materials"}, {Key: "empty-collection", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: "Notes", Purpose: "notes", CollectionID: " "}} {
		if _, err := s.ResourceCreateDocument(t.Context(), token, run, req); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}
