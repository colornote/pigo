package agent

import (
	"context"
	"strings"
	"testing"

	"pigo/config"
)

func newTestConfig(sessDir, sessName string) *config.Config {
	return &config.Config{
		APIKey:        "test-key",
		Model:         "deepseek-v4-flash",
		BaseURL:       "https://api.deepseek.com/anthropic",
		DSBaseURL:     "https://api.deepseek.com",
		ThinkingLevel: "medium",
		SystemPrompt:  "test",
		WorkDir:       "/tmp/pigo-test",
		MaxTurns:      5,
		SessionDir:    sessDir,
		SessionName:   sessName,
	}
}

// TestSessionDirOverride verifies that --session-dir is honored by the
// agent's session manager.
func TestSessionDirOverride(t *testing.T) {
	dir := t.TempDir()
	a := New(newTestConfig(dir, ""))
	if err := a.InitSession(""); err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if a.Session() == nil {
		t.Fatal("expected session to be created")
	}
	// The session file must be rooted at the --session-dir override
	// (layout: <session-dir>/<project-slug>/<id>.jsonl).
	if !strings.HasPrefix(a.Session().FilePath, dir) {
		t.Errorf("session file %q not under session dir %q", a.Session().FilePath, dir)
	}
}

// TestInitSessionUsesConfigName verifies that `pigo --name "task"` names
// the first session created (InitSession falls back to cfg.SessionName).
func TestInitSessionUsesConfigName(t *testing.T) {
	dir := t.TempDir()
	a := New(newTestConfig(dir, "my-task"))
	if err := a.InitSession(""); err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if a.Session().Name != "my-task" {
		t.Errorf("session name: expected 'my-task', got %q", a.Session().Name)
	}
}

// TestExplicitNameOverridesConfig verifies an explicit InitSession name
// wins over the --name config value.
func TestExplicitNameOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	a := New(newTestConfig(dir, "config-name"))
	if err := a.InitSession("explicit"); err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if a.Session().Name != "explicit" {
		t.Errorf("session name: expected 'explicit', got %q", a.Session().Name)
	}
}

// TestSetAPIKeyPreservesSession verifies /login's hot key update keeps the
// active session, message history, model, and thinking level intact.
func TestSetAPIKeyPreservesSession(t *testing.T) {
	dir := t.TempDir()
	a := New(newTestConfig(dir, ""))
	a.SetThinking(ThinkHigh)
	if err := a.InitSession(""); err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	if a.Session() == nil {
		t.Fatal("expected a session")
	}
	a.Run(context.Background(), "hi") // no API call happens — key empty is fine, but messages get appended
	msgCount := len(a.messages)
	oldClient := a.client

	a.SetAPIKey("sk-new-key")

	if a.Session() == nil {
		t.Fatal("session lost after SetAPIKey")
	}
	if a.cfg.APIKey != "sk-new-key" {
		t.Errorf("cfg.APIKey: expected sk-new-key, got %q", a.cfg.APIKey)
	}
	if len(a.messages) != msgCount {
		t.Errorf("message history changed: %d → %d", msgCount, len(a.messages))
	}
	if a.Model() != "deepseek-v4-flash" {
		t.Errorf("model changed: %q", a.Model())
	}
	if a.Thinking() != ThinkHigh {
		t.Errorf("thinking changed: %q", a.Thinking())
	}
	if a.client == oldClient {
		t.Error("client not rebuilt with new key")
	}
}
