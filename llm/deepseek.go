package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ─── DeepSeek Native API types (for reasoner CoT support) ───

// DSMessage is a message in DeepSeek's native format
type DSMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// DSRequest is a request to DeepSeek's native /v1/chat/completions
type DSRequest struct {
	Model    string      `json:"model"`
	Messages []DSMessage `json:"messages"`
	Stream   bool        `json:"stream"`
	MaxTokens int        `json:"max_tokens,omitempty"`
}

// DSDelta is the delta in a streaming chunk
type DSDelta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
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
// onReasoning is called for each reasoning_content chunk (the CoT chain).
// onContent is called for each normal content chunk.
func (c *DeepSeekClient) SendStream(req *DSRequest, onReasoning CoTCallback, onContent func(string)) (string, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
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
			if choice.Delta.ReasoningContent != "" {
				fullReasoning.WriteString(choice.Delta.ReasoningContent)
				if onReasoning != nil {
					onReasoning(choice.Delta.ReasoningContent)
				}
			}
			if choice.Delta.Content != "" {
				fullContent.WriteString(choice.Delta.Content)
				if onContent != nil {
					onContent(choice.Delta.Content)
				}
			}
		}

		// Capture usage from the final chunk
		if chunk.Usage != nil {
			c.TotalUsage.InputTokens += chunk.Usage.InputTokens
			c.TotalUsage.OutputTokens += chunk.Usage.OutputTokens
			c.TotalUsage.CacheHitTokens += chunk.Usage.CacheHitTokens
			c.TotalUsage.CacheMissTokens += chunk.Usage.CacheMissTokens
			c.TotalUsage.CacheWriteTokens += chunk.Usage.CacheWriteTokens
		}
	}

	if err := scanner.Err(); err != nil {
		return fullContent.String(), fmt.Errorf("scan: %w", err)
	}

	return fullContent.String(), nil
}
