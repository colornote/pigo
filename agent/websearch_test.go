package agent

import (
	"os"
	"testing"

	"pigo/config"
)

// testAgentForProvider builds an Agent wired to the given provider with a
// model that resolves on it (avoids nil derefs in ModelInfo/protocol).
func testAgentForProvider(t *testing.T, p *Provider) *Agent {
	t.Helper()
	return &Agent{
		cfg:      &config.Config{Model: p.DefaultModel},
		provider: p,
	}
}

// TestWebSearchEnabled verifies the native web_search tool is advertised
// only on the Responses API protocol and can be disabled via
// PIGO_WEB_SEARCH=0 (default on).
func TestWebSearchEnabled(t *testing.T) {
	os.Unsetenv("PIGO_WEB_SEARCH")
	defer os.Unsetenv("PIGO_WEB_SEARCH")

	a := testAgentForProvider(t, &DeepSeekResponsesProvider)
	if !a.webSearchEnabled() {
		t.Error("responses provider should advertise web_search by default")
	}

	os.Setenv("PIGO_WEB_SEARCH", "0")
	if a.webSearchEnabled() {
		t.Error("PIGO_WEB_SEARCH=0 must disable web_search")
	}
	os.Unsetenv("PIGO_WEB_SEARCH")

	// Anthropic and OpenAI-compatible providers have no native search tool.
	for _, p := range []*Provider{&DeepSeekProvider, &OpenCodeGoProvider} {
		if testAgentForProvider(t, p).webSearchEnabled() {
			t.Errorf("%s must not advertise web_search", p.ID)
		}
	}
}

// TestVisionModelRegisteredMultimodal verifies the official vision models are
// registered and flagged multimodal, so read returns base64 data URLs (not
// metadata). deepseek-v4.1-flash-expires-on-0910 (V4.1 Flash preview,
// vision-capable, expires 2026-09-10) is served on the Responses API only.
func TestVisionModelRegisteredMultimodal(t *testing.T) {
	for _, p := range []*Provider{&DeepSeekProvider, &DeepSeekResponsesProvider} {
		m := p.Model("deepseek-v4-flash-vision-exp")
		if m == nil {
			t.Fatalf("%s: deepseek-v4-flash-vision-exp missing", p.ID)
		}
		if !m.Multimodal {
			t.Errorf("%s: vision-exp must be marked Multimodal", p.ID)
		}
	}
	m := DeepSeekResponsesProvider.Model("deepseek-v4.1-flash-expires-on-0910")
	if m == nil {
		t.Fatal("deepseek-responses: deepseek-v4.1-flash-expires-on-0910 missing")
	}
	if !m.Multimodal {
		t.Error("deepseek-v4.1-flash-expires-on-0910 must be marked Multimodal")
	}
	// The expired spelling (without the s) is rejected by the API — make
	// sure it never resolves silently.
	if DeepSeekResponsesProvider.HasModel("deepseek-v4.1-flash-expire-on-0910") {
		t.Error("legacy -expire-on-0910 spelling must not be registered")
	}
}
