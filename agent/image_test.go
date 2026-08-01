package agent

import (
	"encoding/base64"
	"strings"
	"testing"

	"pigo/llm"
)

// TestToolResultContentPlainText verifies non-image tool results stay strings.
func TestToolResultContentPlainText(t *testing.T) {
	got := toolResultContent("just some text output")
	s, ok := got.(string)
	if !ok || s != "just some text output" {
		t.Errorf("expected plain string, got %#v", got)
	}
}

// TestToolResultContentImage verifies an image data URL becomes an
// Anthropic-style image content block (single-element array).
func TestToolResultContentImage(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	url := "data:image/png;base64," + b64
	got := toolResultContent(url)

	blocks, ok := got.([]interface{})
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected 1 content block, got %#v", got)
	}
	img, ok := blocks[0].(map[string]interface{})
	if !ok || img["type"] != "image" {
		t.Fatalf("expected image block, got %#v", blocks[0])
	}
	src := img["source"].(map[string]interface{})
	if src["media_type"] != "image/png" || src["data"] != b64 {
		t.Errorf("bad image source: %#v", src)
	}
}

// TestImageDataURLRejectsNonImages verifies only data:image/;base64, strings
// are treated as images.
func TestImageDataURLRejectsNonImages(t *testing.T) {
	for _, s := range []string{
		"plain text",
		"data:text/plain;base64,abc",
		"data:image/png,raw",
		"https://example.com/a.png",
	} {
		if _, _, ok := imageDataURL(s); ok {
			t.Errorf("expected %q to be rejected as an image", s)
		}
	}
}

// TestOpenAIImageBlocks verifies the Anthropic→OpenAI image conversion
// produces image_url blocks with a data: URL.
func TestOpenAIImageBlocks(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("bytes"))
	block := map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type": "base64", "media_type": "image/webp", "data": b64,
		},
	}
	blocks := openAIImageBlocks(block)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %#v", blocks)
	}
	iu, ok := blocks[0].(map[string]interface{})
	if !ok || iu["type"] != "image_url" {
		t.Fatalf("expected image_url block, got %#v", blocks[0])
	}
	url := iu["image_url"].(map[string]interface{})["url"].(string)
	if url != "data:image/webp;base64,"+b64 {
		t.Errorf("bad data URL: %q", url)
	}
}

// TestMessagesToOpenAIImageToolResult verifies the full OpenAI conversion:
// a user message whose tool_result holds an image block becomes a role=tool
// message with an image_url content array.
func TestMessagesToOpenAIImageToolResult(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("img"))
	a := &Agent{}
	a.messages = []llm.Message{
		{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{
					"type": "tool_result", "tool_use_id": "tool_1",
					"content": []interface{}{
						map[string]interface{}{
							"type": "image",
							"source": map[string]interface{}{
								"type": "base64", "media_type": "image/png", "data": b64,
							},
						},
					},
				},
			},
		},
	}
	out := a.messagesToOpenAI("sys")
	if len(out) != 2 {
		t.Fatalf("expected system + tool messages, got %d: %#v", len(out), out)
	}
	tool := out[1]
	if tool.Role != "tool" || tool.ToolCallID != "tool_1" {
		t.Errorf("bad tool message: %#v", tool)
	}
	blocks, ok := tool.Content.([]interface{})
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected content block array, got %#v", tool.Content)
	}
	iu := blocks[0].(map[string]interface{})
	if iu["type"] != "image_url" {
		t.Errorf("expected image_url block, got %#v", blocks[0])
	}
	if !strings.Contains(iu["image_url"].(map[string]interface{})["url"].(string), "data:image/png;base64,") {
		t.Errorf("bad image_url: %#v", iu)
	}
}
