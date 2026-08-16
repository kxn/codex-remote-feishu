package singleturn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type anthropicProvider struct {
	cfg    Config
	client *http.Client
}

func (p *anthropicProvider) Complete(ctx context.Context, req Request) (string, error) {
	model := modelOrDefault(p.cfg, req.Model)
	if model == "" {
		return "", fmt.Errorf("singleturn: anthropic requires model")
	}
	maxTokens := p.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	content := make([]any, 0, 2)
	for _, message := range req.Messages {
		if text := strings.TrimSpace(message.Text); text != "" {
			content = append(content, map[string]any{"type": "text", "text": text})
		}
		for _, image := range message.Images {
			content = append(content, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": image.MIMEType,
					"data":       base64.StdEncoding.EncodeToString(image.Data),
				},
			})
		}
	}
	if len(content) == 0 {
		return "", fmt.Errorf("singleturn: empty message content")
	}
	body, err := postJSON(ctx, p.client, p.cfg.BaseURL+"/messages", "", map[string]string{
		"x-api-key":         p.cfg.APIKey,
		"anthropic-version": "2023-06-01",
	}, map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   []any{map[string]any{"role": "user", "content": content}},
	})
	if err != nil {
		return "", err
	}
	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("singleturn: decode anthropic response: %w", err)
	}
	var parts []string
	for _, item := range response.Content {
		if strings.EqualFold(item.Type, "text") {
			parts = append(parts, strings.TrimSpace(item.Text))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}
