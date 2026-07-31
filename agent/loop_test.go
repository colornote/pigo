package agent

import (
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
