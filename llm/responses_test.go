package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseEvent renders one SSE data line (with an optional event: line, as the
// OpenAI-style protocol sends both).
func sseEvent(eventType, payload string) string {
	if eventType != "" {
		return "event: " + eventType + "\ndata: " + payload + "\n\n"
	}
	return "data: " + payload + "\n\n"
}

// TestSendResponsesStream assembles a full semantic SSE stream and checks
// text deltas, reasoning deltas, function-call assembly, usage
// accumulation, and that the stream ends without a [DONE] sentinel.
func TestSendResponsesStream(t *testing.T) {
	events := []struct{ typ, payload string }{
		{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"rsp_1","object":"response","status":"in_progress","model":"deepseek-v4-flash"}}`},
		{"response.in_progress", `{"type":"response.in_progress","sequence_number":1,"response":{"id":"rsp_1","status":"in_progress"}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":2,"output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":null}}`},
		{"response.reasoning_text.delta", `{"type":"response.reasoning_text.delta","sequence_number":3,"item_id":"rs_1","output_index":0,"delta":"Let me think"}`},
		{"response.reasoning_text.delta", `{"type":"response.reasoning_text.delta","sequence_number":4,"item_id":"rs_1","output_index":0,"delta":" carefully."}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":5,"output_index":0,"item":{"type":"reasoning","id":"rs_1","content":[{"type":"summary_text","text":"Let me think carefully."}]}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":6,"output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`},
		{"response.content_part.added", `{"type":"response.content_part.added","sequence_number":7,"item_id":"msg_1","output_index":1,"content_index":0,"part":{"type":"output_text","text":""}}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":8,"item_id":"msg_1","output_index":1,"delta":"Hello "}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":9,"item_id":"msg_1","output_index":1,"delta":"world"}`},
		{"response.output_text.done", `{"type":"response.output_text.done","sequence_number":10,"item_id":"msg_1","output_index":1,"text":"Hello world"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":11,"output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Hello world","annotations":[]}]}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":12,"output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash","arguments":""}}`},
		{"response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","sequence_number":13,"item_id":"fc_1","output_index":2,"delta":"{\"cmd\":"}`},
		{"response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","sequence_number":14,"item_id":"fc_1","output_index":2,"delta":"\"ls\"}"}`},
		{"response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","sequence_number":15,"item_id":"fc_1","output_index":2,"arguments":"{\"cmd\":\"ls\"}"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":16,"output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash","arguments":"{\"cmd\":\"ls\"}"}}`},
		{"response.completed", `{"type":"response.completed","sequence_number":17,"response":{"id":"rsp_1","object":"response","status":"completed","model":"deepseek-v4-flash","output":[{"type":"reasoning","id":"rs_1","status":"completed","content":[{"type":"reasoning_text","text":"Let me think carefully."}]},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Hello world"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash","arguments":"{\"cmd\":\"ls\"}"}],"usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":80},"output_tokens":45,"output_tokens_details":{"reasoning_tokens":20},"total_tokens":165}}}`},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth: got %q", got)
		}
		var req RSRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !req.Stream {
			t.Error("expected stream=true")
		}
		if req.Model != "deepseek-v4-flash" {
			t.Errorf("model: got %q", req.Model)
		}
		if req.Instructions != "You are a helpful assistant." {
			t.Errorf("instructions: got %q", req.Instructions)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			w.Write([]byte(sseEvent(e.typ, e.payload)))
		}
	}))
	defer srv.Close()

	c := NewDeepSeekClient("test-key", srv.URL)
	var reasoningBuf, content strings.Builder
	req := &RSRequest{
		Model:        "deepseek-v4-flash",
		Instructions: "You are a helpful assistant.",
		Input:        "Hi, how are you?",
	}
	text, reasoning, calls, final, err := c.SendResponsesStream(context.Background(), req,
		func(s string) { reasoningBuf.WriteString(s) },
		func(s string) { content.WriteString(s) },
	)
	if err != nil {
		t.Fatalf("SendResponsesStream: %v", err)
	}
	if text != "Hello world" {
		t.Errorf("text: got %q", text)
	}
	if reasoning != "Let me think carefully." {
		t.Errorf("reasoning: got %q", reasoning)
	}
	if content.String() != "Hello world" {
		t.Errorf("content stream: got %q", content.String())
	}
	if reasoningBuf.String() != "Let me think carefully." {
		t.Errorf("reasoning stream: got %q", reasoningBuf.String())
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 function call, got %d", len(calls))
	}
	call := calls[0]
	if call.ID != "fc_1" || call.CallID != "call_1" || call.Name != "bash" {
		t.Errorf("call: id=%q callID=%q name=%q", call.ID, call.CallID, call.Name)
	}
	if call.Arguments != `{"cmd":"ls"}` {
		t.Errorf("arguments: got %q", call.Arguments)
	}
	if final == nil || final.Status != "completed" || final.ID != "rsp_1" {
		t.Fatalf("final response: %+v", final)
	}
	// Usage accumulated from the terminal event.
	if c.TotalUsage.InputTokens != 120 {
		t.Errorf("input tokens: got %d", c.TotalUsage.InputTokens)
	}
	if c.TotalUsage.OutputTokens != 45 {
		t.Errorf("output tokens: got %d", c.TotalUsage.OutputTokens)
	}
	if c.TotalUsage.CacheHitTokens != 80 {
		t.Errorf("cache hit tokens: got %d", c.TotalUsage.CacheHitTokens)
	}
	// The non-streaming response helpers agree with the streamed result.
	if got := final.Text(); got != "Hello world" {
		t.Errorf("final.Text(): got %q", got)
	}
	if got := len(final.ToolCalls()); got != 1 {
		t.Errorf("final.ToolCalls(): got %d", got)
	}
	if got := final.Reasoning(); got != "Let me think carefully." {
		t.Errorf("final.Reasoning(): got %q", got)
	}
}

// TestSendResponsesStreamFailed verifies the response.failed terminal event
// surfaces the server error.
func TestSendResponsesStreamFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseEvent("response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"rsp_x","status":"in_progress"}}`)))
		w.Write([]byte(sseEvent("response.failed", `{"type":"response.failed","sequence_number":1,"response":{"id":"rsp_x","status":"failed","error":{"code":"api_error","message":"boom","type":"server_error"}}}`)))
	}))
	defer srv.Close()

	c := NewDeepSeekClient("test-key", srv.URL)
	_, _, _, final, err := c.SendResponsesStream(context.Background(), &RSRequest{Model: "m"}, nil, nil)
	if err == nil {
		t.Fatal("expected error from failed response")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should carry server message, got %v", err)
	}
	if final == nil || final.Status != "failed" {
		t.Errorf("final response: %+v", final)
	}
}

// TestSendResponsesStreamDataOnlyType verifies events that carry the type in
// the JSON payload (no event: line) are still parsed — the format DeepSeek
// documents (each event has a type field + sequence_number).
func TestSendResponsesStreamDataOnlyType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + `{"type":"response.output_text.delta","sequence_number":0,"delta":"data-only "}` + "\n\n"))
		w.Write([]byte("data: " + `{"type":"response.output_text.delta","sequence_number":1,"delta":"type works"}` + "\n\n"))
		w.Write([]byte("data: " + `{"type":"response.completed","sequence_number":2,"response":{"id":"r","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}}` + "\n\n"))
	}))
	defer srv.Close()

	c := NewDeepSeekClient("test-key", srv.URL)
	var got strings.Builder
	text, _, _, final, err := c.SendResponsesStream(context.Background(), &RSRequest{Model: "m"}, nil, func(s string) { got.WriteString(s) })
	if err != nil {
		t.Fatalf("SendResponsesStream: %v", err)
	}
	if text != "data-only type works" || got.String() != text {
		t.Errorf("text: got %q (streamed %q)", text, got.String())
	}
	if final == nil || final.Status != "completed" {
		t.Errorf("final: %+v", final)
	}
}

// TestSendResponsesNonStreaming verifies the non-streaming path.
func TestSendResponsesNonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path: %s", r.URL.Path)
		}
		var req RSRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.Stream {
			t.Error("non-streaming request should not set stream=true")
		}
		items, ok := req.Input.([]interface{})
		if !ok || len(items) != 1 {
			t.Errorf("input items: %#v", req.Input)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "rsp_9", "object": "response", "status": "completed", "model": "deepseek-v4-flash",
			"output": []map[string]interface{}{
				{"type": "message", "id": "msg_9", "role": "assistant",
					"content": []map[string]string{{"type": "output_text", "text": "42"}}},
			},
			"usage": map[string]interface{}{
				"input_tokens": 10, "output_tokens": 3, "total_tokens": 13,
				"input_tokens_details":  map[string]int{"cached_tokens": 6},
				"output_tokens_details": map[string]int{"reasoning_tokens": 1},
			},
		})
	}))
	defer srv.Close()

	c := NewDeepSeekClient("test-key", srv.URL)
	resp, err := c.SendResponses(context.Background(), &RSRequest{
		Model: "deepseek-v4-flash",
		Input: []RSInputItem{{Type: "message", Role: "user", Content: "what is 6*7?"}},
	})
	if err != nil {
		t.Fatalf("SendResponses: %v", err)
	}
	if resp.Text() != "42" {
		t.Errorf("text: got %q", resp.Text())
	}
	if c.TotalUsage.InputTokens != 10 || c.TotalUsage.OutputTokens != 3 || c.TotalUsage.CacheHitTokens != 6 {
		t.Errorf("usage: %+v", c.TotalUsage)
	}
}
