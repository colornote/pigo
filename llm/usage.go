package llm

// Usage tracks token consumption from API calls
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// Cache-specific fields (DeepSeek context caching)
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

// ModelPricing maps model names to per-token pricing (USD)
type ModelPricing struct {
	InputPrice  float64 // per 1M tokens
	OutputPrice float64 // per 1M tokens
	CacheHit    float64 // per 1M tokens (cached input)
}

// ContextWindow maps model names to max context window size
var ModelContextWindows = map[string]int{
	"deepseek-v4-flash":    128_000,
	"deepseek-chat":        128_000,
	"deepseek-reasoner":    128_000,
	"deepseek-v4-pro":     1_000_000,
	"deepseek-v4-pro[1m]": 1_000_000,
}

// Pricing for DeepSeek models (USD per 1M tokens)
var ModelPrices = map[string]ModelPricing{
	"deepseek-v4-flash": {
		InputPrice:  0.14,
		OutputPrice: 0.28,
		CacheHit:    0.014,
	},
	"deepseek-v4-pro": {
		InputPrice:  0.14,
		OutputPrice: 0.28,
		CacheHit:    0.014,
	},
	"deepseek-v4-pro[1m]": {
		InputPrice:  0.14,
		OutputPrice: 0.28,
		CacheHit:    0.014,
	},
	"deepseek-chat": {
		InputPrice:  0.27,
		OutputPrice: 1.10,
		CacheHit:    0.07,
	},
	"deepseek-reasoner": {
		InputPrice:  0.55,
		OutputPrice: 2.19,
		CacheHit:    0.14,
	},
}

// GetContextWindow returns the max context window for a model
func GetContextWindow(model string) int {
	if w, ok := ModelContextWindows[model]; ok {
		return w
	}
	return 128_000 // default
}

// GetPricing returns the pricing for a model
func GetPricing(model string) ModelPricing {
	if p, ok := ModelPrices[model]; ok {
		return p
	}
	// Default to flash pricing
	return ModelPrices["deepseek-v4-flash"]
}

// CostUSD estimates the cost in USD
func (u *Usage) CostUSD(model string) float64 {
	if u == nil {
		return 0
	}
	p := GetPricing(model)
	// Input: cache hits are cheaper
	inputCost := float64(u.InputTokens-u.CacheHitTokens)/1_000_000.0*p.InputPrice +
		float64(u.CacheHitTokens)/1_000_000.0*p.CacheHit
	outputCost := float64(u.OutputTokens) / 1_000_000.0 * p.OutputPrice
	return inputCost + outputCost
}
