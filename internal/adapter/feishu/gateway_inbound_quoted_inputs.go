package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (g *LiveGateway) quotedInputs(ctx context.Context, message *larkim.EventMessage) []agentproto.Input {
	if message == nil || g.fetchMessageFn == nil {
		return nil
	}
	ctx, cancel := newFeishuTimeoutContext(ctx, inboundMessageParseTimeout)
	defer cancel()

	targetMessageID := referencedMessageID(message)
	if targetMessageID == "" {
		return nil
	}
	referenced, err := g.fetchMessageFn(ctx, targetMessageID)
	if err != nil {
		log.Printf("feishu quote fetch ignored: message=%s err=%v", targetMessageID, err)
		return nil
	}
	return g.inputsFromReferencedMessage(ctx, referenced)
}

func (g *LiveGateway) inputsFromReferencedMessage(ctx context.Context, referenced *gatewayMessage) []agentproto.Input {
	if referenced == nil || referenced.Deleted {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(referenced.MessageType)) {
	case "text":
		text, err := gatewaypkg.ParseTextContent(referenced.Content)
		if err != nil {
			log.Printf("feishu quote text parse ignored: message=%s err=%v", referenced.MessageID, err)
			return nil
		}
		if wrapped := quotedTextInput(text); wrapped.Text != "" {
			return []agentproto.Input{wrapped}
		}
		return nil
	case "post":
		inputs, text, err := g.parsePostInputs(ctx, referenced.MessageID, referenced.Content)
		if err != nil {
			log.Printf("feishu quote post parse ignored: message=%s err=%v", referenced.MessageID, err)
			return nil
		}
		quoted := make([]agentproto.Input, 0, len(inputs)+1)
		if wrapped := quotedTextInput(text); wrapped.Text != "" {
			quoted = append(quoted, wrapped)
		}
		for _, input := range inputs {
			if input.Type == agentproto.InputLocalImage || input.Type == agentproto.InputRemoteImage {
				quoted = append(quoted, input)
			}
		}
		return quoted
	case "image":
		imageKey, err := gatewaypkg.ParseImageKey(referenced.Content)
		if err != nil {
			log.Printf("feishu quote image parse ignored: message=%s err=%v", referenced.MessageID, err)
			return nil
		}
		path, mimeType, err := g.downloadImageFn(ctx, referenced.MessageID, imageKey)
		if err != nil {
			log.Printf("feishu quote image download ignored: message=%s err=%v", referenced.MessageID, err)
			return nil
		}
		return []agentproto.Input{{Type: agentproto.InputLocalImage, Path: path, MIMEType: mimeType}}
	case "merge_forward":
		payload, err := g.buildMergeForwardStructuredPayloadFromGatewayMessage(ctx, referenced, true)
		if err != nil {
			log.Printf("feishu quote merge_forward parse ignored: message=%s err=%v", referenced.MessageID, err)
			return nil
		}
		if len(payload.Inputs) > 0 {
			return payload.Inputs
		}
		return nil
	default:
		return nil
	}
}

func referencedMessageID(message *larkim.EventMessage) string {
	if message == nil {
		return ""
	}
	targetMessageID := strings.TrimSpace(stringPtr(message.ParentId))
	if targetMessageID == "" {
		targetMessageID = strings.TrimSpace(stringPtr(message.RootId))
	}
	return targetMessageID
}

func quotedTextInput(text string) agentproto.Input {
	text = strings.TrimSpace(text)
	if text == "" {
		return agentproto.Input{}
	}
	return agentproto.Input{
		Type: agentproto.InputText,
		Text: "<被引用内容>\n" + text + "\n</被引用内容>",
	}
}

func (g *LiveGateway) parsePostInputs(ctx context.Context, messageID, rawContent string) ([]agentproto.Input, string, error) {
	ctx, cancel := newFeishuTimeoutContext(ctx, inboundMessageParseTimeout)
	defer cancel()

	var content feishuPostContent
	if err := json.Unmarshal([]byte(rawContent), &content); err != nil {
		return nil, "", err
	}
	inputs := make([]agentproto.Input, 0, len(content.Content)+1)
	textParts := make([]string, 0, len(content.Content)+1)
	if title := strings.TrimSpace(content.Title); title != "" {
		inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: title})
		textParts = append(textParts, title)
	}
	for _, paragraph := range content.Content {
		var segment strings.Builder
		flushText := func() {
			text := strings.TrimSpace(segment.String())
			segment.Reset()
			if text == "" {
				return
			}
			inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: text})
			textParts = append(textParts, text)
		}
		for _, node := range paragraph {
			switch strings.ToLower(strings.TrimSpace(node.Tag)) {
			case "text":
				segment.WriteString(node.Text)
			case "a":
				switch {
				case strings.TrimSpace(node.Text) != "" && strings.TrimSpace(node.Href) != "":
					segment.WriteString(strings.TrimSpace(node.Text) + " (" + strings.TrimSpace(node.Href) + ")")
				case strings.TrimSpace(node.Text) != "":
					segment.WriteString(node.Text)
				case strings.TrimSpace(node.Href) != "":
					segment.WriteString(strings.TrimSpace(node.Href))
				}
			case "at":
				switch {
				case strings.TrimSpace(node.Text) != "":
					segment.WriteString(node.Text)
				case strings.TrimSpace(node.UserName) != "":
					segment.WriteString("@" + strings.TrimSpace(node.UserName))
				case strings.TrimSpace(node.UserID) != "":
					segment.WriteString("@" + strings.TrimSpace(node.UserID))
				}
			case "emotion":
				if emoji := strings.TrimSpace(node.EmojiType); emoji != "" {
					segment.WriteString(":" + emoji + ":")
				}
			case "code_block":
				code := strings.TrimSpace(node.Text)
				if code == "" {
					continue
				}
				if segment.Len() > 0 {
					segment.WriteString("\n")
				}
				if language := strings.TrimSpace(node.Language); language != "" {
					segment.WriteString("```" + language + "\n" + code + "\n```")
				} else {
					segment.WriteString("```\n" + code + "\n```")
				}
			case "img", "media":
				if strings.TrimSpace(node.ImageKey) == "" {
					continue
				}
				flushText()
				path, mimeType, err := g.downloadImageFn(ctx, messageID, strings.TrimSpace(node.ImageKey))
				if err != nil {
					return nil, "", err
				}
				inputs = append(inputs, agentproto.Input{Type: agentproto.InputLocalImage, Path: path, MIMEType: mimeType})
			}
		}
		flushText()
	}
	return inputs, strings.Join(textParts, "\n\n"), nil
}

func (g *LiveGateway) fetchMessage(ctx context.Context, messageID string) (*gatewayMessage, error) {
	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message.get",
		Class:     CallClassIMRead,
		Priority:  CallPriorityReadAssist,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
		},
		Retry:      RetrySafe,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.GetMessageResp, error) {
		resp, err := client.Im.V1.Message.Get(callCtx, larkim.NewGetMessageReqBuilder().
			MessageId(messageID).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message.get", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 || resp.Data.Items[0] == nil {
		return nil, fmt.Errorf("get message failed: empty response")
	}
	items := make([]*gatewayMessage, 0, len(resp.Data.Items))
	index := make(map[string]*gatewayMessage, len(resp.Data.Items))
	for _, item := range resp.Data.Items {
		if item == nil {
			continue
		}
		content := ""
		if item.Body != nil {
			content = stringPtr(item.Body.Content)
		}
		msg := &gatewayMessage{
			MessageID:      stringPtr(item.MessageId),
			MessageType:    stringPtr(item.MsgType),
			Content:        content,
			Deleted:        boolPtr(item.Deleted),
			UpperMessageID: stringPtr(item.UpperMessageId),
		}
		if item.Sender != nil {
			msg.SenderID = stringPtr(item.Sender.Id)
			msg.SenderType = stringPtr(item.Sender.SenderType)
		}
		items = append(items, msg)
		if msg.MessageID != "" {
			index[msg.MessageID] = msg
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("get message failed: empty response")
	}
	root := index[messageID]
	if root == nil {
		root = items[0]
	}
	for _, item := range items {
		if item == nil || item == root {
			continue
		}
		parentID := strings.TrimSpace(item.UpperMessageID)
		if parentID == "" {
			continue
		}
		parent := index[parentID]
		if parent == nil {
			continue
		}
		parent.Children = append(parent.Children, item)
	}
	return root, nil
}

func (g *LiveGateway) downloadImage(ctx context.Context, messageID, imageKey string) (string, string, error) {
	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message_resource.get",
		Class:     CallClassIMRead,
		Priority:  CallPriorityReadAssist,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
			FileKey:   imageKey,
		},
		Retry:      RetrySafe,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.GetMessageResourceResp, error) {
		resp, err := client.Im.V1.MessageResource.Get(callCtx, larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(imageKey).
			Type("image").
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message_resource.get", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if err != nil {
		return "", "", err
	}
	dir := g.config.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	file, err := os.CreateTemp(dir, "codex-remote-image-*")
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	bytes, err := io.ReadAll(resp.File)
	if err != nil {
		return "", "", err
	}
	if _, err := file.Write(bytes); err != nil {
		return "", "", err
	}
	if err := file.Close(); err != nil {
		return "", "", err
	}
	mimeType := http.DetectContentType(bytes)
	target := file.Name()
	if ext := mimeExtension(mimeType); ext != "" && !strings.HasSuffix(target, ext) {
		renamed := target + ext
		if err := os.Rename(target, renamed); err == nil {
			target = renamed
		}
	}
	return target, mimeType, nil
}

func (g *LiveGateway) downloadFile(ctx context.Context, messageID, fileKey, fileName string) (string, error) {
	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message_resource.get",
		Class:     CallClassIMRead,
		Priority:  CallPriorityReadAssist,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
			FileKey:   fileKey,
		},
		Retry:      RetrySafe,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.GetMessageResourceResp, error) {
		resp, err := client.Im.V1.MessageResource.Get(callCtx, larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(fileKey).
			Type("file").
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message_resource.get", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if err != nil {
		return "", err
	}
	dir := g.config.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	pattern := "codex-remote-file-*"
	if trimmed := strings.TrimSpace(fileName); trimmed != "" {
		pattern = strings.NewReplacer("\n", "-", "\r", "-", "\\", "-", "/", "-").Replace(trimmed) + "-*"
	}
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.File); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func boolPtr(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
