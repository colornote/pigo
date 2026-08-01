package llm

// Usage tracks token consumption from API calls.
//
// The struct accepts BOTH field-name conventions so the same accumulator
// works for Anthropic-compatible and OpenAI-compatible endpoints:
//
//	Anthropic-compatible:  input_tokens / output_tokens,
//	                       cache_read_input_tokens / cache_creation_input_tokens
//	OpenAI-compatible:     prompt_tokens / completion_tokens,
//	                       prompt_cache_hit_tokens / prompt_cache_miss_tokens
//
// addUsage() normalizes whichever form the endpoint sent into the canonical
// InputTokens / OutputTokens / Cache* fields before accumulating.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	// Cache-specific fields (provider context caching)
	CacheHitTokens   int `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens  int `json:"cache_miss_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	// OpenAI-compatible field names (prompt_tokens etc.)
	PromptTokens           int `json:"prompt_tokens,omitempty"`
	CompletionTokens       int `json:"completion_tokens,omitempty"`
	PromptCacheHitTokens   int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens  int `json:"prompt_cache_miss_tokens,omitempty"`
	PromptCacheWriteTokens int `json:"prompt_cache_write_tokens,omitempty"`
	// Anthropic-compatible cache field names
	CacheReadTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// Total returns total tokens
func (u *Usage) Total() int {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.OutputTokens
}

// CostUSD estimates the cost in USD using a model's pricing table.
func (u *Usage) CostUSD(info *ModelInfo) float64 {
	if u == nil || info == nil {
		return 0
	}
	p := info.Pricing
	// Input: cache hits are cheaper
	inputCost := float64(u.InputTokens-u.CacheHitTokens)/1_000_000.0*p.InputPrice +
		float64(u.CacheHitTokens)/1_000_000.0*p.CacheHit
	writeCost := float64(u.CacheWriteTokens) / 1_000_000.0 * p.CacheWrite
	outputCost := float64(u.OutputTokens) / 1_000_000.0 * p.OutputPrice
	return inputCost + writeCost + outputCost
}
