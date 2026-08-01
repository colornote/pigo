package llm

// ModelPricing lists per-1M-token prices in USD for a model.
type ModelPricing struct {
	InputPrice  float64 // per 1M input tokens
	OutputPrice float64 // per 1M output tokens
	CacheHit    float64 // per 1M cached-read tokens
	CacheWrite  float64 // per 1M cache-write tokens (0 when unsupported)
}

// ModelInfo describes one model in a provider's registry. It carries every
// property PiGo needs at runtime (context window, max tokens, pricing) so
// providers are the single source of truth — no global lookup tables.
type ModelInfo struct {
	ID            string       // canonical API model id (sent in requests)
	Name          string       // display name (footer, /models)
	Description   string       // short description shown in /models
	Reasoning     bool         // model supports thinking / chain-of-thought
	CoT           bool         // use the native OpenAI-compatible CoT path (PiGo runCoT, no tools)
	ContextWindow int          // max context window in tokens
	MaxTokens     int          // max output tokens the API accepts
	Pricing       ModelPricing // USD per 1M tokens
	// ToolsFormat overrides the provider-level tool schema style for this
	// model: "" (use provider default), "anthropic" (input_schema), or
	// "openai" (parameters). Some gateways route models to different
	// upstreams that each accept only one style (opencode.ai/zen/go).
	ToolsFormat string
}
