package singleturn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type geminiProvider struct {
	cfg    Config
	client *http.Client
}

func (p *geminiProvider) Complete(ctx context.Context, req Request) (string, error) {
	model := modelOrDefault(p.cfg, req.Model)
	if model == "" {
		return "", fmt.Errorf("singleturn: gemini requires model")
	}
	parts := make([]any, 0, 2)
	for _, message := range req.Messages {
		if text := strings.TrimSpace(message.Text); text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
		for _, image := range message.Images {
			parts = append(parts, map[string]any{
				"inline_data": map[string]any{
					"mime_type": image.MIMEType,
					"data":      image.Data,
				},
			})
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("singleturn: empty message content")
	}
	body, err := postJSON(ctx, p.client, p.cfg.BaseURL+"/models/"+model+":generateContent", p.cfg.APIKey, nil, map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": parts}},
	})
	if err != nil {
		return "", err
	}
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("singleturn: decode gemini response: %w", err)
	}
	if len(response.Candidates) == 0 {
		return "", fmt.Errorf("singleturn: gemini returned no candidates")
	}
	var partsText []string
	for _, part := range response.Candidates[0].Content.Parts {
		partsText = append(partsText, strings.TrimSpace(part.Text))
	}
	return strings.TrimSpace(strings.Join(partsText, "\n")), nil
}
