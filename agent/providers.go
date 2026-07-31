package agent

// Provider describes a supported LLM provider for the /login flow.
// Only DeepSeek is supported for now; add entries here (plus config/env
// plumbing) to support more providers.
type Provider struct {
	ID        string
	Name      string
	EnvKey    string            // primary API key environment variable
	DSBaseURL string            // native API base URL (used for key verification)
	Models    map[string]string // model registry (name → description)
}

// Providers lists the providers supported by /login.
var Providers = []Provider{
	{
		ID:        "deepseek",
		Name:      "DeepSeek",
		EnvKey:    "DEEPSEEK_API_KEY",
		DSBaseURL: "https://api.deepseek.com",
		Models:    DeepSeekModels,
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
