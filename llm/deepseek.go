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

// ─── DeepSeek Native API types (for reasoner CoT support) ───

// DSTool is an OpenAI-style function tool definition.
type DSTool struct {
	Type     string         `json:"type"` // "function"
	Function DSToolFunction `json:"function"`
}

type DSToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// DSToolCall is a tool invocation returned by the model.
type DSToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"` // "function"
	Function DSFunctionCall `json:"function"`
}

type DSFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// DSMessage is a message in DeepSeek's native format. Content is a plain
// string (or a content-block array for multimodal messages — image_url
// blocks); tool interactions use ToolCallID / ToolCalls.
type DSMessage struct {
	Role             string       `json:"role"` // system/user/assistant/tool
	Content          interface{}  `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
	ToolCalls        []DSToolCall `json:"tool_calls,omitempty"`
}

// DSRequest is a request to DeepSeek's native /v1/chat/completions
type DSRequest struct {
	Model         string         `json:"model"`
	Messages      []DSMessage    `json:"messages"`
	Stream        bool           `json:"stream"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Tools         []DSTool       `json:"tools,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// StreamOptions mirrors OpenAI's stream_options. IncludeUsage requests the
// server to include a final usage chunk in streaming responses — without it,
// OpenAI-compatible gateways (opencode.ai/zen/go) omit usage entirely.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// DSToolCallDelta is a partial tool call inside a streaming delta.
type DSToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// DSDelta is the delta in a streaming chunk
type DSDelta struct {
	Role             string            `json:"role,omitempty"`
	Content          string            `json:"content,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCalls        []DSToolCallDelta `json:"tool_calls,omitempty"`
}

// DSChoice is a choice in the streaming response
type DSChoice struct {
	Index        int     `json:"index"`
	Delta        DSDelta `json:"delta"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

// DSStreamChunk is a single SSE chunk from DeepSeek
type DSStreamChunk struct {
	ID      string     `json:"id"`
	Object  string     `json:"object"`
	Created int64      `json:"created"`
	Model   string     `json:"model"`
	Choices []DSChoice `json:"choices"`
	Usage   *Usage     `json:"usage,omitempty"`
}

// DeepSeekClient uses the native DeepSeek API (supports reasoning_content / CoT)
type DeepSeekClient struct {
	apiKey     string
	baseURL    string // e.g. https://api.deepseek.com
	http       *http.Client
	TotalUsage Usage // accumulated across requests
}

func NewDeepSeekClient(apiKey, baseURL string) *DeepSeekClient {
	return &DeepSeekClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 300 * time.Second},
	}
}

// CoTCallback is called for reasoning (thinking) content chunks
type CoTCallback func(reasoning string)

// SendStream sends a streaming request and returns the final content.
func (c *DeepSeekClient) SendStream(req *DSRequest, onReasoning CoTCallback, onContent func(string)) (string, error) {
	return c.SendStreamWithContext(context.Background(), req, onReasoning, onContent)
}

// SendStreamWithTools is like SendStreamWithContext but also assembles
// OpenAI-style tool_calls from the stream (id + name + concatenated
// arguments per index). Content text and reasoning are streamed via the
// callbacks; the finished tool calls are returned with the content.
func (c *DeepSeekClient) SendStreamWithTools(ctx context.Context, req *DSRequest,
	onReasoning CoTCallback, onContent func(string)) (string, []DSToolCall, error) {
	req.Stream = true
	req.StreamOptions = &StreamOptions{IncludeUsage: true}
	if os.Getenv("PIGO_DEBUG") == "1" {
		dbg, _ := json.MarshalIndent(req, "", "  ")
		fmt.Fprintf(os.Stderr, "\n[DS REQUEST]\n%s\n", string(dbg))
		fmt.Fprintf(os.Stderr, "[ENDPOINT] %s\n", c.baseURL+"/v1/chat/completions")
		k := c.apiKey
		if len(k) > 8 {
			k = k[:4] + "…" + k[len(k)-4:]
		}
		fmt.Fprintf(os.Stderr, "[KEY] %s\n", k)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	var fullContent strings.Builder
	var fullReasoning strings.Builder
	var inThink bool   // true when we're inside an inline CoT tag pair
	var pending string // held-back bytes that may start a tag

	// Tool-call assembly: partial deltas arrive per index.
	type agg struct {
		id   string
		name string
		args strings.Builder
	}
	aggs := map[int]*agg{}
	order := []int{}

	finishReason := ""

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk DSStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			// Explicit reasoning_content field (native DeepSeek CoT)
			if choice.Delta.ReasoningContent != "" {
				fullReasoning.WriteString(choice.Delta.ReasoningContent)
				if onReasoning != nil {
					onReasoning(choice.Delta.ReasoningContent)
				}
			}

			// Regular content — may include inline CoT tags
			if choice.Delta.Content != "" {
				c.processDelta(choice.Delta.Content, &inThink, &pending,
					&fullReasoning, &fullContent,
					onReasoning, onContent)
			}

			// Tool call deltas
			for _, tc := range choice.Delta.ToolCalls {
				a, ok := aggs[tc.Index]
				if !ok {
					a = &agg{}
					aggs[tc.Index] = a
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					a.id = tc.ID
				}
				if tc.Function.Name != "" {
					a.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					a.args.WriteString(tc.Function.Arguments)
				}
			}
		}

		// Capture usage from the final chunk
		if chunk.Usage != nil {
			c.TotalUsage = addUsage(c.TotalUsage, *chunk.Usage)
		}
	}

	if err := scanner.Err(); err != nil {
		return fullContent.String(), nil, fmt.Errorf("scan: %w", err)
	}

	// Flush any held-back partial tag at end of stream.
	if pending != "" {
		if inThink {
			fullReasoning.WriteString(pending)
			if onReasoning != nil {
				onReasoning(pending)
			}
		} else {
			fullContent.WriteString(pending)
			if onContent != nil {
				onContent(pending)
			}
		}
	}

	// Assemble tool calls in index order (only complete ones with an id).
	var calls []DSToolCall
	for _, idx := range order {
		a := aggs[idx]
		if a.id == "" || a.name == "" {
			continue
		}
		calls = append(calls, DSToolCall{
			ID:   a.id,
			Type: "function",
			Function: DSFunctionCall{
				Name:      a.name,
				Arguments: a.args.String(),
			},
		})
	}
	_ = finishReason

	return fullContent.String(), calls, nil
}

// SendStreamWithContext is like SendStream but with context support for cancellation.
func (c *DeepSeekClient) SendStreamWithContext(ctx context.Context, req *DSRequest, onReasoning CoTCallback, onContent func(string)) (string, error) {
	req.Stream = true
	req.StreamOptions = &StreamOptions{IncludeUsage: true}
	if os.Getenv("PIGO_DEBUG") == "1" {
		dbg, _ := json.MarshalIndent(req, "", "  ")
		fmt.Fprintf(os.Stderr, "\n[DS REQUEST]\n%s\n", string(dbg))
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	var fullContent strings.Builder
	var fullReasoning strings.Builder
	var inThink bool   // true when we're inside an inline CoT tag pair
	var pending string // held-back bytes that may start a tag

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk DSStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			// Explicit reasoning_content field (native DeepSeek CoT)
			if choice.Delta.ReasoningContent != "" {
				fullReasoning.WriteString(choice.Delta.ReasoningContent)
				if onReasoning != nil {
					onReasoning(choice.Delta.ReasoningContent)
				}
			}

			// Regular content — may include inline CoT tags
			if choice.Delta.Content != "" {
				c.processDelta(choice.Delta.Content, &inThink, &pending,
					&fullReasoning, &fullContent,
					onReasoning, onContent)
			}
		}

		// Capture usage from the final chunk
		if chunk.Usage != nil {
			c.TotalUsage = addUsage(c.TotalUsage, *chunk.Usage)
		}
	}

	if err := scanner.Err(); err != nil {
		return fullContent.String(), fmt.Errorf("scan: %w", err)
	}

	// Flush any held-back partial tag at end of stream.
	if pending != "" {
		if inThink {
			fullReasoning.WriteString(pending)
			if onReasoning != nil {
				onReasoning(pending)
			}
		} else {
			fullContent.WriteString(pending)
			if onContent != nil {
				onContent(pending)
			}
		}
	}

	return fullContent.String(), nil
}

// Inline CoT tags. Some DeepSeek models emit reasoning by wrapping it in
// tag pairs inside the regular content field instead of using the
// dedicated reasoning_content field. Accept both Chinese and English forms.
var (
	cotOpenTags  = []string{" 回复", "<reply>", " 思考"}
	cotCloseTags = []string{" /回复", "</reply>", " /思考"}
)

// indexAny returns the earliest index of any tag in tags and the length of
// the matched tag, or (-1, 0) when nothing matches.
func indexAny(s string, tags []string) (int, int) {
	best, bestLen := -1, 0
	for _, t := range tags {
		if i := strings.Index(s, t); i >= 0 && (best < 0 || i < best) {
			best, bestLen = i, len(t)
		}
	}
	return best, bestLen
}

// processDelta handles inline CoT tags embedded in content deltas.
// Reasoning between an opening/closing tag pair is routed to the
// reasoning callback; everything else goes to the content callback.
// When no opening tag appears, all content is treated as a normal
// answer (never swallowed). Tags split across stream chunks are
// reassembled via the pending buffer.
func (c *DeepSeekClient) processDelta(delta string, inThink *bool, pending *string,
	reasoning, content *strings.Builder,
	onReasoning CoTCallback, onContent func(string)) {

	rem := *pending + delta
	*pending = ""

	for rem != "" {
		if *inThink {
			// Looking for a closing tag to end the thinking block.
			idx, tagLen := indexAny(rem, cotCloseTags)
			if idx < 0 {
				// No closing tag yet — emit reasoning, but hold back a
				// trailing partial tag prefix in case it's split mid-chunk.
				emit, held := splitPartialTag(rem, cotCloseTags)
				if emit != "" {
					reasoning.WriteString(emit)
					if onReasoning != nil {
						onReasoning(emit)
					}
				}
				*pending = held
				return
			}
			if idx > 0 {
				part := rem[:idx]
				reasoning.WriteString(part)
				if onReasoning != nil {
					onReasoning(part)
				}
			}
			*inThink = false
			rem = rem[idx+tagLen:]
			continue
		}

		// Not in think — look for an opening tag.
		idx, tagLen := indexAny(rem, cotOpenTags)
		if idx < 0 {
			// No opening tag in this chunk — it's regular answer content.
			// Hold back a trailing partial tag prefix if any.
			emit, held := splitPartialTag(rem, cotOpenTags)
			if emit != "" {
				content.WriteString(emit)
				if onContent != nil {
					onContent(emit)
				}
			}
			*pending = held
			return
		}
		if idx > 0 {
			part := rem[:idx]
			content.WriteString(part)
			if onContent != nil {
				onContent(part)
			}
		}
		*inThink = true
		rem = rem[idx+tagLen:]
	}
}

// splitPartialTag splits s into (emit, held): held is the longest suffix of
// s that is a proper prefix of any tag in tags (at least 2 bytes long), and
// emit is the rest. If no such suffix exists, held is "" and emit is s.
func splitPartialTag(s string, tags []string) (string, string) {
	best := 0
	for _, t := range tags {
		// Check suffixes of s of length 2..len(t)-1 against prefixes of t.
		maxL := len(t) - 1
		limit := maxL
		if limit > len(s) {
			limit = len(s)
		}
		for l := 2; l <= limit; l++ {
			if strings.HasPrefix(t, s[len(s)-l:]) && l > best {
				best = l
			}
		}
	}
	if best == 0 {
		return s, ""
	}
	return s[:len(s)-best], s[len(s)-best:]
}
