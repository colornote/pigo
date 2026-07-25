package llm

import (
	"bufio"
	"bytes"
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
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
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
}

type ContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
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
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type Client struct {
	apiKey    string
	baseURL   string
	model     string
	http      *http.Client
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
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
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

	return &result, nil
}

func (c *Client) SendStream(req *Request, onText func(string), onToolStart func(string, string)) (*Response, error) {
	req.Stream = true
	if os.Getenv("PIGO_DEBUG") == "1" {
		dbg, _ := json.MarshalIndent(req, "", "  ")
		fmt.Fprintf(os.Stderr, "\n[REQUEST]\n%s\n", string(dbg))
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
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
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// Handle stream end
		if data == "[DONE]" {
			break
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
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

// addUsage accumulates usage
func addUsage(total, delta Usage) Usage {
	total.InputTokens += delta.InputTokens
	total.OutputTokens += delta.OutputTokens
	// Cache info from DeepSeek's anthropic-compatible API
	total.CacheHitTokens += delta.CacheHitTokens
	total.CacheMissTokens += delta.CacheMissTokens
	total.CacheWriteTokens += delta.CacheWriteTokens
	return total
}
