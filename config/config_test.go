package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadSystemPromptNoContextFiles verifies that -nc/--no-context-files
// forces the minimal default prompt without reading AGENTS.md/CLAUDE.md.
func TestLoadSystemPromptNoContextFiles(t *testing.T) {
	// Plant a context file in the home dir so we can prove it's ignored.
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")
	pigoDir := filepath.Join(home, ".pigo")
	os.MkdirAll(pigoDir, 0755)
	if err := os.WriteFile(filepath.Join(pigoDir, "AGENTS.md"), []byte("SECRET CONTEXT"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	cfg := &Config{NoContextFiles: true}
	cfg.LoadSystemPrompt()
	if cfg.SystemPrompt == "" {
		t.Fatal("expected non-empty default system prompt")
	}
	if cfg.SystemPrompt != "You are PiGo. Use read/write/edit/bash." {
		t.Errorf("expected minimal default prompt, got %q", cfg.SystemPrompt)
	}
}

// TestLoadSystemPromptReadsContext verifies that without -nc the context
// file is loaded (global ~/.pigo/AGENTS.md takes priority).
func TestLoadSystemPromptReadsContext(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")
	pigoDir := filepath.Join(home, ".pigo")
	os.MkdirAll(pigoDir, 0755)
	if err := os.WriteFile(filepath.Join(pigoDir, "AGENTS.md"), []byte("PROJECT RULES"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	cfg := &Config{}
	cfg.LoadSystemPrompt()
	if cfg.SystemPrompt != "PROJECT RULES" {
		t.Errorf("expected AGENTS.md content, got %q", cfg.SystemPrompt)
	}
}
