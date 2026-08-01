package agent

import (
	"strings"

	"pigo/llm"
)

// Provider describes a supported LLM provider for PiGo. Each provider owns
// its API key environment variable, endpoint URLs, and model registry, so
// adding a new provider is one entry here (plus config plumbing).
type Provider struct {
	ID        string // canonical id (also used by PIGO_PROVIDER and /provider)
	Name      string // display name (/login, banner)
	EnvKey    string // primary API key environment variable
	BaseURL   string // Anthropic-compatible endpoint root (client appends /v1/messages)
	DSBaseURL string // OpenAI-compatible endpoint root (native client appends /v1/chat/completions; key verification appends /v1/models)
	// DefaultModel is used when PIGO_MODEL is unset or names a model that
	// doesn't exist on the active provider.
	DefaultModel string
	// ToolsFormat is the tool-schema style sent to the API: ""/"anthropic"
	// uses input_schema (standard Anthropic), "openai" uses parameters
	// (required by some Anthropic-compatible gateways like opencode.ai/zen/go).
	ToolsFormat string
	Models      map[string]llm.ModelInfo // canonical id → info (aliases resolved via NormalizeModel)
}

// NormalizeModel resolves aliases to canonical API ids ("" → id unchanged).
func (p *Provider) NormalizeModel(name string) string {
	switch p.ID {
	case "deepseek":
		if name == "deepseek-v4-pro" {
			return "deepseek-v4-pro[1m]"
		}
	}
	return name
}

// HasModel reports whether id (after alias normalization) exists on this provider.
func (p *Provider) HasModel(name string) bool {
	_, ok := p.Models[p.NormalizeModel(name)]
	return ok
}

// Model returns the model info for id (after alias normalization), or nil.
func (p *Provider) Model(name string) *llm.ModelInfo {
	if info, ok := p.Models[p.NormalizeModel(name)]; ok {
		return &info
	}
	return nil
}

// Resolve returns the canonical model to use for a requested name: the
// requested model if it exists on this provider, otherwise the provider
// default. Returns the canonical id.
func (p *Provider) Resolve(name string) string {
	if name != "" && p.HasModel(name) {
		return p.NormalizeModel(name)
	}
	return p.DefaultModel
}

// SortedModelNames returns canonical model ids in deterministic order.
func (p *Provider) SortedModelNames() []string {
	names := make([]string, 0, len(p.Models))
	for name := range p.Models {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ─── Registered Providers ─────────────────────────────────────

// Providers lists all providers supported by PiGo (/login, /provider).
var Providers = []Provider{
	DeepSeekProvider,
	OpenCodeGoProvider,
}

// DeepSeekProvider is the original provider: DeepSeek's Anthropic-compatible
// endpoint for tool calling plus the native API for deepseek-reasoner CoT.
var DeepSeekProvider = Provider{
	ID:           "deepseek",
	Name:         "DeepSeek",
	EnvKey:       "DEEPSEEK_API_KEY",
	BaseURL:      "https://api.deepseek.com/anthropic",
	DSBaseURL:    "https://api.deepseek.com",
	DefaultModel: "deepseek-v4-flash",
	ToolsFormat:  "anthropic", // DeepSeek 官方端点只接受 Anthropic input_schema
	Models: map[string]llm.ModelInfo{
		"deepseek-v4-flash": {
			ID: "deepseek-v4-flash", Name: "V4 Flash", Description: "快速",
			Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 384_000,
			Pricing: llm.ModelPricing{InputPrice: 0.14, OutputPrice: 0.28, CacheHit: 0.014},
		},
		"deepseek-v4-pro[1m]": {
			ID: "deepseek-v4-pro[1m]", Name: "V4 Pro 1M", Description: "长上下文",
			Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 384_000,
			Pricing: llm.ModelPricing{InputPrice: 0.14, OutputPrice: 0.28, CacheHit: 0.014},
		},
		"deepseek-chat": {
			ID: "deepseek-chat", Name: "Chat", Description: "通用",
			Reasoning: true, ContextWindow: 128_000, MaxTokens: 8_192,
			Pricing: llm.ModelPricing{InputPrice: 0.27, OutputPrice: 1.10, CacheHit: 0.07},
		},
		"deepseek-reasoner": {
			ID: "deepseek-reasoner", Name: "Reasoner", Description: "深度推理",
			Reasoning: true, CoT: true, ContextWindow: 128_000, MaxTokens: 64_000,
			Pricing: llm.ModelPricing{InputPrice: 0.55, OutputPrice: 2.19, CacheHit: 0.14},
		},
	},
}

// OpenCodeGoProvider is OpenCode Go (https://opencode.ai/go) — a low-cost
// subscription for open coding models. Auth is a plain API key
// (OPENCODE_API_KEY, mirroring pi).
//
// The DeepSeek family is served through the OpenAI-compatible endpoint
// https://opencode.ai/zen/go/v1/chat/completions (per opencode docs) with
// OpenAI-format tool calling; hence ToolsFormat "openai".
var OpenCodeGoProvider = Provider{
	ID:           "opencode-go",
	Name:         "OpenCode Go",
	EnvKey:       "OPENCODE_API_KEY",
	BaseURL:      "https://opencode.ai/zen/go",
	DSBaseURL:    "https://opencode.ai/zen/go",
	DefaultModel: "deepseek-v4-flash",
	ToolsFormat:  "openai", // DeepSeek 系列只走 OpenAI 兼容端点（opencode 文档端点表）
	Models: map[string]llm.ModelInfo{
		"deepseek-v4-flash": {
			ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Description: "低价 · 大用量",
			Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 384_000,
			Pricing: llm.ModelPricing{InputPrice: 0.14, OutputPrice: 0.28, CacheHit: 0.0028},
		},
		"deepseek-v4-pro": {
			ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Description: "更强推理",
			Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 384_000,
			Pricing: llm.ModelPricing{InputPrice: 0.435, OutputPrice: 0.87, CacheHit: 0.003625},
		},
		// Xiaomi MiMo V2.5 — multimodal (vision): reads images via the read
		// tool (data URLs → image content blocks). Model id verified against
		// opencode.ai/zen/go/v1/models.
		"mimo-v2.5": {
			ID: "mimo-v2.5", Name: "MiMo V2.5", Description: "多模态 · 图像理解",
			Reasoning: true, Multimodal: true, ContextWindow: 128_000, MaxTokens: 16_384,
			Pricing: llm.ModelPricing{InputPrice: 0.14, OutputPrice: 0.28, CacheHit: 0.0028},
		},
		"mimo-v2.5-pro": {
			ID: "mimo-v2.5-pro", Name: "MiMo V2.5 Pro", Description: "多模态 · 更强推理",
			Reasoning: true, Multimodal: true, ContextWindow: 128_000, MaxTokens: 32_768,
			Pricing: llm.ModelPricing{InputPrice: 0.2, OutputPrice: 0.4, CacheHit: 0.004},
		},
	},
}

// ProviderByID returns the provider with the given ID, or nil.
func ProviderByID(id string) *Provider {
	for i := range Providers {
		if Providers[i].ID == id {
			return &Providers[i]
		}
	}
	return nil
}

// ProviderForModel returns the provider whose registry contains name.
// preferID (usually the active provider) wins when several providers match,
// which matters for aliases shared across providers (e.g. deepseek-v4-pro is
// both a deepseek alias and a real opencode-go model id). Exact canonical
// ids beat alias matches elsewhere.
func ProviderForModel(name, preferID string) *Provider {
	if preferID != "" {
		if p := ProviderByID(preferID); p != nil && p.HasModel(name) {
			return p
		}
	}
	// Exact canonical id match (no alias resolution).
	for i := range Providers {
		if _, ok := Providers[i].Models[name]; ok {
			return &Providers[i]
		}
	}
	// Alias-normalized match (e.g. deepseek-v4-pro → deepseek-v4-pro[1m]).
	for i := range Providers {
		normalized := Providers[i].NormalizeModel(name)
		if normalized != name {
			if _, ok := Providers[i].Models[normalized]; ok {
				return &Providers[i]
			}
		}
	}
	return nil
}

// IsKnownModel reports whether name exists on any provider.
func IsKnownModel(name string) bool {
	return ProviderForModel(name, "") != nil
}

// IsCoTModel reports whether name is a native-CoT model on any provider.
func IsCoTModel(name string) bool {
	if p := ProviderForModel(name, ""); p != nil {
		if info := p.Model(name); info != nil {
			return info.CoT
		}
	}
	return false
}

// ShortModelName returns a compact display alias for a model (prompt status bar).
func ShortModelName(model string) string {
	switch {
	case strings.Contains(model, "flash"):
		return "flash"
	case strings.Contains(model, "reasoner"):
		return "reasoner"
	case strings.Contains(model, "chat"):
		return "chat"
	case strings.Contains(model, "pro"):
		return "pro"
	case strings.Contains(model, "max"):
		return "max"
	}
	return model
}
