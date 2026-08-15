package singleturn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type openAIResponsesProvider struct {
	cfg    Config
	client *http.Client
}

func (p *openAIResponsesProvider) Complete(ctx context.Context, req Request) (string, error) {
	model := modelOrDefault(p.cfg, req.Model)
	if model == "" {
		return "", fmt.Errorf("singleturn: openai_responses requires model")
	}
	content := make([]any, 0, 2)
	for _, message := range req.Messages {
		if text := strings.TrimSpace(message.Text); text != "" {
			content = append(content, map[string]any{"type": "input_text", "text": text})
		}
		for _, image := range message.Images {
			content = append(content, map[string]any{
				"type":      "input_image",
				"image_url": dataURL(image),
			})
		}
	}
	if len(content) == 0 {
		return "", fmt.Errorf("singleturn: empty message content")
	}
	body, err := postJSON(ctx, p.client, p.cfg.BaseURL+"/responses", p.cfg.APIKey, nil, map[string]any{
		"model": model,
		"input": []any{map[string]any{"role": "user", "content": content}},
	})
	if err != nil {
		return "", err
	}
	var response struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("singleturn: decode openai_responses response: %w", err)
	}
	if text := strings.TrimSpace(response.OutputText); text != "" {
		return text, nil
	}
	var parts []string
	for _, item := range response.Output {
		for _, contentItem := range item.Content {
			if strings.EqualFold(contentItem.Type, "output_text") {
				parts = append(parts, strings.TrimSpace(contentItem.Text))
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}
