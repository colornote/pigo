package llm

// Usage tracks token consumption from API calls
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// Cache-specific fields (provider context caching)
	CacheHitTokens   int `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens  int `json:"cache_miss_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
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
