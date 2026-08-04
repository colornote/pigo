package agent

import (
	"os"
	"strings"
	"testing"

	"pigo/config"
	"pigo/llm"
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
	// Simulate one exchanged turn WITHOUT hitting the network (a.Run would
	// issue a real API call against the provider endpoint).
	a.messages = append(a.messages, llm.Message{
		Role:    "user",
		Content: []llm.TextContent{{Type: "text", Text: "hi"}},
	})
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

// allToolNames returns the names of every registered tool, in order.
func allToolNames() []string {
	dir, _ := os.MkdirTemp("", "pigo-tools")
	defer os.RemoveAll(dir)
	a := New(newTestConfig(dir, ""))
	return a.enabledToolNames()
}

// TestToolFilterAllowlist verifies --tools restricts the registry to the
// named tools (in registration order) and blocks execution of others.
func TestToolFilterAllowlist(t *testing.T) {
	a := New(newTestConfig(t.TempDir(), ""))
	a.applyToolFilter([]string{"read", "bash", "grep"}, nil, false)

	if got := a.enabledToolNames(); len(got) != 3 || got[0] != "read" || got[1] != "bash" || got[2] != "grep" {
		t.Errorf("allowlist: expected [read bash grep], got %v", got)
	}
	if !a.toolEnabled("read") || !a.toolEnabled("grep") {
		t.Error("allowlisted tools should be enabled")
	}
	if a.toolEnabled("write") || a.toolEnabled("edit") || a.toolEnabled("vision") {
		t.Error("non-allowlisted tools must be disabled")
	}
	if !a.toolFilterActive() {
		t.Error("filter should be active")
	}
}

// TestToolFilterExclude verifies --exclude-tools disables only the named
// tools while keeping the rest.
func TestToolFilterExclude(t *testing.T) {
	a := New(newTestConfig(t.TempDir(), ""))
	a.applyToolFilter(nil, []string{"bash", "vision"}, false)

	names := a.enabledToolNames()
	if len(names) != len(allToolNames())-2 {
		t.Errorf("exclude: expected %d tools, got %d (%v)", len(allToolNames())-2, len(names), names)
	}
	if a.toolEnabled("bash") || a.toolEnabled("vision") {
		t.Error("excluded tools must be disabled")
	}
	if !a.toolEnabled("read") || !a.toolEnabled("edit") {
		t.Error("non-excluded tools must stay enabled")
	}
}

// TestToolFilterNoTools verifies --no-tools disables everything.
func TestToolFilterNoTools(t *testing.T) {
	a := New(newTestConfig(t.TempDir(), ""))
	a.applyToolFilter(nil, nil, true)

	if len(a.enabledTools()) != 0 {
		t.Errorf("--no-tools should disable all tools, got %v", a.enabledToolNames())
	}
	if a.toolEnabled("read") || a.toolEnabled("bash") {
		t.Error("no tool should be enabled under --no-tools")
	}
}

// TestToolFilterEmpty verifies an empty filter leaves all tools enabled.
func TestToolFilterEmpty(t *testing.T) {
	a := New(newTestConfig(t.TempDir(), ""))
	a.applyToolFilter(nil, nil, false)
	if len(a.enabledTools()) != len(allToolNames()) {
		t.Errorf("expected all %d tools, got %d", len(allToolNames()), len(a.enabledTools()))
	}
	if a.toolFilterActive() {
		t.Error("no filter should be active by default")
	}
}

// TestBuildSysPromptToolList verifies the active tool list is appended to
// the system prompt when a filter is applied (so the model stops calling
// removed tools), and omitted when there's no filter.
func TestBuildSysPromptToolList(t *testing.T) {
	base := New(newTestConfig(t.TempDir(), ""))
	plain := base.buildSysPrompt()
	if strings.Contains(plain, "Available tools:") {
		t.Errorf("unfiltered prompt should not list tools:\n%s", plain)
	}

	base.applyToolFilter([]string{"read", "write"}, nil, false)
	filtered := base.buildSysPrompt()
	if !strings.Contains(filtered, "Available tools: read, write") {
		t.Errorf("filtered prompt missing tool list:\n%s", filtered)
	}

	base.applyToolFilter(nil, nil, true)
	none := base.buildSysPrompt()
	if !strings.Contains(none, "Available tools: none") {
		t.Errorf("--no-tools prompt should say none:\n%s", none)
	}
}

// TestSessionEnv verifies bash session metadata env vars (pi parity),
// including the PI_SESSION_FILE omission for ephemeral sessions.
func TestSessionEnv(t *testing.T) {
	a := New(newTestConfig(t.TempDir(), ""))
	env := a.sessionEnv()
	if env["PI_PROVIDER"] != "deepseek" || env["PI_MODEL"] != "deepseek-v4-flash" {
		t.Errorf("provider/model env: %v", env)
	}
	if env["PI_REASONING_LEVEL"] != "medium" {
		t.Errorf("thinking env: %q", env["PI_REASONING_LEVEL"])
	}
	if _, ok := env["PI_SESSION_FILE"]; ok {
		t.Error("PI_SESSION_FILE must be unset for ephemeral/no-session runs")
	}

	if err := a.InitSession(""); err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	env = a.sessionEnv()
	if env["PI_SESSION_ID"] == "" || env["PI_SESSION_ID"] != a.Session().ID {
		t.Errorf("PI_SESSION_ID mismatch: %q", env["PI_SESSION_ID"])
	}
	if env["PI_SESSION_FILE"] != a.Session().FilePath {
		t.Errorf("PI_SESSION_FILE mismatch: %q", env["PI_SESSION_FILE"])
	}
}

// TestNewSession verifies /new resets in-memory history and starts a fresh
// session without touching the persisted old one.
func TestNewSession(t *testing.T) {
	dir := t.TempDir()
	a := New(newTestConfig(dir, ""))
	if err := a.InitSession(""); err != nil {
		t.Fatalf("InitSession: %v", err)
	}
	old := a.Session()
	oldPath := old.FilePath
	old.AddEntry("", "user", "old message", "")
	a.messages = append(a.messages, llm.Message{
		Role:    "user",
		Content: []llm.TextContent{{Type: "text", Text: "old message"}},
	})
	a.messageIDs = append(a.messageIDs, "x")

	if err := a.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if a.Session() == nil || a.Session().ID == old.ID {
		t.Error("expected a new session ID")
	}
	if len(a.messages) != 0 {
		t.Errorf("messages not reset: %v", a.messages)
	}
	if len(a.messageIDs) != 0 {
		t.Errorf("messageIDs not reset: %v", a.messageIDs)
	}
	// The old session file must still exist with its entry.
	data, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("old session file lost: %v", err)
	}
	if !strings.Contains(string(data), "old message") {
		t.Errorf("old session content lost:\n%s", data)
	}
}
