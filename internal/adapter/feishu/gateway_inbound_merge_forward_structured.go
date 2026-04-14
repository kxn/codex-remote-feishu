package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	forwardedChatSchemaV1         = "forwarded_chat_bundle.v1"
	forwardedChatInputTagV1       = "forwarded_chat_bundle_v1"
	quotedForwardedChatInputTagV1 = "quoted_forwarded_chat_bundle_v1"
)

type mergeForwardStructuredPayload struct {
	Summary string
	Inputs  []agentproto.Input
}

type forwardedChatEnvelope struct {
	Schema string               `json:"schema"`
	Source string               `json:"source,omitempty"`
	Root   forwardedChatNode    `json:"root"`
	Assets *forwardedChatAssets `json:"assets,omitempty"`
}

type forwardedChatAssets struct {
	Images []forwardedChatImageAsset `json:"images,omitempty"`
}

type forwardedChatImageAsset struct {
	Ref      string `json:"ref"`
	MIMEType string `json:"mime_type,omitempty"`
}

type forwardedChatNode struct {
	Kind        string               `json:"kind"`
	BundleID    string               `json:"bundle_id,omitempty"`
	Title       string               `json:"title,omitempty"`
	Items       []forwardedChatNode  `json:"items,omitempty"`
	MessageID   string               `json:"message_id,omitempty"`
	Sender      *forwardedChatSender `json:"sender,omitempty"`
	MessageType string               `json:"message_type,omitempty"`
	Text        string               `json:"text,omitempty"`
	DisplayText string               `json:"display_text,omitempty"`
	ImageRefs   []string             `json:"image_refs,omitempty"`
	State       string               `json:"state,omitempty"`
}

type forwardedChatSender struct {
	ID    string `json:"id,omitempty"`
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

type mergeForwardBuilder struct {
	gateway     *LiveGateway
	nextBundle  int
	nextImage   int
	imageAssets []forwardedChatImageAsset
	imageInputs []agentproto.Input
}

func (g *LiveGateway) buildMergeForwardStructuredPayloadFromEvent(ctx context.Context, message *larkim.EventMessage) (mergeForwardStructuredPayload, error) {
	if message == nil {
		return mergeForwardStructuredPayload{}, fmt.Errorf("nil merge_forward message")
	}
	if g.fetchMessageFn != nil {
		messageID := strings.TrimSpace(stringPtr(message.MessageId))
		if messageID != "" {
			referenced, err := g.fetchMessageFn(ctx, messageID)
			if err == nil && referenced != nil && strings.EqualFold(strings.TrimSpace(referenced.MessageType), "merge_forward") {
				return g.buildMergeForwardStructuredPayloadFromGatewayMessage(ctx, referenced, false)
			}
		}
	}
	return g.buildMergeForwardStructuredPayloadFromRawContent(strings.TrimSpace(stringPtr(message.Content)), false)
}

func (g *LiveGateway) buildMergeForwardStructuredPayloadFromGatewayMessage(ctx context.Context, message *gatewayMessage, quoted bool) (mergeForwardStructuredPayload, error) {
	builder := mergeForwardBuilder{gateway: g}
	root, err := builder.buildRootBundleFromGatewayMessage(ctx, message)
	if err != nil {
		return mergeForwardStructuredPayload{}, err
	}
	return builder.renderPayload(root, quoted)
}

func (g *LiveGateway) buildMergeForwardStructuredPayloadFromRawContent(rawContent string, quoted bool) (mergeForwardStructuredPayload, error) {
	builder := mergeForwardBuilder{gateway: g}
	root, err := builder.buildRootBundleFromRawContent(rawContent)
	if err != nil {
		return mergeForwardStructuredPayload{}, err
	}
	return builder.renderPayload(root, quoted)
}

func (b *mergeForwardBuilder) renderPayload(root *forwardedChatNode, quoted bool) (mergeForwardStructuredPayload, error) {
	if root == nil {
		return mergeForwardStructuredPayload{}, fmt.Errorf("nil forwarded chat root")
	}
	envelope := forwardedChatEnvelope{
		Schema: forwardedChatSchemaV1,
		Source: "feishu.merge_forward",
		Root:   *root,
	}
	if len(b.imageAssets) > 0 {
		envelope.Assets = &forwardedChatAssets{Images: append([]forwardedChatImageAsset{}, b.imageAssets...)}
	}
	rendered, err := renderForwardedChatEnvelope(envelope, quoted)
	if err != nil {
		return mergeForwardStructuredPayload{}, err
	}
	inputs := make([]agentproto.Input, 0, 1+len(b.imageInputs))
	inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: rendered})
	inputs = append(inputs, b.imageInputs...)
	return mergeForwardStructuredPayload{
		Summary: forwardedChatSummaryText(*root),
		Inputs:  inputs,
	}, nil
}

func renderForwardedChatEnvelope(envelope forwardedChatEnvelope, quoted bool) (string, error) {
	body, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", err
	}
	tag := forwardedChatInputTagV1
	if quoted {
		tag = quotedForwardedChatInputTagV1
	}
	return "<" + tag + ">\n" + string(body) + "\n</" + tag + ">", nil
}

func (b *mergeForwardBuilder) buildRootBundleFromGatewayMessage(ctx context.Context, message *gatewayMessage) (*forwardedChatNode, error) {
	if message == nil || message.Deleted {
		return nil, fmt.Errorf("empty gateway message")
	}
	root := &forwardedChatNode{
		Kind:     "bundle",
		BundleID: b.nextBundleID(),
		Title:    mergeForwardTitle(message.Content),
	}
	if len(message.Children) == 0 {
		items, err := b.buildNodesFromRawContent(strings.TrimSpace(message.Content))
		if err != nil {
			return nil, err
		}
		if len(items) == 1 && items[0].Kind == "bundle" {
			copied := items[0]
			if copied.BundleID == "" {
				copied.BundleID = b.nextBundleID()
			}
			return &copied, nil
		}
		root.Items = append(root.Items, items...)
	} else {
		for _, child := range message.Children {
			node, err := b.buildNodeFromGatewayMessage(ctx, child)
			if err != nil {
				root.Items = append(root.Items, unavailableForwardedChatNode(child, err))
				continue
			}
			root.Items = append(root.Items, node)
		}
	}
	if strings.TrimSpace(root.Title) == "" && len(root.Items) == 0 {
		return nil, fmt.Errorf("empty merge_forward content")
	}
	return root, nil
}

func (b *mergeForwardBuilder) buildRootBundleFromRawContent(rawContent string) (*forwardedChatNode, error) {
	items, err := b.buildNodesFromRawContent(rawContent)
	if err != nil {
		return nil, err
	}
	root := &forwardedChatNode{
		Kind:     "bundle",
		BundleID: b.nextBundleID(),
		Items:    append([]forwardedChatNode{}, items...),
	}
	if len(root.Items) == 1 && root.Items[0].Kind == "bundle" {
		copied := root.Items[0]
		if copied.BundleID == "" {
			copied.BundleID = b.nextBundleID()
		}
		return &copied, nil
	}
	return root, nil
}

func (b *mergeForwardBuilder) buildNodeFromGatewayMessage(ctx context.Context, message *gatewayMessage) (forwardedChatNode, error) {
	if message == nil || message.Deleted {
		return forwardedChatNode{}, fmt.Errorf("empty gateway message")
	}
	sender := forwardedChatSenderFromGatewayMessage(message)
	switch strings.ToLower(strings.TrimSpace(message.MessageType)) {
	case "text":
		text, err := parseTextContent(message.Content)
		if err != nil {
			return forwardedChatNode{}, err
		}
		return forwardedChatNode{
			Kind:        "message",
			MessageID:   strings.TrimSpace(message.MessageID),
			Sender:      sender,
			MessageType: "text",
			Text:        strings.TrimSpace(text),
		}, nil
	case "post":
		inputs, text, err := b.gateway.parsePostInputs(ctx, message.MessageID, message.Content)
		if err != nil {
			return forwardedChatNode{}, err
		}
		imageRefs := b.consumeImageInputs(inputs)
		return forwardedChatNode{
			Kind:        "message",
			MessageID:   strings.TrimSpace(message.MessageID),
			Sender:      sender,
			MessageType: "post",
			Text:        strings.TrimSpace(text),
			ImageRefs:   imageRefs,
		}, nil
	case "image":
		imageKey, err := parseImageKey(message.Content)
		if err != nil {
			return forwardedChatNode{
				Kind:        "message",
				MessageID:   strings.TrimSpace(message.MessageID),
				Sender:      sender,
				MessageType: "image",
				DisplayText: "[图片不可用]",
				State:       "unavailable",
			}, nil
		}
		path, mimeType, err := b.gateway.downloadImageFn(ctx, message.MessageID, imageKey)
		if err != nil {
			return forwardedChatNode{
				Kind:        "message",
				MessageID:   strings.TrimSpace(message.MessageID),
				Sender:      sender,
				MessageType: "image",
				DisplayText: "[图片不可用]",
				State:       "unavailable",
			}, nil
		}
		ref := b.appendImageInput(agentproto.Input{Type: agentproto.InputLocalImage, Path: path, MIMEType: mimeType})
		return forwardedChatNode{
			Kind:        "message",
			MessageID:   strings.TrimSpace(message.MessageID),
			Sender:      sender,
			MessageType: "image",
			DisplayText: "[图片]",
			ImageRefs:   []string{ref},
		}, nil
	case "file":
		name := strings.TrimSpace(parseFileName(message.Content))
		displayText := "[文件]"
		if name != "" {
			displayText = name
		}
		return forwardedChatNode{
			Kind:        "message",
			MessageID:   strings.TrimSpace(message.MessageID),
			Sender:      sender,
			MessageType: "file",
			DisplayText: displayText,
		}, nil
	case "merge_forward":
		return *b.mustBuildNestedBundle(ctx, message), nil
	default:
		return forwardedChatNode{
			Kind:        "message",
			MessageID:   strings.TrimSpace(message.MessageID),
			Sender:      sender,
			MessageType: "unsupported",
			DisplayText: firstNonEmpty(strings.TrimSpace(message.MessageType), "unsupported"),
			State:       "unavailable",
		}, nil
	}
}

func (b *mergeForwardBuilder) mustBuildNestedBundle(ctx context.Context, message *gatewayMessage) *forwardedChatNode {
	node, err := b.buildRootBundleFromGatewayMessage(ctx, message)
	if err != nil {
		placeholder := unavailableForwardedChatNode(message, err)
		return &placeholder
	}
	return node
}

func (b *mergeForwardBuilder) buildNodesFromRawContent(rawContent string) ([]forwardedChatNode, error) {
	rawContent = strings.TrimSpace(rawContent)
	if rawContent == "" {
		return nil, fmt.Errorf("empty merge_forward content")
	}
	if !looksLikeJSONObject(rawContent) {
		return []forwardedChatNode{{
			Kind:        "message",
			MessageType: "text",
			Text:        rawContent,
		}}, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(rawContent), &decoded); err != nil {
		return nil, err
	}
	nodes := b.buildNodesFromRawValue(decoded)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("empty merge_forward content")
	}
	return nodes, nil
}

func (b *mergeForwardBuilder) buildNodesFromRawValue(value any) []forwardedChatNode {
	switch current := value.(type) {
	case []any:
		nodes := make([]forwardedChatNode, 0, len(current))
		for _, item := range current {
			nodes = append(nodes, b.buildNodesFromRawValue(item)...)
		}
		return nodes
	case map[string]any:
		return b.buildNodesFromRawMap(current)
	case string:
		text := strings.TrimSpace(current)
		if text == "" {
			return nil
		}
		if looksLikeJSONObject(text) {
			var nested any
			if err := json.Unmarshal([]byte(text), &nested); err == nil {
				return b.buildNodesFromRawValue(nested)
			}
		}
		return []forwardedChatNode{{
			Kind:        "message",
			MessageType: "text",
			Text:        text,
		}}
	default:
		return nil
	}
}

func (b *mergeForwardBuilder) buildNodesFromRawMap(values map[string]any) []forwardedChatNode {
	title := firstJSONString(values, "title", "topic", "chat_name", "chat_title")
	speaker := firstJSONString(values, "sender_name", "user_name", "name", "from_name", "sender")
	text := firstJSONString(values, "text", "message", "summary", "description", "desc")
	if text == "" {
		content := strings.TrimSpace(firstJSONString(values, "content"))
		if content != "" && !looksLikeJSONObject(content) {
			text = content
		}
	}

	if imageNode := b.imageNodeFromRawMap(values, speaker); imageNode != nil {
		return []forwardedChatNode{*imageNode}
	}
	if fileNode := rawFileNode(values, speaker); fileNode != nil {
		return []forwardedChatNode{*fileNode}
	}

	childNodes := make([]forwardedChatNode, 0, 4)
	for _, key := range []string{"items", "messages", "message_list", "children"} {
		if child, ok := values[key]; ok {
			childNodes = append(childNodes, b.buildNodesFromRawValue(child)...)
		}
	}
	if child, ok := values["content"]; ok {
		switch typed := child.(type) {
		case []any, map[string]any:
			childNodes = append(childNodes, b.buildNodesFromRawValue(typed)...)
		case string:
			if looksLikeJSONObject(strings.TrimSpace(typed)) {
				childNodes = append(childNodes, b.buildNodesFromRawValue(typed)...)
			}
		}
	}
	for key, child := range values {
		switch key {
		case "title", "topic", "chat_name", "chat_title",
			"sender_name", "user_name", "name", "from_name", "sender",
			"text", "message", "summary", "description", "desc",
			"content", "items", "messages", "message_list", "children",
			"message_id_list", "image_key", "file_name", "file_key", "tag":
			continue
		}
		childNodes = append(childNodes, b.buildNodesFromRawValue(child)...)
	}

	if len(childNodes) > 0 || strings.TrimSpace(title) != "" {
		node := forwardedChatNode{
			Kind:     "bundle",
			BundleID: b.nextBundleID(),
			Title:    strings.TrimSpace(title),
		}
		if text != "" {
			node.Items = append(node.Items, rawTextNode(text, speaker))
		} else {
			for _, line := range linesFromMessageIDs(values) {
				node.Items = append(node.Items, forwardedChatNode{
					Kind:        "message",
					MessageType: "unsupported",
					DisplayText: strings.TrimSpace(line),
					State:       "unavailable",
				})
			}
		}
		node.Items = append(node.Items, childNodes...)
		return []forwardedChatNode{node}
	}
	if text != "" {
		return []forwardedChatNode{rawTextNode(text, speaker)}
	}
	if lines := linesFromMessageIDs(values); len(lines) > 0 {
		nodes := make([]forwardedChatNode, 0, len(lines))
		for _, line := range lines {
			nodes = append(nodes, forwardedChatNode{
				Kind:        "message",
				MessageType: "unsupported",
				DisplayText: strings.TrimSpace(line),
				State:       "unavailable",
			})
		}
		return nodes
	}
	return nil
}

func (b *mergeForwardBuilder) imageNodeFromRawMap(values map[string]any, speaker string) *forwardedChatNode {
	imageKey := strings.TrimSpace(firstJSONString(values, "image_key"))
	tag := strings.ToLower(strings.TrimSpace(firstJSONString(values, "tag")))
	if imageKey == "" && tag != "img" && tag != "media" {
		return nil
	}
	node := forwardedChatNode{
		Kind:        "message",
		MessageType: "image",
		DisplayText: "[图片]",
	}
	if speaker = strings.TrimSpace(speaker); speaker != "" {
		node.Sender = &forwardedChatSender{Label: speaker}
	}
	return &node
}

func rawFileNode(values map[string]any, speaker string) *forwardedChatNode {
	fileName := strings.TrimSpace(firstJSONString(values, "file_name"))
	if fileName == "" && strings.TrimSpace(firstJSONString(values, "file_key")) != "" {
		fileName = strings.TrimSpace(firstJSONString(values, "name"))
	}
	if fileName == "" || strings.EqualFold(strings.TrimSpace(firstJSONString(values, "tag")), "img") {
		return nil
	}
	node := forwardedChatNode{
		Kind:        "message",
		MessageType: "file",
		DisplayText: fileName,
	}
	if speaker = strings.TrimSpace(speaker); speaker != "" {
		node.Sender = &forwardedChatSender{Label: speaker}
	}
	return &node
}

func rawTextNode(text, speaker string) forwardedChatNode {
	node := forwardedChatNode{
		Kind:        "message",
		MessageType: "text",
		Text:        strings.TrimSpace(text),
	}
	if speaker = strings.TrimSpace(speaker); speaker != "" {
		node.Sender = &forwardedChatSender{Label: speaker}
	}
	return node
}

func unavailableForwardedChatNode(message *gatewayMessage, err error) forwardedChatNode {
	displayText := "[内容不可用]"
	if message != nil {
		switch strings.ToLower(strings.TrimSpace(message.MessageType)) {
		case "image":
			displayText = "[图片不可用]"
		case "file":
			displayText = firstNonEmpty(strings.TrimSpace(parseFileName(message.Content)), "[文件不可用]")
		case "merge_forward":
			displayText = "[转发聊天记录不可用]"
		case "post":
			displayText = "[图文内容不可用]"
		}
	}
	node := forwardedChatNode{
		Kind:        "message",
		MessageID:   strings.TrimSpace(messageIDValue(message)),
		MessageType: "unsupported",
		DisplayText: displayText,
		State:       "unavailable",
	}
	if message != nil {
		node.Sender = forwardedChatSenderFromGatewayMessage(message)
	}
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		node.DisplayText = firstNonEmpty(node.DisplayText, "[内容不可用]")
	}
	return node
}

func messageIDValue(message *gatewayMessage) string {
	if message == nil {
		return ""
	}
	return message.MessageID
}

func forwardedChatSenderFromGatewayMessage(message *gatewayMessage) *forwardedChatSender {
	if message == nil {
		return nil
	}
	sender := &forwardedChatSender{
		ID:    strings.TrimSpace(message.SenderID),
		Type:  strings.ToLower(strings.TrimSpace(message.SenderType)),
		Label: gatewayMessageSpeakerLabel(message),
	}
	if sender.ID == "" && sender.Type == "" && sender.Label == "" {
		return nil
	}
	return sender
}

func (b *mergeForwardBuilder) consumeImageInputs(inputs []agentproto.Input) []string {
	refs := make([]string, 0, len(inputs))
	for _, input := range inputs {
		switch input.Type {
		case agentproto.InputLocalImage, agentproto.InputRemoteImage:
			refs = append(refs, b.appendImageInput(input))
		}
	}
	return refs
}

func (b *mergeForwardBuilder) appendImageInput(input agentproto.Input) string {
	b.nextImage++
	ref := fmt.Sprintf("img_%03d", b.nextImage)
	b.imageAssets = append(b.imageAssets, forwardedChatImageAsset{
		Ref:      ref,
		MIMEType: strings.TrimSpace(input.MIMEType),
	})
	b.imageInputs = append(b.imageInputs, input)
	return ref
}

func (b *mergeForwardBuilder) nextBundleID() string {
	b.nextBundle++
	return fmt.Sprintf("bundle_%03d", b.nextBundle)
}

func forwardedChatSummaryText(root forwardedChatNode) string {
	lines := make([]string, 0, 8)
	collectForwardedChatSummaryLines(root, &lines)
	return strings.Join(lines, "\n")
}

func collectForwardedChatSummaryLines(node forwardedChatNode, lines *[]string) {
	switch node.Kind {
	case "bundle":
		if title := strings.TrimSpace(node.Title); title != "" {
			*lines = append(*lines, title)
		}
		for _, child := range node.Items {
			collectForwardedChatSummaryLines(child, lines)
		}
	case "message":
		text := strings.TrimSpace(node.Text)
		if text == "" {
			text = strings.TrimSpace(node.DisplayText)
		}
		if text == "" && len(node.ImageRefs) > 0 {
			text = "[图片]"
		}
		if text == "" {
			return
		}
		label := ""
		if node.Sender != nil {
			label = strings.TrimSpace(firstNonEmpty(node.Sender.Label, node.Sender.ID))
		}
		if label != "" && text != "" {
			*lines = append(*lines, label+": "+text)
			return
		}
		*lines = append(*lines, text)
	}
}
