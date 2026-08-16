package singleturn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testImage() Image {
	return Image{
		ID:       "img1",
		Data:     []byte("fake-image-bytes"),
		MIMEType: "image/png",
	}
}

func decodeRequestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return payload
}

func TestProviderRejectsUnsupportedProtocol(t *testing.T) {
	if _, err := NewProvider("unknown", Config{BaseURL: "http://x"}); err == nil {
		t.Fatal("expected unsupported protocol error")
	}
	if _, err := NewProvider(ProtocolOpenAIChat, Config{}); err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestOpenAIChatProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		payload := decodeRequestBody(t, r)
		messages := payload["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("expected text + image content, got %#v", content)
		}
		imageURL := content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
		if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
			t.Fatalf("unexpected image url: %q", imageURL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" 图片里有报错信息  "}}]}`))
	}))
	defer server.Close()

	provider, err := NewProvider(ProtocolOpenAIChat, Config{BaseURL: server.URL, APIKey: "test-key", Model: "vision-model"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	text, err := provider.Complete(context.Background(), Request{
		Messages: []Message{{Text: "看看这张图", Images: []Image{testImage()}}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if text != "图片里有报错信息" {
		t.Fatalf("unexpected text %q", text)
	}
}

func TestOpenAIResponsesProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		payload := decodeRequestBody(t, r)
		input := payload["input"].([]any)
		content := input[0].(map[string]any)["content"].([]any)
		image := content[1].(map[string]any)
		if image["type"] != "input_image" || !strings.HasPrefix(image["image_url"].(string), "data:image/png;base64,") {
			t.Fatalf("unexpected image content %#v", image)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output_text":"responses 分析结果"}`))
	}))
	defer server.Close()

	provider, err := NewProvider(ProtocolOpenAIResponses, Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	text, err := provider.Complete(context.Background(), Request{
		Model:    "vision-model",
		Messages: []Message{{Text: "分析", Images: []Image{testImage()}}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if text != "responses 分析结果" {
		t.Fatalf("unexpected text %q", text)
	}
}

func TestAnthropicProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("anthropic-version = %q", got)
		}
		payload := decodeRequestBody(t, r)
		messages := payload["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		image := content[1].(map[string]any)
		source := image["source"].(map[string]any)
		if source["media_type"] != "image/png" {
			t.Fatalf("unexpected source %#v", source)
		}
		decoded, err := base64.StdEncoding.DecodeString(source["data"].(string))
		if err != nil || string(decoded) != "fake-image-bytes" {
			t.Fatalf("unexpected base64 data: %v %q", err, source["data"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"claude 分析"}]}`))
	}))
	defer server.Close()

	provider, err := NewProvider(ProtocolAnthropic, Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	text, err := provider.Complete(context.Background(), Request{
		Model:    "claude-vision",
		Messages: []Message{{Text: "分析", Images: []Image{testImage()}}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if text != "claude 分析" {
		t.Fatalf("unexpected text %q", text)
	}
}

func TestGeminiProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-vision:generateContent" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		payload := decodeRequestBody(t, r)
		contents := payload["contents"].([]any)
		parts := contents[0].(map[string]any)["parts"].([]any)
		inline := parts[1].(map[string]any)["inline_data"].(map[string]any)
		if inline["mime_type"] != "image/png" {
			t.Fatalf("unexpected inline_data %#v", inline)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"gemini 分析"}]}}]}`))
	}))
	defer server.Close()

	provider, err := NewProvider(ProtocolGemini, Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	text, err := provider.Complete(context.Background(), Request{
		Model:    "gemini-vision",
		Messages: []Message{{Text: "分析", Images: []Image{testImage()}}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if text != "gemini 分析" {
		t.Fatalf("unexpected text %q", text)
	}
}

func TestProviderReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad model"}}`))
	}))
	defer server.Close()

	provider, err := NewProvider(ProtocolOpenAIChat, Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	_, err = provider.Complete(context.Background(), Request{
		Model:    "vision-model",
		Messages: []Message{{Text: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "bad model") {
		t.Fatalf("expected error with upstream message, got %v", err)
	}
}
