package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestResourceWriteBuilderSharesLiveActivityUntilEntireCallFinishes(t *testing.T) {
	s, token, run, fake := writeServiceFixture(t)
	builder := s.WithNativeDispatcher(nil)
	entered, release := make(chan struct{}), make(chan struct{})
	fake.create = func(context.Context, work.Resource, CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
		close(entered)
		<-release
		return ResourceWriteReceipt{Attempted: true, ExternalID: "created", SourceIdentity: "docx:created", CanonicalURI: "https://a.feishu.cn/docx/created"}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := builder.ResourceCreateDocument(context.Background(), token, run, CreateResourceDocumentRequest{Key: "builder-held", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: "Doc", Purpose: "Work", CollectionID: "materials"})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("call never reached SDK boundary")
	}
	id := ""
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		ops, e := tx.ListOperations("scope")
		if e != nil {
			return e
		}
		for _, op := range ops {
			if op.Kind == "resource.document.create" {
				id = op.ID
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if id == "" || !s.ResourceWriteCallInFlight(id) || !builder.ResourceWriteCallInFlight(id) {
		t.Fatal("construction builder lost authority's actual live call token")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.ResourceWriteCallInFlight(id) || builder.ResourceWriteCallInFlight(id) {
		t.Fatal("completed lifecycle kept stale activity")
	}
}
