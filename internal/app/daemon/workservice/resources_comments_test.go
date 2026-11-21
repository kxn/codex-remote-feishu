package workservice

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResourceCommentsUsesActualBrokerPagesAndPreservesUnmappedQuote(t *testing.T) {
	calls := 0
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Query().Get("file_type") != "docx" || r.URL.Query().Get("user_id_type") != "open_id" || r.URL.Query().Get("page_size") != "2" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		switch r.URL.Path {
		case "/open-apis/drive/v1/files/document/comments":
			if r.URL.Query().Get("page_token") != "next-comments" {
				t.Error("comment cursor lost")
			}
			fmt.Fprint(w, `{"code":0,"data":{"has_more":true,"page_token":"more-comments","items":[{"comment_id":"comment","user_id":"actual-app-open-id","is_solved":true,"quote":"original text now deleted","has_more":true,"page_token":"next-replies"}]}}`)
		case "/open-apis/drive/v1/files/document/comments/comment/replies":
			if r.URL.Query().Get("page_token") != "next-replies" {
				t.Error("reply cursor lost")
			}
			fmt.Fprint(w, `{"code":0,"data":{"has_more":false,"items":[{"reply_id":"reply","user_id":"actual-app-open-id","content":{"elements":[{"type":"text_run","text_run":{"text":"Please keep the original condition"}}]}}]}}`)
		default:
			t.Error(r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	})
	resource := work.Resource{SourceIdentity: "docx:document"}
	page, err := connector.ReadResourceComments(t.Context(), resource, ReadResourceCommentsRequest{PageToken: "next-comments", PageSize: 2})
	if err != nil || !page.HasMore || page.NextPageToken != "more-comments" || len(page.Comments) != 1 || page.Comments[0].Quote == nil || *page.Comments[0].Quote != "original text now deleted" || page.Comments[0].HasMore == nil || !*page.Comments[0].HasMore {
		t.Fatalf("comment facts %+v %v", page, err)
	}
	replies, err := connector.ReadResourceComments(t.Context(), resource, ReadResourceCommentsRequest{CommentID: "comment", PageToken: "next-replies", PageSize: 2})
	if err != nil || replies.HasMore || len(replies.Replies) != 1 {
		t.Fatalf("reply page %+v %v", replies, err)
	}
	if _, err = connector.ReadResourceComments(t.Context(), work.Resource{SourceIdentity: "base:not-supported"}, ReadResourceCommentsRequest{}); err == nil || calls != 2 {
		t.Fatal("unsupported comments guessed an API")
	}
}

type resourceCommentsFake struct {
	*resourceFake
	read func(context.Context, work.Resource, ReadResourceCommentsRequest) (feishu.ProjectDocumentCommentsPage, error)
}

func (f *resourceCommentsFake) ReadResourceComments(ctx context.Context, r work.Resource, req ReadResourceCommentsRequest) (feishu.ProjectDocumentCommentsPage, error) {
	return f.read(ctx, r, req)
}
func TestResourceCommentsPersistsPageWithoutInventingAcceptedHumanDecision(t *testing.T) {
	s, token, run := knowledgeService(t)
	calls := 0
	fake := &resourceCommentsFake{resourceFake: &resourceFake{}}
	fake.read = func(ctx context.Context, r work.Resource, req ReadResourceCommentsRequest) (feishu.ProjectDocumentCommentsPage, error) {
		calls++
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := s.store.Within(bounded, func(tx *workstore.Tx) error {
			ops, e := tx.ListOperations("scope")
			if e != nil {
				return e
			}
			found := false
			for _, op := range ops {
				found = found || op.Kind == "resource.comments.read" && op.State == work.OperationSending
			}
			if !found {
				t.Fatal("comments read not journaled before network")
			}
			return nil
		}); err != nil {
			return feishu.ProjectDocumentCommentsPage{}, err
		}
		return feishu.ProjectDocumentCommentsPage{FileToken: "document", FileType: "docx", HasMore: true, NextPageToken: "more", RequestID: "actual-read"}, nil
	}
	s.connectors = map[string]ResourceConnector{"leader": fake}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutResource(work.Resource{ID: "doc", ScopeID: "scope", ConnectorIdentity: fake.Identity(), SourceIdentity: "docx:document", CanonicalURI: "https://a.feishu.cn/docx/document", Purpose: "Review", CollectionID: "materials", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	req := ReadResourceCommentsRequest{Key: "read", ResourceID: "doc"}
	observed, err := s.ResourceReadComments(t.Context(), token, run, req)
	if err != nil || observed.ObservationRef == "" || observed.Consistency != work.ConsistencyObserved {
		t.Fatalf("observation %+v %v", observed, err)
	}
	if _, err = s.ResourceReadComments(t.Context(), token, run, req); err != nil || calls != 1 {
		t.Fatal("same read key reissued")
	}
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		body, e := tx.GetContent("scope", observed.ObservationRef)
		if e != nil {
			return e
		}
		if !strings.Contains(string(body.Data), "actual-read") {
			t.Fatal("actual SDK evidence not persisted")
		}
		decisions, e := tx.ListDecisionRequests("scope")
		if e != nil {
			return e
		}
		if len(decisions) != 0 {
			t.Fatal("comment observation invented a business decision")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	req.Key = "foreign"
	req.ResourceID = "other-scope"
	if _, err = s.ResourceReadComments(t.Context(), token, run, req); err == nil || calls != 1 {
		t.Fatal("comments crossed resource scope")
	}
}
