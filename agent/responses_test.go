package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"pigo/llm"
)

// TestMessagesToResponses verifies the internal message list converts to
// Responses-API input items: user text → message items, tool results →
// function_call_output, assistant replies → message + function_call items.
func TestMessagesToResponses(t *testing.T) {
	a := &Agent{}
	a.messages = []llm.Message{
		{
			Role:    "user",
			Content: []llm.TextContent{{Type: "text", Text: "list files"}},
		},
		{
			Role: "assistant",
			Content: []interface{}{
				llm.TextContent{Type: "text", Text: "Running bash..."},
				llm.ToolUseContent{Type: "tool_use", ID: "call_1", Name: "bash", Input: map[string]interface{}{"cmd": "ls"}},
			},
		},
		{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{
					"type": "tool_result", "tool_use_id": "call_1", "content": "file.txt",
				},
				llm.TextContent{Type: "text", Text: "and now?"},
			},
		},
	}
	items := a.messagesToResponses()
	var kinds []string
	for _, it := range items {
		kinds = append(kinds, it.Type)
	}
	want := []string{"message", "message", "function_call", "function_call_output", "message"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("item kinds: got %v, want %v", kinds, want)
	}
	// First user message: plain string content.
	if items[0].Role != "user" || items[0].Content != "list files" {
		t.Errorf("first item: %#v", items[0])
	}
	// Assistant message: output_text blocks.
	blocks, ok := items[1].Content.([]llm.RSContentBlock)
	if !ok || len(blocks) != 1 || blocks[0].Text != "Running bash..." {
		t.Errorf("assistant blocks: %#v", items[1].Content)
	}
	// Function call item carries id + name + JSON arguments.
	fc := items[2]
	if fc.Type != "function_call" || fc.CallID != "call_1" || fc.Name != "bash" {
		t.Errorf("function_call item: %#v", fc)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(fc.Arguments), &args); err != nil || args["cmd"] != "ls" {
		t.Errorf("function_call arguments: %q", fc.Arguments)
	}
	// Tool result → function_call_output.
	if items[3].Type != "function_call_output" || items[3].CallID != "call_1" || items[3].Output != "file.txt" {
		t.Errorf("function_call_output item: %#v", items[3])
	}
	// Trailing user text stays a separate message item.
	if items[4].Role != "user" || items[4].Content != "and now?" {
		t.Errorf("last item: %#v", items[4])
	}
}

// TestMessagesToResponsesToolResultBlocks verifies content-block tool
// results collapse to their text portion.
func TestMessagesToResponsesToolResultBlocks(t *testing.T) {
	a := &Agent{}
	a.messages = []llm.Message{
		{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{
					"type": "tool_result", "tool_use_id": "t1",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "stdout line"},
						map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": "x"}},
					},
				},
			},
		},
	}
	items := a.messagesToResponses()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %#v", items)
	}
	it := items[0]
	if it.Type != "function_call_output" || it.Output != "stdout line" {
		t.Errorf("expected text-only output, got %#v", it)
	}
}

// TestProtocolResolution verifies the protocol switch covers responses.
func TestProtocolResolution(t *testing.T) {
	a := New(newTestConfig(t.TempDir(), ""))
	if a.protocol() != "anthropic" {
		t.Errorf("deepseek provider should default to anthropic, got %q", a.protocol())
	}
	if a.useOpenAIProtocol() {
		t.Error("deepseek provider should not use OpenAI protocol")
	}
	a.SwitchProvider("deepseek-responses")
	if a.protocol() != "responses" {
		t.Errorf("deepseek-responses should use responses protocol, got %q", a.protocol())
	}
	if a.useOpenAIProtocol() {
		t.Error("responses protocol should not report useOpenAIProtocol")
	}
	a.SwitchProvider("opencode-go")
	if a.protocol() != "openai" {
		t.Errorf("opencode-go should use openai protocol, got %q", a.protocol())
	}
	if !a.useOpenAIProtocol() {
		t.Error("opencode-go should report useOpenAIProtocol")
	}
}
