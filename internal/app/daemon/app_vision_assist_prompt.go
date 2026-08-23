package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const visionAssistImagePathPromptHeader = "附带参考图片（图片内容未直接注入上下文；如需理解图片，请调用 describe_image 并使用以下本地路径）："

func (a *App) prepareAgentCommandForDispatch(instanceID string, command agentproto.Command) agentproto.Command {
	if !commandHasPromptInputs(command.Kind) || !a.callerInstanceNeedsVisionAssist(instanceID) {
		return command
	}
	command.Prompt.Inputs = visionAssistPromptInputs(command.Prompt.Inputs)
	return command
}

func commandHasPromptInputs(kind agentproto.CommandKind) bool {
	switch kind {
	case agentproto.CommandPromptSend, agentproto.CommandTurnSteer:
		return true
	default:
		return false
	}
}

func visionAssistPromptInputs(inputs []agentproto.Input) []agentproto.Input {
	if len(inputs) == 0 {
		return inputs
	}
	imageRefs := make([]agentproto.Input, 0)
	output := make([]agentproto.Input, 0, len(inputs))
	for _, input := range inputs {
		if input.Type == agentproto.InputLocalImage && strings.TrimSpace(input.Path) != "" {
			imageRefs = append(imageRefs, input)
			continue
		}
		output = append(output, input)
	}
	if len(imageRefs) == 0 {
		return inputs
	}
	pathPrompt := agentproto.Input{
		Type: agentproto.InputText,
		Text: visionAssistImagePathPrompt(imageRefs),
	}
	return append([]agentproto.Input{pathPrompt}, output...)
}

func visionAssistImagePathPrompt(images []agentproto.Input) string {
	lines := []string{visionAssistImagePathPromptHeader}
	for index, image := range images {
		path := strings.TrimSpace(image.Path)
		if path == "" {
			continue
		}
		id := fmt.Sprintf("img%d", index+1)
		mimeType := strings.TrimSpace(image.MIMEType)
		if mimeType != "" {
			lines = append(lines, fmt.Sprintf("- %s: %s (%s)", id, path, mimeType))
		} else {
			lines = append(lines, fmt.Sprintf("- %s: %s", id, path))
		}
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}
