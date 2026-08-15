package singleturn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type openAIChatProvider struct {
	cfg    Config
	client *http.Client
}

func (p *openAIChatProvider) Complete(ctx context.Context, req Request) (string, error) {
	model := modelOrDefault(p.cfg, req.Model)
	if model == "" {
		return "", fmt.Errorf("singleturn: openai_chat requires model")
	}
	content := make([]any, 0, 2)
	for _, message := range req.Messages {
		if text := strings.TrimSpace(message.Text); text != "" {
			content = append(content, map[string]any{"type": "text", "text": text})
		}
		for _, image := range message.Images {
			content = append(content, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": dataURL(image),
				},
			})
		}
	}
	if len(content) == 0 {
		return "", fmt.Errorf("singleturn: empty message content")
	}
	body, err := postJSON(ctx, p.client, p.cfg.BaseURL+"/chat/completions", p.cfg.APIKey, nil, map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": content}},
	})
	if err != nil {
		return "", err
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("singleturn: decode openai_chat response: %w", err)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("singleturn: openai_chat returned no choices")
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}
