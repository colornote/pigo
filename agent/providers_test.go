package agent

import (
	"testing"

	"pigo/llm"
)

// TestProviderForModel verifies cross-provider model lookup used by
// /model <name> and --model.
func TestProviderForModel(t *testing.T) {
	if p := ProviderForModel("deepseek-reasoner", ""); p == nil || p.ID != "deepseek" {
		t.Errorf("deepseek-reasoner should resolve to deepseek provider, got %v", p)
	}
	// opencode-go only carries the DeepSeek family (flash/pro).
	if p := ProviderForModel("deepseek-v4-pro", ""); p == nil || p.ID != "opencode-go" {
		t.Errorf("deepseek-v4-pro should resolve to opencode-go (exact id), got %v", p)
	}
	if p := ProviderForModel("kimi-k2.7-code", ""); p != nil {
		t.Errorf("kimi-k2.7-code was removed from opencode-go, got %v", p)
	}
	if p := ProviderForModel("does-not-exist", ""); p != nil {
		t.Errorf("unknown model should resolve to nil, got %v", p)
	}
	// deepseek-v4-pro is both a deepseek alias and a real opencode-go model.
	// Without a preferred provider, the exact id match wins (opencode-go).
	if p := ProviderForModel("deepseek-v4-pro", ""); p == nil || p.ID != "opencode-go" {
		t.Errorf("exact id should beat alias: got %v", p)
	}
	// With the deepseek provider preferred, its alias wins.
	if p := ProviderForModel("deepseek-v4-pro", "deepseek"); p == nil || p.ID != "deepseek" {
		t.Errorf("preferred provider alias should win: got %v", p)
	}
}

// TestProviderResolve verifies alias normalization and default fallback.
func TestProviderResolve(t *testing.T) {
	ds := ProviderByID("deepseek")
	if ds == nil {
		t.Fatal("deepseek provider missing")
	}
	if got := ds.Resolve("deepseek-v4-pro"); got != "deepseek-v4-pro[1m]" {
		t.Errorf("alias resolve: expected deepseek-v4-pro[1m], got %q", got)
	}
	if got := ds.Resolve(""); got != "deepseek-v4-flash" {
		t.Errorf("empty resolve: expected default deepseek-v4-flash, got %q", got)
	}
	if got := ds.Resolve("nope"); got != "deepseek-v4-flash" {
		t.Errorf("unknown resolve: expected default deepseek-v4-flash, got %q", got)
	}

	oc := ProviderByID("opencode-go")
	if oc == nil {
		t.Fatal("opencode-go provider missing")
	}
	if got := oc.Resolve("deepseek-v4-flash"); got != "deepseek-v4-flash" {
		t.Errorf("opencode-go resolve: expected deepseek-v4-flash, got %q", got)
	}
	// deepseek-reasoner does not exist on opencode-go → default.
	if got := oc.Resolve("deepseek-reasoner"); got != "deepseek-v4-flash" {
		t.Errorf("cross-provider model should fall back to opencode-go default, got %q", got)
	}
	// Model info carries pricing + context window + OpenAI protocol flag.
	info := oc.Model("deepseek-v4-flash")
	if info == nil {
		t.Fatal("deepseek-v4-flash missing from opencode-go")
	}
	if info.ContextWindow != 1_000_000 {
		t.Errorf("deepseek-v4-flash context window: got %d", info.ContextWindow)
	}
	if info.Pricing.InputPrice != 0.14 {
		t.Errorf("deepseek-v4-flash input price: got %v", info.Pricing.InputPrice)
	}
	if oc.ToolsFormat != "openai" {
		t.Errorf("opencode-go should use OpenAI protocol, got %q", oc.ToolsFormat)
	}
}

// TestSwitchProviderPreservesState verifies SwitchProvider keeps the
// session, message history, and thinking level while retargeting clients.
func TestSwitchProviderPreservesState(t *testing.T) {
	dir := t.TempDir()
	a := New(newTestConfig(dir, ""))
	a.SetThinking(ThinkHigh)
	if err := a.InitSession(""); err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	// Simulate one exchanged turn WITHOUT hitting the network.
	a.messages = append(a.messages, llm.Message{
		Role:    "user",
		Content: []llm.TextContent{{Type: "text", Text: "hi"}},
	})
	msgCount := len(a.messages)
	oldClient := a.client

	if !a.SwitchProvider("opencode-go") {
		t.Fatal("SwitchProvider(opencode-go) failed")
	}

	if a.Session() == nil {
		t.Fatal("session lost after SwitchProvider")
	}
	if len(a.messages) != msgCount {
		t.Errorf("message history changed: %d → %d", msgCount, len(a.messages))
	}
	if a.Thinking() != ThinkHigh {
		t.Errorf("thinking changed: %q", a.Thinking())
	}
	if a.ProviderID() != "opencode-go" {
		t.Errorf("provider id: expected opencode-go, got %q", a.ProviderID())
	}
	// Current model (deepseek-v4-flash) exists on opencode-go → kept.
	if a.Model() != "deepseek-v4-flash" {
		t.Errorf("model: expected deepseek-v4-flash, got %q", a.Model())
	}
	if a.BaseURL() != "https://opencode.ai/zen/go" {
		t.Errorf("baseURL: got %q", a.BaseURL())
	}
	if a.DSBaseURL() != "https://opencode.ai/zen/go" {
		t.Errorf("dsBaseURL: got %q", a.DSBaseURL())
	}
	// cfg keeps the user's explicit values untouched (test config set a BaseURL).
	if a.cfg.BaseURL != "https://api.deepseek.com/anthropic" {
		t.Errorf("cfg.BaseURL should stay user-config only, got %q", a.cfg.BaseURL)
	}
	if a.client == oldClient {
		t.Error("client not rebuilt for new provider")
	}

	// Unknown provider: rejected, nothing changes.
	if a.SwitchProvider("nope") {
		t.Error("SwitchProvider(nope) should fail")
	}
	if a.ProviderID() != "opencode-go" {
		t.Errorf("provider changed after failed switch: %q", a.ProviderID())
	}
}

// TestNewFallsBackToDefaultModel verifies agent.New resolves a model that
// doesn't exist on the configured provider to the provider default.
func TestNewFallsBackToDefaultModel(t *testing.T) {
	cfg := newTestConfig(t.TempDir(), "")
	cfg.ProviderID = "opencode-go"
	cfg.Model = "deepseek-reasoner" // doesn't exist on opencode-go
	cfg.BaseURL = ""                // empty → provider default
	cfg.DSBaseURL = ""
	a := New(cfg)
	if a.Model() != "deepseek-v4-flash" {
		t.Errorf("expected fallback to deepseek-v4-flash, got %q", a.Model())
	}
	if a.ProviderID() != "opencode-go" {
		t.Errorf("provider: %q", a.ProviderID())
	}
	if a.BaseURL() != "https://opencode.ai/zen/go" {
		t.Errorf("baseURL should come from provider defaults, got %q", a.BaseURL())
	}
}
