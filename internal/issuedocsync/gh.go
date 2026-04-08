package issuedocsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const listClosedIssuesQuery = `
query($owner: String!, $name: String!, $after: String) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, states: CLOSED, orderBy: {field: UPDATED_AT, direction: DESC}, after: $after) {
      pageInfo {
        hasNextPage
        endCursor
      }
      nodes {
        number
        title
        updatedAt
        closedAt
        url
      }
    }
  }
}
`

const inspectIssueQuery = `
query($owner: String!, $name: String!, $number: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      number
      title
      body
      updatedAt
      closedAt
      url
      labels(first: 50) {
        nodes {
          name
        }
      }
      comments(first: 100, after: $after) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          body
          publishedAt
          updatedAt
          url
          author {
            login
          }
        }
      }
    }
  }
}
`

type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type ghCLI struct {
	runner Runner
}

func NewGitHubCLI() *ghCLI {
	return &ghCLI{runner: execRunner{}}
}

func (c *ghCLI) ListClosedIssueSummaries(ctx context.Context, repo Repo) ([]IssueSummary, error) {
	type graphQLResponse struct {
		Data struct {
			Repository struct {
				Issues struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Number    int    `json:"number"`
						Title     string `json:"title"`
						UpdatedAt string `json:"updatedAt"`
						ClosedAt  string `json:"closedAt"`
						URL       string `json:"url"`
					} `json:"nodes"`
				} `json:"issues"`
			} `json:"repository"`
		} `json:"data"`
	}

	after := ""
	all := make([]IssueSummary, 0, 128)
	for {
		args := []string{
			"api", "graphql",
			"-f", "query=" + listClosedIssuesQuery,
			"-f", "owner=" + repo.Owner,
			"-f", "name=" + repo.Name,
		}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		payload, err := c.runner.Run(ctx, args...)
		if err != nil {
			return nil, err
		}
		var resp graphQLResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return nil, fmt.Errorf("decode github graphql response: %w", err)
		}
		for _, node := range resp.Data.Repository.Issues.Nodes {
			updatedAt, err := time.Parse(time.RFC3339, node.UpdatedAt)
			if err != nil {
				return nil, fmt.Errorf("parse issue #%d updatedAt: %w", node.Number, err)
			}
			var closedAt time.Time
			if node.ClosedAt != "" {
				closedAt, err = time.Parse(time.RFC3339, node.ClosedAt)
				if err != nil {
					return nil, fmt.Errorf("parse issue #%d closedAt: %w", node.Number, err)
				}
			}
			all = append(all, IssueSummary{
				Number:    node.Number,
				Title:     node.Title,
				UpdatedAt: updatedAt,
				ClosedAt:  closedAt,
				URL:       node.URL,
			})
		}
		if !resp.Data.Repository.Issues.PageInfo.HasNextPage {
			return all, nil
		}
		after = resp.Data.Repository.Issues.PageInfo.EndCursor
	}
}

func (c *ghCLI) FetchIssueDetails(ctx context.Context, repo Repo, number int) (IssueDetails, error) {
	type graphQLResponse struct {
		Data struct {
			Repository struct {
				Issue *struct {
					Number    int    `json:"number"`
					Title     string `json:"title"`
					Body      string `json:"body"`
					UpdatedAt string `json:"updatedAt"`
					ClosedAt  string `json:"closedAt"`
					URL       string `json:"url"`
					Labels    struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"labels"`
					Comments struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							Body        string `json:"body"`
							PublishedAt string `json:"publishedAt"`
							UpdatedAt   string `json:"updatedAt"`
							URL         string `json:"url"`
							Author      struct {
								Login string `json:"login"`
							} `json:"author"`
						} `json:"nodes"`
					} `json:"comments"`
				} `json:"issue"`
			} `json:"repository"`
		} `json:"data"`
	}

	after := ""
	var details IssueDetails
	for {
		args := []string{
			"api", "graphql",
			"-f", "query=" + inspectIssueQuery,
			"-f", "owner=" + repo.Owner,
			"-f", "name=" + repo.Name,
			"-F", fmt.Sprintf("number=%d", number),
		}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		payload, err := c.runner.Run(ctx, args...)
		if err != nil {
			return IssueDetails{}, err
		}
		var resp graphQLResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return IssueDetails{}, fmt.Errorf("decode github graphql response: %w", err)
		}
		if resp.Data.Repository.Issue == nil {
			return IssueDetails{}, fmt.Errorf("issue #%d not found in %s", number, repo.String())
		}
		issue := resp.Data.Repository.Issue
		if details.Number == 0 {
			updatedAt, err := time.Parse(time.RFC3339, issue.UpdatedAt)
			if err != nil {
				return IssueDetails{}, fmt.Errorf("parse issue #%d updatedAt: %w", issue.Number, err)
			}
			var closedAt time.Time
			if issue.ClosedAt != "" {
				closedAt, err = time.Parse(time.RFC3339, issue.ClosedAt)
				if err != nil {
					return IssueDetails{}, fmt.Errorf("parse issue #%d closedAt: %w", issue.Number, err)
				}
			}
			labels := make([]string, 0, len(issue.Labels.Nodes))
			for _, node := range issue.Labels.Nodes {
				labels = append(labels, node.Name)
			}
			sort.Strings(labels)
			details = IssueDetails{
				Number:    issue.Number,
				Title:     issue.Title,
				Body:      issue.Body,
				UpdatedAt: updatedAt,
				ClosedAt:  closedAt,
				URL:       issue.URL,
				Labels:    labels,
				Comments:  make([]IssueComment, 0, len(issue.Comments.Nodes)),
			}
		}
		for _, commentNode := range issue.Comments.Nodes {
			publishedAt, err := time.Parse(time.RFC3339, commentNode.PublishedAt)
			if err != nil {
				return IssueDetails{}, fmt.Errorf("parse issue #%d comment publishedAt: %w", issue.Number, err)
			}
			var updatedAt time.Time
			if commentNode.UpdatedAt != "" {
				updatedAt, err = time.Parse(time.RFC3339, commentNode.UpdatedAt)
				if err != nil {
					return IssueDetails{}, fmt.Errorf("parse issue #%d comment updatedAt: %w", issue.Number, err)
				}
			}
			details.Comments = append(details.Comments, IssueComment{
				Author:      strings.TrimSpace(commentNode.Author.Login),
				Body:        commentNode.Body,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				URL:         commentNode.URL,
			})
		}
		if !issue.Comments.PageInfo.HasNextPage {
			return details, nil
		}
		after = issue.Comments.PageInfo.EndCursor
	}
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	payload, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh %v: %w\n%s", args, err, string(payload))
	}
	return payload, nil
}
