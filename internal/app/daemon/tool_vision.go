package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/singleturn"
)

const (
	describeImageToolName         = "describe_image"
	describeImageMaxImages        = 5
	describeImageMaxBytes         = 20 << 20
	describeImageDefaultMaxTokens = 1024
)

const describeImageToolDescription = "Describe one or more local images through a vision model and return its textual analysis. Call this tool when you cannot directly see image content in the conversation and need to know what an image shows. Useful for: reading text in images (errors, code, documents, numbers), describing UI / charts / objects, and comparing multiple images. Do NOT call this tool if you can view images directly; in that case answer from the image itself. Pass each image's local path (same reference as in the conversation inputs). For multiple images, give each a short id and refer to ids in prompt (e.g. \"compare img1 and img2\"). The tool returns plain text; use it to continue answering the user."

const describeImageFallbackPrompt = "请描述这张图片。"

func describeImageToolDefinitions() []toolDefinition {
	return []toolDefinition{{
		Name:        describeImageToolName,
		Description: describeImageToolDescription,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"images": map[string]any{
					"type":     "array",
					"minItems": 1,
					"maxItems": describeImageMaxImages,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id": map[string]any{
								"type":        "string",
								"description": "Short identifier, e.g. img1, img2",
							},
							"image": map[string]any{
								"type":        "string",
								"description": "Local file path of the image, same reference as in the conversation inputs",
							},
						},
						"required": []string{"id", "image"},
					},
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Required question for the vision model; may reference ids, e.g. \"compare img1 and img2\".",
				},
			},
			"required": []string{"images", "prompt"},
		},
	}}
}

// callerProfileVisionSupported 判断调用方实例当前生效 profile 是否声明主模型
// 支持直接看图。实例或 profile 未知时保守返回 false（默认注入工具）。
func (a *App) callerProfileVisionSupported(req *http.Request) bool {
	if req == nil {
		return false
	}
	return a.callerInstanceProfileVisionSupported(toolCallerInstanceIDFromContext(req.Context()))
}

func (a *App) callerInstanceProfileVisionSupported(instanceID string) bool {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return false
	}
	return a.callerInstanceProfileVisionSupportedWithConfig(loaded.Config, instanceID)
}

func (a *App) callerInstanceNeedsVisionAssist(instanceID string) bool {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return false
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return false
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return false
	}
	if !visionAssistConfigured(loaded.Config.VisionAssist) {
		return false
	}
	return !instanceProfileVisionSupported(loaded.Config, inst)
}

func (a *App) callerRequestNeedsVisionAssist(req *http.Request) bool {
	if req == nil {
		return false
	}
	return a.callerInstanceNeedsVisionAssist(toolCallerInstanceIDFromContext(req.Context()))
}

func (a *App) callerInstanceProfileVisionSupportedWithConfig(cfg config.AppConfig, instanceID string) bool {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return false
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return false
	}
	return instanceProfileVisionSupported(cfg, inst)
}

func instanceProfileVisionSupported(cfg config.AppConfig, inst *state.InstanceRecord) bool {
	if inst == nil {
		return false
	}
	switch agentproto.NormalizeBackend(inst.Backend) {
	case agentproto.BackendCodex:
		return codexProfileVisionSupported(cfg, inst.CodexProfileID)
	case agentproto.BackendClaude:
		return claudeProfileVisionSupported(cfg, inst.ClaudeProfileID)
	case agentproto.BackendOpenCode:
		return openCodeProfileVisionSupported(cfg, inst.OpenCodeProfileID)
	default:
		return false
	}
}

func codexProfileVisionSupported(cfg config.AppConfig, profileID string) bool {
	profileID = state.NormalizeCodexProfileID(profileID)
	if profileID == config.CodexOAuthProfileID {
		return true
	}
	if profileID == config.CodexNativeProfileID {
		return false
	}
	index := config.IndexOfCodexAPIProfile(cfg.Codex.Profiles, profileID)
	if index < 0 {
		return false
	}
	profile, ok := config.CurrentCodexAPIProfile(cfg.Codex.Profiles[index])
	return ok && profile.VisionSupported
}

func claudeProfileVisionSupported(cfg config.AppConfig, profileID string) bool {
	profileID = state.NormalizeClaudeProfileID(profileID)
	for _, profile := range cfg.Claude.Profiles {
		if state.NormalizeClaudeProfileID(profile.ID) == profileID {
			return profile.VisionSupported
		}
	}
	return false
}

func openCodeProfileVisionSupported(cfg config.AppConfig, profileID string) bool {
	profileID = state.NormalizeOpenCodeProfileID(profileID)
	if profileID == state.DefaultOpenCodeProfileID {
		return false
	}
	for _, record := range config.NormalizeOpenCodeAPIProfileRecords(cfg.OpenCode.Profiles) {
		profile, ok := config.CurrentOpenCodeAPIProfile(record)
		if ok && state.NormalizeOpenCodeProfileID(profile.ID) == profileID {
			return profile.VisionSupported
		}
	}
	return false
}

func (a *App) describeImageTool(ctx context.Context, arguments map[string]any) (any, *toolError) {
	// 防御性校验：声明支持视觉的 profile 不应调用本工具。
	if a.callerInstanceProfileVisionSupported(toolCallerInstanceIDFromContext(ctx)) {
		return nil, &toolError{
			Code:    "describe_image_not_needed",
			Message: "当前主模型支持直接看图，不需要调用 describe_image。",
		}
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, &toolError{Code: "config_unavailable", Message: "无法读取配置：" + err.Error()}
	}
	settings := loaded.Config.VisionAssist
	protocol := strings.TrimSpace(settings.Protocol)
	if protocol == "" {
		protocol = string(singleturn.ProtocolOpenAIChat)
	}
	if strings.TrimSpace(settings.BaseURL) == "" {
		return nil, &toolError{
			Code:    "vision_assist_not_configured",
			Message: "视觉辅助模型未配置，请在管理页「对话后端 → 辅助模型」中配置端点。",
		}
	}
	rawImages, ok := arguments["images"].([]any)
	if !ok || len(rawImages) == 0 || len(rawImages) > describeImageMaxImages {
		return nil, &toolError{
			Code:    "describe_image_invalid_images",
			Message: fmt.Sprintf("images 必须包含 1-%d 张图片。", describeImageMaxImages),
		}
	}
	images := make([]singleturn.Image, 0, len(rawImages))
	idMap := make([]string, 0, len(rawImages))
	for index, raw := range rawImages {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, &toolError{Code: "describe_image_invalid_item", Message: "images 每项必须是 {id, image} 对象。"}
		}
		id := strings.TrimSpace(fmt.Sprint(item["id"]))
		path := strings.TrimSpace(fmt.Sprint(item["image"]))
		if id == "" || path == "" {
			return nil, &toolError{Code: "describe_image_invalid_item", Message: "images 每项需要非空的 id 与 image。"}
		}
		data, mimeType, toolErr := readImageFile(path)
		if toolErr != nil {
			return nil, toolErr
		}
		images = append(images, singleturn.Image{ID: id, Data: data, MIMEType: mimeType})
		idMap = append(idMap, fmt.Sprintf("%s=第%d张", id, index+1))
	}
	prompt := ""
	if promptValue, ok := arguments["prompt"]; ok && promptValue != nil {
		prompt = strings.TrimSpace(fmt.Sprint(promptValue))
	}
	if prompt == "" {
		// 模型未提供有效问题时使用最简单的默认指令，避免空指令调用。
		prompt = describeImageFallbackPrompt
	}
	text := "图片 ID 映射：" + strings.Join(idMap, "，") + "。请按 ID 引用图片。"
	if prompt != "" {
		text += "\n" + prompt
	}
	provider, err := singleturn.NewProvider(singleturn.Protocol(protocol), singleturn.Config{
		BaseURL:   settings.BaseURL,
		APIKey:    strings.TrimSpace(settings.APIKey),
		Model:     strings.TrimSpace(settings.Model),
		MaxTokens: describeImageDefaultMaxTokens,
	})
	if err != nil {
		return nil, &toolError{Code: "vision_assist_provider_invalid", Message: err.Error()}
	}
	result, err := provider.Complete(ctx, singleturn.Request{
		Messages: []singleturn.Message{{Text: text, Images: images}},
	})
	if err != nil {
		return nil, &toolError{Code: "vision_assist_call_failed", Message: err.Error()}
	}
	return map[string]any{"text": result}, nil
}

func readImageFile(path string) ([]byte, string, *toolError) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", &toolError{Code: "image_not_found", Message: "图片路径不存在或不可读：" + path}
	}
	if info.Size() > describeImageMaxBytes {
		return nil, "", &toolError{Code: "image_too_large", Message: "图片超过 20MB 限制。"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", &toolError{Code: "image_read_failed", Message: "读取图片失败：" + err.Error()}
	}
	mimeType := imageMIMEType(data)
	if mimeType == "" {
		return nil, "", &toolError{Code: "image_not_supported", Message: "不支持的文件格式，仅支持 PNG/JPEG/WebP/GIF 图片。"}
	}
	return data, mimeType, nil
}

func imageMIMEType(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 6 && string(data[:6]) == "GIF87a" || len(data) >= 6 && string(data[:6]) == "GIF89a":
		return "image/gif"
	default:
		return ""
	}
}
