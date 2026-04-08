package issuedocsync

import (
	"context"
	"strings"
	"testing"
)

type fakeRunner struct {
	payloads map[string]string
}

func (f fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	cursor := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "after=") {
			cursor = strings.TrimPrefix(arg, "after=")
		}
	}
	return []byte(f.payloads[cursor]), nil
}

func TestListClosedIssueSummariesPaginates(t *testing.T) {
	client := &ghCLI{
		runner: fakeRunner{
			payloads: map[string]string{
				"": `{
  "data": {
    "repository": {
      "issues": {
        "pageInfo": {"hasNextPage": true, "endCursor": "cursor-1"},
        "nodes": [
          {"number": 40, "title": "first", "updatedAt": "2026-04-08T04:00:00Z", "closedAt": "2026-04-08T03:00:00Z", "url": "https://example.com/40"}
        ]
      }
    }
  }
}`,
				"cursor-1": `{
  "data": {
    "repository": {
      "issues": {
        "pageInfo": {"hasNextPage": false, "endCursor": ""},
        "nodes": [
          {"number": 39, "title": "second", "updatedAt": "2026-04-08T02:00:00Z", "closedAt": "2026-04-08T01:00:00Z", "url": "https://example.com/39"}
        ]
      }
    }
  }
}`,
			},
		},
	}

	summaries, err := client.ListClosedIssueSummaries(context.Background(), Repo{Owner: "kxn", Name: "codex-remote-feishu"})
	if err != nil {
		t.Fatalf("ListClosedIssueSummaries error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summary count = %d, want 2", len(summaries))
	}
	if summaries[0].Number != 40 || summaries[1].Number != 39 {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
}

func TestFetchIssueDetailsPaginatesComments(t *testing.T) {
	client := &ghCLI{
		runner: fakeRunner{
			payloads: map[string]string{
				"": `{
  "data": {
    "repository": {
      "issue": {
        "number": 22,
        "title": "Headless instance 改用 pool 管理",
        "body": "issue body",
        "updatedAt": "2026-04-08T02:29:31Z",
        "closedAt": "2026-04-08T02:29:31Z",
        "url": "https://example.com/22",
        "labels": {
          "nodes": [{"name": "enhancement"}, {"name": "area:daemon"}]
        },
        "comments": {
          "pageInfo": {"hasNextPage": true, "endCursor": "cursor-1"},
          "nodes": [
            {
              "body": "first comment",
              "publishedAt": "2026-04-07T12:54:29Z",
              "updatedAt": "2026-04-07T12:54:29Z",
              "url": "https://example.com/comment-1",
              "author": {"login": "kxn"}
            }
          ]
        }
      }
    }
  }
}`,
				"cursor-1": `{
  "data": {
    "repository": {
      "issue": {
        "number": 22,
        "title": "Headless instance 改用 pool 管理",
        "body": "issue body",
        "updatedAt": "2026-04-08T02:29:31Z",
        "closedAt": "2026-04-08T02:29:31Z",
        "url": "https://example.com/22",
        "labels": {
          "nodes": [{"name": "enhancement"}, {"name": "area:daemon"}]
        },
        "comments": {
          "pageInfo": {"hasNextPage": false, "endCursor": ""},
          "nodes": [
            {
              "body": "second comment",
              "publishedAt": "2026-04-08T02:29:22Z",
              "updatedAt": "2026-04-08T02:29:22Z",
              "url": "https://example.com/comment-2",
              "author": {"login": "kxn"}
            }
          ]
        }
      }
    }
  }
}`,
			},
		},
	}

	details, err := client.FetchIssueDetails(context.Background(), Repo{Owner: "kxn", Name: "codex-remote-feishu"}, 22)
	if err != nil {
		t.Fatalf("FetchIssueDetails error = %v", err)
	}
	if details.Number != 22 || len(details.Comments) != 2 {
		t.Fatalf("unexpected details: %#v", details)
	}
	if details.Labels[0] != "area:daemon" || details.Labels[1] != "enhancement" {
		t.Fatalf("unexpected labels: %#v", details.Labels)
	}
}
