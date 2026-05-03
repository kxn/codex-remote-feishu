package claude

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

var claudeSupportedImageMediaTypes = map[string]struct{}{
	"image/gif":  {},
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

func buildClaudeUserPromptContent(inputs []agentproto.Input) (any, error) {
	if len(inputs) == 0 {
		return "", nil
	}
	textParts := make([]string, 0, len(inputs))
	blocks := make([]map[string]any, 0, len(inputs))
	usingBlocks := false
	flushTextParts := func() {
		for _, text := range textParts {
			if strings.TrimSpace(text) == "" {
				continue
			}
			blocks = append(blocks, claudeTextBlock(text))
		}
		textParts = textParts[:0]
	}

	for _, input := range inputs {
		switch input.Type {
		case agentproto.InputText:
			if strings.TrimSpace(input.Text) == "" {
				continue
			}
			if usingBlocks {
				blocks = append(blocks, claudeTextBlock(input.Text))
			} else {
				textParts = append(textParts, input.Text)
			}
		case agentproto.InputLocalImage:
			if !usingBlocks {
				flushTextParts()
				usingBlocks = true
			}
			block, err := claudeLocalImageBlock(input)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		default:
			return nil, fmt.Errorf("unsupported prompt input type %q", input.Type)
		}
	}

	if !usingBlocks {
		return strings.Join(textParts, "\n\n"), nil
	}
	return blocks, nil
}

func claudeTextBlock(text string) map[string]any {
	return map[string]any{
		"type": "text",
		"text": text,
	}
}

func claudeLocalImageBlock(input agentproto.Input) (map[string]any, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return nil, fmt.Errorf("local image path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local image %q: %w", path, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("local image %q is empty", path)
	}
	mediaType, err := claudeImageMediaType(input.MIMEType, path, data)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": mediaType,
			"data":       base64.StdEncoding.EncodeToString(data),
		},
	}, nil
}

func claudeImageMediaType(explicit, path string, data []byte) (string, error) {
	candidates := []string{
		strings.ToLower(strings.TrimSpace(explicit)),
		claudeImageMediaTypeFromPath(path),
		strings.ToLower(strings.TrimSpace(http.DetectContentType(data))),
	}
	for _, candidate := range candidates {
		if claudeImageMediaTypeSupported(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("unsupported local image media type for %q", strings.TrimSpace(path))
}

func claudeImageMediaTypeFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".gif":
		return "image/gif"
	case ".jpeg", ".jpg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

func claudeImageMediaTypeSupported(value string) bool {
	_, ok := claudeSupportedImageMediaTypes[strings.ToLower(strings.TrimSpace(value))]
	return ok
}
