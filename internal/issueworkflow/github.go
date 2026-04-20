package issueworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

const fetchIssueQuery = `
query($owner: String!, $name: String!, $number: Int!, $comments: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      number
      title
      body
      state
      updatedAt
      url
      labels(first: 100) {
        nodes {
          name
        }
      }
      comments(last: $comments) {
        totalCount
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

type GitHubClient interface {
	FetchIssue(context.Context, Repo, int, int) (Issue, error)
	AddLabels(context.Context, Repo, int, []string) error
	RemoveLabels(context.Context, Repo, int, []string) error
	Comment(context.Context, Repo, int, string) error
	Close(context.Context, Repo, int) error
}

type ghCLI struct{}

func NewGitHubCLI() GitHubClient {
	return &ghCLI{}
}

func (g *ghCLI) FetchIssue(ctx context.Context, repo Repo, number int, comments int) (Issue, error) {
	if comments <= 0 {
		comments = 8
	}
	args := []string{
		"api", "graphql",
		"-f", "query=" + fetchIssueQuery,
		"-f", "owner=" + repo.Owner,
		"-f", "name=" + repo.Name,
		"-F", fmt.Sprintf("number=%d", number),
		"-F", fmt.Sprintf("comments=%d", comments),
	}
	payload, err := g.run(ctx, args...)
	if err != nil {
		return Issue{}, err
	}
	var resp struct {
		Data struct {
			Repository struct {
				Issue *struct {
					Number    int    `json:"number"`
					Title     string `json:"title"`
					Body      string `json:"body"`
					State     string `json:"state"`
					UpdatedAt string `json:"updatedAt"`
					URL       string `json:"url"`
					Labels    struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"labels"`
					Comments struct {
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
	if err := json.Unmarshal(payload, &resp); err != nil {
		return Issue{}, fmt.Errorf("decode gh issue response: %w", err)
	}
	if resp.Data.Repository.Issue == nil {
		return Issue{}, fmt.Errorf("issue #%d not found in %s", number, repo.String())
	}
	raw := resp.Data.Repository.Issue
	updatedAt, err := time.Parse(time.RFC3339, raw.UpdatedAt)
	if err != nil {
		return Issue{}, fmt.Errorf("parse issue updatedAt: %w", err)
	}
	labels := make([]string, 0, len(raw.Labels.Nodes))
	for _, label := range raw.Labels.Nodes {
		labels = append(labels, strings.TrimSpace(label.Name))
	}
	sort.Strings(labels)
	commentsOut := make([]IssueComment, 0, len(raw.Comments.Nodes))
	for _, comment := range raw.Comments.Nodes {
		publishedAt, err := time.Parse(time.RFC3339, comment.PublishedAt)
		if err != nil {
			return Issue{}, fmt.Errorf("parse issue comment publishedAt: %w", err)
		}
		var updatedCommentAt time.Time
		if strings.TrimSpace(comment.UpdatedAt) != "" {
			updatedCommentAt, err = time.Parse(time.RFC3339, comment.UpdatedAt)
			if err != nil {
				return Issue{}, fmt.Errorf("parse issue comment updatedAt: %w", err)
			}
		}
		commentsOut = append(commentsOut, IssueComment{
			Author:      strings.TrimSpace(comment.Author.Login),
			Body:        comment.Body,
			PublishedAt: publishedAt,
			UpdatedAt:   updatedCommentAt,
			URL:         strings.TrimSpace(comment.URL),
		})
	}
	return Issue{
		Number:    raw.Number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     raw.State,
		URL:       raw.URL,
		UpdatedAt: updatedAt,
		Labels:    labels,
		Comments:  commentsOut,
	}, nil
}

func (g *ghCLI) AddLabels(ctx context.Context, repo Repo, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	args := []string{"issue", "edit", fmt.Sprintf("%d", number), "--repo", repo.String()}
	for _, label := range labels {
		args = append(args, "--add-label", label)
	}
	_, err := g.run(ctx, args...)
	return err
}

func (g *ghCLI) RemoveLabels(ctx context.Context, repo Repo, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	args := []string{"issue", "edit", fmt.Sprintf("%d", number), "--repo", repo.String()}
	for _, label := range labels {
		args = append(args, "--remove-label", label)
	}
	_, err := g.run(ctx, args...)
	return err
}

func (g *ghCLI) Comment(ctx context.Context, repo Repo, number int, bodyFile string) error {
	_, err := g.run(ctx, "issue", "comment", fmt.Sprintf("%d", number), "--repo", repo.String(), "--body-file", bodyFile)
	return err
}

func (g *ghCLI) Close(ctx context.Context, repo Repo, number int) error {
	_, err := g.run(ctx, "issue", "close", fmt.Sprintf("%d", number), "--repo", repo.String())
	return err
}

func (g *ghCLI) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := execlaunch.CommandContext(ctx, "gh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), message)
	}
	return output, nil
}
