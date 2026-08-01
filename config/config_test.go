package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureGlobalContextCreatesStarter verifies first run creates
// ~/.pigo/AGENTS.md with the vision tool documented, and later runs never
// overwrite user customizations.
func TestEnsureGlobalContextCreatesStarter(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")

	path := ensureGlobalContext()
	if path == "" {
		t.Fatal("expected a starter AGENTS.md path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read starter: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "vision") {
		t.Errorf("starter AGENTS.md should document the vision tool:\n%s", s)
	}
	if !strings.Contains(s, "PiGo Agent Instructions") {
		t.Errorf("starter AGENTS.md should carry a title:\n%s", s)
	}

	// Second call must not clobber user edits.
	custom := "MY CUSTOM RULES\n"
	if err := os.WriteFile(path, []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}
	ensureGlobalContext()
	data, _ = os.ReadFile(path)
	if string(data) != custom {
		t.Errorf("ensureGlobalContext overwrote user content: %q", data)
	}
}

// TestEnsureGlobalContextPreservesExisting verifies an existing AGENTS.md is
// left alone entirely (no re-write, no touch).
func TestEnsureGlobalContextPreservesExisting(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")

	dir := filepath.Join(home, ".pigo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(existing, []byte("KEEP ME"), 0644); err != nil {
		t.Fatal(err)
	}

	path := ensureGlobalContext()
	if path != existing {
		t.Errorf("expected %q, got %q", existing, path)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "KEEP ME" {
		t.Errorf("existing file modified: %q", data)
	}
}

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
