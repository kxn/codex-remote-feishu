package workservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type projectACLFixture struct {
	*resourceWriterFake
	read  func(context.Context) (string, error)
	grant func() error
}

func (f *projectACLFixture) ProjectDocumentGroupPermission(ctx context.Context, _ feishu.ProjectDocumentID, _ string) (string, error) {
	return f.read(ctx)
}

func (f *projectACLFixture) GrantProjectDocumentGroupView(context.Context, feishu.ProjectDocumentID, string) error {
	return f.grant()
}

func projectACLServiceFixture(t *testing.T) (*Service, string, string, projectDocumentAccessTarget, *projectACLFixture) {
	t.Helper()
	s, token, run := lifecycleProjectService(t, false, "restoring-member")
	gateway := &projectACLFixture{resourceWriterFake: &resourceWriterFake{resourceFake: &resourceFake{}}}
	gateway.create = func(context.Context, work.Resource, CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
		return ResourceWriteReceipt{Attempted: true, ExternalID: "retained-doc", SourceIdentity: "docx:retained-doc", CanonicalURI: "https://a.feishu.cn/docx/retained-doc"}, nil
	}
	s.connectors = map[string]ResourceConnector{"leader": gateway}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutResource(work.Resource{ID: "project-directory", ScopeID: "project-scope", ConnectorIdentity: gateway.Identity(), SourceIdentity: "folder:directory", CanonicalURI: "https://a.feishu.cn/drive/folder/directory", Purpose: "Project directory", CollectionID: "materials", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	created, err := s.ResourceCreateDocument(t.Context(), token, run, CreateResourceDocumentRequest{Key: "create-project-acl-doc", ParentResourceID: "project-directory", ExpectedParentRevision: 1, Title: "Retained document", Purpose: "Retained document", CollectionID: "materials"})
	if err != nil || created.Status != "created" {
		t.Fatalf("create = %+v, %v", created, err)
	}
	var target projectDocumentAccessTarget
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		resource, err := tx.GetResource("project-scope", created.ObjectID)
		if err != nil {
			return err
		}
		target, err = s.projectDocumentAccessTarget(t.Context(), tx, resource, MessageProjection{Tenant: "tenant", ChatID: "group", ChannelGeneration: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return s, token, run, target, gateway
}

func TestProjectDocumentRestoreRetryRequiresStillCurrentMembership(t *testing.T) {
	s, token, run, target, gateway := projectACLServiceFixture(t)
	ctx := t.Context()
	const sourceID = "restoring-member"
	gateway.read = func(context.Context) (string, error) { return "", nil }
	grants := 0
	gateway.grant = func() error {
		grants++
		return &feishu.APIError{API: "drive.v1.permission_member.create", Code: 1, StatusCode: 400}
	}
	if err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, caller Caller) error {
		message, err := s.authorizeProjectDocumentRestore(tx, caller, target.intent.Channel, sourceID)
		if err != nil {
			return err
		}
		intent := target.intent
		source := humanSource(message)
		intent.RestoreSource = &source
		op, err := s.prepareProjectDocumentShare(tx, target, caller, intent, target.base+":restore:fixture")
		if err != nil {
			return err
		}
		op.State, op.Revision = work.OperationSending, 2
		if _, _, err = tx.PutOperation(op, 1); err != nil {
			return err
		}
		op.State, op.Revision = work.OperationRetryable, 3
		_, _, err = tx.PutOperation(op, 2)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	req := ShareProjectDocumentRequest{ResourceID: target.resource.ID, SourceMessageID: sourceID}
	valid, err := s.ShareProjectDocument(ctx, token, run, req)
	if err != nil || valid.Status != "project_access_retryable" || grants != 1 {
		t.Fatalf("valid retry = %+v, %v, grants=%d", valid, err, grants)
	}
	expired := s.now().Add(2 * time.Hour)
	s.now = func() time.Time { return expired }
	if _, err := s.ShareProjectDocument(ctx, token, run, req); !errors.Is(err, workstore.ErrScopeDenied) || grants != 1 {
		t.Fatalf("expired human membership allowed a grant call: %v, grants=%d", err, grants)
	}
}

func TestProjectDocumentAccessLateAllowanceCannotOverrideNewerRevocation(t *testing.T) {
	s, _, _, target, gateway := projectACLServiceFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int64
	gateway.read = func(ctx context.Context) (string, error) {
		if calls.Add(1) != 1 {
			return "", nil
		}
		close(started)
		select {
		case <-release:
			return "view", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	var delayed = make(chan error, 1)
	go func() {
		_, err := s.observeProjectDocumentAccess(ctx, target)
		delayed <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if permission, err := s.observeProjectDocumentAccess(ctx, target); err != nil || permission != "" {
		t.Fatalf("newer revocation = %q, %v", permission, err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-delayed:
		var superseded projectDocumentPermissionReadError
		if !errors.As(err, &superseded) {
			t.Fatalf("late result was returned as current: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		latest, err := tx.LatestProjectDocumentAccessObservation(target.resource.ScopeID, target.base)
		if err != nil {
			return err
		}
		if latest.TargetGeneration != 2 {
			t.Fatalf("expected second started read to own the projection: %+v", latest)
		}
		managed, confirmed, err := tx.CurrentProjectDocumentAccess(target.resource, "tenant", "group", 1)
		if err == nil && (!managed || confirmed) {
			t.Fatalf("late allow overrode fresher revoked observation: managed=%v, confirmed=%v", managed, confirmed)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
