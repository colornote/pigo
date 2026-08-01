package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Anthropic-compatible Messages API types

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolUseContent struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// InputSchema is the Anthropic-style tool schema (input_schema).
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
	// Parameters is the OpenAI-style tool schema (parameters). Some
	// Anthropic-compatible gateways (e.g. opencode.ai/zen/go) only accept
	// this form and reject input_schema with a 400.
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

type Request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
	Stream    bool      `json:"stream"`
}

type Response struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Model      string         `json:"model"`
	Usage      *Usage         `json:"usage,omitempty"`
}

type ContentBlock struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text,omitempty"`
	Thinking string                 `json:"thinking,omitempty"`
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Input    map[string]interface{} `json:"input,omitempty"`
}

type StreamEvent struct {
	Type         string        `json:"type"`
	Index        int           `json:"index"`
	Delta        *StreamDelta  `json:"delta,omitempty"`
	ContentBlock *ContentBlock `json:"content_block,omitempty"`
	Usage        *Usage        `json:"usage,omitempty"`
}

type StreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type Client struct {
	apiKey     string
	baseURL    string
	model      string
	http       *http.Client
	TotalUsage Usage // accumulated across requests
}

func New(apiKey, baseURL, model string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) Send(req *Request) (*Response, error) {
	return c.SendWithContext(context.Background(), req)
}

func (c *Client) SendWithContext(ctx context.Context, req *Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Track token usage from the response (non-streaming path).
	if result.Usage != nil {
		c.TotalUsage = addUsage(c.TotalUsage, *result.Usage)
	}

	return &result, nil
}

func (c *Client) SendStream(req *Request, onText func(string), onToolStart func(string, string), onThinking func(string)) (*Response, error) {
	return c.SendStreamWithContext(context.Background(), req, onText, onToolStart, onThinking)
}

func (c *Client) SendStreamWithContext(ctx context.Context, req *Request, onText func(string), onToolStart func(string, string), onThinking func(string)) (*Response, error) {
	req.Stream = true
	if os.Getenv("PIGO_DEBUG") == "1" {
		dbg, _ := json.MarshalIndent(req, "", "  ")
		fmt.Fprintf(os.Stderr, "\n[REQUEST]\n%s\n", string(dbg))
		fmt.Fprintf(os.Stderr, "[ENDPOINT] %s\n", c.baseURL+"/v1/messages")
		k := c.apiKey
		if len(k) > 8 {
			k = k[:4] + "…" + k[len(k)-4:]
		}
		fmt.Fprintf(os.Stderr, "[KEY] %s\n", k)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	var result Response
	result.Content = []ContentBlock{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	type currentToolState struct {
		id        string
		name      string
		inputJSON []string
	}
	var currentTool currentToolState

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Standard SSE: "data: {...}". Some Anthropic-compatible gateways
		// (opencode.ai/zen/go) emit bare JSON events instead, plus "{}"
		// keep-alive heartbeats — accept those too.
		data := line
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		// Skip keep-alive heartbeats ("{}" or events with no type).
		if event.Type == "" {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil {
				// Ensure content slice is large enough
				for len(result.Content) <= event.Index {
					result.Content = append(result.Content, ContentBlock{})
				}
				result.Content[event.Index] = *event.ContentBlock
				if event.ContentBlock.Type == "tool_use" {
					currentTool.id = event.ContentBlock.ID
					currentTool.name = event.ContentBlock.Name
					currentTool.inputJSON = nil
					if onToolStart != nil {
						onToolStart(event.ContentBlock.Name, event.ContentBlock.ID)
					}
				}
			}
		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta":
					if event.Index < len(result.Content) {
						result.Content[event.Index].Text += event.Delta.Text
					}
					if onText != nil {
						onText(event.Delta.Text)
					}
				case "thinking_delta":
					if event.Index < len(result.Content) {
						result.Content[event.Index].Thinking += event.Delta.Thinking
					}
					if onThinking != nil {
						onThinking(event.Delta.Thinking)
					}
				case "signature_delta":
					// signature — internal, don't display
				case "input_json_delta":
					currentTool.inputJSON = append(currentTool.inputJSON, event.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			if event.Index < len(result.Content) && result.Content[event.Index].Type == "tool_use" {
				if len(currentTool.inputJSON) > 0 {
					fullJSON := strings.Join(currentTool.inputJSON, "")
					var input map[string]interface{}
					if err := json.Unmarshal([]byte(fullJSON), &input); err == nil {
						result.Content[event.Index].Input = input
					}
					currentTool = currentToolState{}
				}
			}
		case "message_delta":
			if event.Delta != nil {
				if event.Delta.StopReason != "" {
					result.StopReason = event.Delta.StopReason
				}
			}
			// Some APIs put usage here
			if event.Usage != nil {
				c.TotalUsage = addUsage(c.TotalUsage, *event.Usage)
			}
		case "message_stop":
			// Capture usage from message_stop event
			if event.Usage != nil {
				c.TotalUsage = addUsage(c.TotalUsage, *event.Usage)
			}
		}
	}

	return &result, scanner.Err()
}

// addUsage accumulates usage. It normalizes the endpoint-specific field
// names (Anthropic: input_tokens/output_tokens; OpenAI: prompt_tokens/
// completion_tokens) and all three cache-naming conventions into the
// canonical InputTokens/OutputTokens/Cache* totals.
func addUsage(total, delta Usage) Usage {
	// Normalize token counts: prefer the Anthropic form, fall back to the
	// OpenAI form when the endpoint used prompt/completion naming.
	in, out := delta.InputTokens, delta.OutputTokens
	if in == 0 && delta.PromptTokens > 0 {
		in = delta.PromptTokens
	}
	if out == 0 && delta.CompletionTokens > 0 {
		out = delta.CompletionTokens
	}
	total.InputTokens += in
	total.OutputTokens += out
	// Cache info from all conventions (whichever endpoint sent).
	total.CacheHitTokens += delta.CacheHitTokens + delta.CacheReadTokens + delta.PromptCacheHitTokens
	total.CacheMissTokens += delta.CacheMissTokens + delta.PromptCacheMissTokens
	total.CacheWriteTokens += delta.CacheWriteTokens + delta.CacheCreationTokens + delta.PromptCacheWriteTokens
	return total
}
