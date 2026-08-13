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
)

// ─── DeepSeek Responses API types (POST {base}/v1/responses) ───
//
// The Responses API is the OpenAI-compatible protocol used by Codex.
// DeepSeek serves it at https://api.deepseek.com with the same request
// shape as OpenAI's /v1/responses (see
// https://api-docs.deepseek.com/zh-cn/guides/responses_api). Unsupported
// parameters are silently ignored by the server. Streaming returns
// semantic SSE events (each JSON carries a `type` field and an increasing
// `sequence_number`) and ends with response.completed / response.incomplete
// / response.failed — there is no data: [DONE] terminator.

// RSContentBlock is a content block inside a message item.
type RSContentBlock struct {
	Type string `json:"type"` // input_text / output_text
	Text string `json:"text,omitempty"`
}

// RSInputItem is one item of the request `input` array.
type RSInputItem struct {
	Type      string            `json:"type"` // message / function_call / function_call_output / reasoning / web_search_call
	Role      string            `json:"role,omitempty"`
	Content   interface{}       `json:"content,omitempty"` // string or []RSContentBlock
	CallID    string            `json:"call_id,omitempty"`
	Name      string            `json:"name,omitempty"`
	Arguments string            `json:"arguments,omitempty"`
	Output    string            `json:"output,omitempty"`
	Summary   interface{}       `json:"summary,omitempty"`
}

// RSTool is a tool definition in Responses API format. function and
// web_search are supported by DeepSeek; custom is accepted only for
// {"type":"custom","name":"apply_patch"} (Codex compatibility).
type RSTool struct {
	Type        string                 `json:"type"` // function / web_search / web_search_2025_08_26 / custom
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// RSReasoning controls thinking effort.
type RSReasoning struct {
	Effort string `json:"effort,omitempty"` // none / low / medium / high
}

// RSText controls the output text format.
type RSText struct {
	Format string `json:"format,omitempty"` // text / json_object / json_schema
}

// RSRequest is a request to /v1/responses. Fields not supported by
// DeepSeek are simply omitted — the server silently ignores them.
type RSRequest struct {
	Model           string       `json:"model"`
	Instructions    string       `json:"instructions,omitempty"`
	Input           interface{}  `json:"input,omitempty"` // string or []RSInputItem
	Stream          bool         `json:"stream"`
	MaxOutputTokens int          `json:"max_output_tokens,omitempty"`
	Temperature     *float64     `json:"temperature,omitempty"`
	TopP            *float64     `json:"top_p,omitempty"`
	Tools           []RSTool     `json:"tools,omitempty"`
	ToolChoice      interface{}  `json:"tool_choice,omitempty"`
	Reasoning       *RSReasoning `json:"reasoning,omitempty"`
	Text            *RSText      `json:"text,omitempty"`
	User            string       `json:"user,omitempty"`
}

// RSOutputItem is one item in the response `output` array (or in a
// streaming output_item event).
type RSOutputItem struct {
	Type      string           `json:"type"` // message / function_call / reasoning / web_search_call / ...
	ID        string           `json:"id,omitempty"`
	CallID    string           `json:"call_id,omitempty"`
	Role      string           `json:"role,omitempty"`
	Status    string           `json:"status,omitempty"`
	Name      string           `json:"name,omitempty"`
	Arguments string           `json:"arguments,omitempty"`
	Content   []RSContentBlock `json:"content,omitempty"`
	Output    []interface{}    `json:"output,omitempty"`
}

// RSUsage mirrors the Responses API usage object. input_tokens and
// output_tokens use the same field names as llm.Usage; the cache and
// reasoning details are nested.
type RSUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

// RSError is the error object carried by a failed response.
type RSError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
}

// RSResponse is a (non-streaming) response object from /v1/responses, and
// the payload of the terminal streaming events.
type RSResponse struct {
	ID        string         `json:"id"`
	Object    string         `json:"object"`
	CreatedAt int64          `json:"created_at"`
	Status    string         `json:"status"` // completed / incomplete / failed / in_progress
	Model     string         `json:"model"`
	Output    []RSOutputItem `json:"output"`
	Usage     *RSUsage       `json:"usage,omitempty"`
	Error     *RSError       `json:"error,omitempty"`
}

// RSToolCall is a finished function call assembled from a stream.
type RSToolCall struct {
	ID        string // item id
	CallID    string // function call id (usually equals ID)
	Name      string
	Arguments string // JSON string
}

// RSEvent is one streaming SSE event. Every event carries a `type` field
// and an increasing sequence_number; terminal events carry the full
// response object.
type RSEvent struct {
	Type           string        `json:"type"`
	SequenceNumber int           `json:"sequence_number"`
	Delta          string        `json:"delta,omitempty"`
	ItemID         string        `json:"item_id,omitempty"`
	OutputIndex    int           `json:"output_index,omitempty"`
	Item           *RSOutputItem `json:"item,omitempty"`
	Response       *RSResponse   `json:"response,omitempty"`
}

// Terminal streaming events: after one of these the stream is finished.
var rsTerminalEvents = map[string]bool{
	"response.completed":  true,
	"response.incomplete": true,
	"response.failed":     true,
}

// Text returns the concatenated output text of a response object.
func (r *RSResponse) Text() string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, block := range item.Content {
			if block.Type == "output_text" {
				parts = append(parts, block.Text)
			}
		}
	}
	return strings.Join(parts, "")
}

// ToolCalls returns the finished function calls of a response object.
func (r *RSResponse) ToolCalls() []RSToolCall {
	if r == nil {
		return nil
	}
	var calls []RSToolCall
	for _, item := range r.Output {
		if item.Type != "function_call" {
			continue
		}
		calls = append(calls, RSToolCall{
			ID:        item.ID,
			CallID:    item.CallID,
			Name:      item.Name,
			Arguments: item.Arguments,
		})
	}
	return calls
}

// rsUsageToUsage converts a Responses-API usage object into the canonical
// llm.Usage form (cache hits from input_tokens_details.cached_tokens).
// Reasoning tokens are already included in output_tokens by the API.
func rsUsageToUsage(rs *RSUsage) Usage {
	if rs == nil {
		return Usage{}
	}
	return Usage{
		InputTokens:    rs.InputTokens,
		OutputTokens:   rs.OutputTokens,
		CacheHitTokens: rs.InputTokensDetails.CachedTokens,
	}
}

// SendResponses sends a non-streaming request to /v1/responses and returns
// the response object (with usage accumulated into TotalUsage).
func (c *DeepSeekClient) SendResponses(ctx context.Context, req *RSRequest) (*RSResponse, error) {
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	var result RSResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	c.TotalUsage = addUsage(c.TotalUsage, rsUsageToUsage(result.Usage))
	return &result, nil
}

// SendResponsesStream sends a streaming request to /v1/responses and parses
// the semantic SSE events. Output text and reasoning deltas are routed to
// the callbacks in real time; finished function calls (id + name +
// concatenated arguments) and the terminal response object (which carries
// usage and, on failure, the error) are returned.
//
// The stream ends with response.completed / response.incomplete /
// response.failed — unlike chat/completions there is no data: [DONE].
func (c *DeepSeekClient) SendResponsesStream(ctx context.Context, req *RSRequest,
	onReasoning CoTCallback, onContent func(string)) (string, []RSToolCall, *RSResponse, error) {

	req.Stream = true
	if os.Getenv("PIGO_DEBUG") == "1" {
		dbg, _ := json.MarshalIndent(req, "", "  ")
		fmt.Fprintf(os.Stderr, "\n[RS REQUEST]\n%s\n", string(dbg))
		fmt.Fprintf(os.Stderr, "[ENDPOINT] %s\n", c.baseURL+"/v1/responses")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", nil, nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return "", nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", nil, nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", nil, nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	var fullContent strings.Builder
	var fullReasoning strings.Builder

	// Function-call assembly: deltas arrive per item id.
	type agg struct {
		id     string
		callID string
		name   string
		args   strings.Builder
	}
	aggs := map[string]*agg{}
	order := []string{}

	var finalResp *RSResponse
	var apiErr error

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// pendingEvent holds an `event: <type>` line awaiting its data line
	// (standard SSE). DeepSeek also embeds the type in the JSON payload;
	// the event line (when present) is used as a fallback.
	pendingEvent := ""

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			pendingEvent = ""
			continue
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			pendingEvent = strings.TrimPrefix(line, "event: ")
			continue
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var ev RSEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			// Fall back to the SSE event: line when the JSON lacks a type
			// (OpenAI-style SSE sends both).
			if ev.Type == "" && pendingEvent != "" {
				ev.Type = pendingEvent
			}
			if ev.Type == "" {
				continue
			}
			pendingEvent = ""

			switch {
			case ev.Type == "response.output_text.delta":
				if ev.Delta != "" {
					fullContent.WriteString(ev.Delta)
					if onContent != nil {
						onContent(ev.Delta)
					}
				}
			case ev.Type == "response.reasoning_text.delta":
				if ev.Delta != "" {
					fullReasoning.WriteString(ev.Delta)
					if onReasoning != nil {
						onReasoning(ev.Delta)
					}
				}
			case ev.Type == "response.output_item.added" && ev.Item != nil && ev.Item.Type == "function_call":
				key := ev.ItemID
				if key == "" {
					key = ev.Item.ID
				}
				if key == "" {
					key = fmt.Sprintf("idx%d", ev.OutputIndex)
				}
				if _, ok := aggs[key]; !ok {
					aggs[key] = &agg{id: ev.Item.ID, callID: ev.Item.CallID, name: ev.Item.Name}
					order = append(order, key)
				} else {
					aggs[key].id = ev.Item.ID
					aggs[key].callID = ev.Item.CallID
					aggs[key].name = ev.Item.Name
				}
			case ev.Type == "response.function_call_arguments.delta":
				key := ev.ItemID
				if key == "" {
					key = fmt.Sprintf("idx%d", ev.OutputIndex)
				}
				if a, ok := aggs[key]; ok {
					a.args.WriteString(ev.Delta)
				} else {
					// Arguments can arrive before/without output_item.added —
					// track by key anyway.
					aggs[key] = &agg{args: strings.Builder{}}
					aggs[key].args.WriteString(ev.Delta)
					order = append(order, key)
				}
			case ev.Type == "response.output_item.done" && ev.Item != nil && ev.Item.Type == "function_call":
				key := ev.ItemID
				if key == "" {
					key = ev.Item.ID
				}
				if a, ok := aggs[key]; ok {
					a.id = ev.Item.ID
					a.callID = ev.Item.CallID
					a.name = ev.Item.Name
					if ev.Item.Arguments != "" && a.args.Len() == 0 {
						a.args.WriteString(ev.Item.Arguments)
					}
				}
			case rsTerminalEvents[ev.Type]:
				finalResp = ev.Response
				if ev.Type == "response.failed" && ev.Response != nil && ev.Response.Error != nil {
					apiErr = fmt.Errorf("API error: %s (%s)", ev.Response.Error.Message, ev.Response.Error.Type)
				}
				if finalResp != nil && finalResp.Usage != nil {
					c.TotalUsage = addUsage(c.TotalUsage, rsUsageToUsage(finalResp.Usage))
				}
				// Terminal event — stream is over (no [DONE] sentinel).
				goto done
			}
		default:
			// Ignore bare keep-alive / comment lines.
		}
	}

done:
	if err := scanner.Err(); err != nil {
		return fullContent.String(), nil, nil, fmt.Errorf("scan: %w", err)
	}
	if apiErr != nil {
		return fullContent.String(), nil, finalResp, apiErr
	}

	// Assemble finished function calls in arrival order.
	var calls []RSToolCall
	for _, key := range order {
		a := aggs[key]
		if a.name == "" {
			continue
		}
		calls = append(calls, RSToolCall{
			ID:        a.id,
			CallID:    a.callID,
			Name:      a.name,
			Arguments: a.args.String(),
		})
	}
	_ = fullReasoning

	return fullContent.String(), calls, finalResp, nil
}
