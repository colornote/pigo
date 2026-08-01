package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	APIKey         string
	ProviderID     string // active provider id (PIGO_PROVIDER, default "deepseek")
	Model          string
	BaseURL        string
	DSBaseURL      string // native OpenAI-compatible API (CoT/reasoner)
	ThinkingLevel  string
	SystemPrompt   string
	WorkDir        string
	MaxTurns       int
	NoSession      bool   // ephemeral mode
	SessionName    string // optional session display name
	SessionPath    string // specific session file to load
	SessionDir     string // --session-dir: custom session storage directory
	NoContextFiles bool   // -nc/--no-context-files: skip AGENTS.md/CLAUDE.md
	Continue       bool   // continue latest session
	AutoRepair     bool   // auto-trigger repair on error without asking
	Print          bool   // -p/--print: non-interactive, print response and exit
	// VisionModel is the vision sub-agent model used by the `vision` tool
	// (default: mimo-v2.5 on opencode-go). Auth comes from OPENCODE_API_KEY.
	VisionModel string
}

// starterAGENTS is written to ~/.pigo/AGENTS.md on first run (when the file
// doesn't exist yet). It doubles as user-facing docs and as the default
// system-prompt context: the file is loaded automatically at startup and
// appended to the built-in prompt. Users edit it freely; PiGo never
// overwrites it.
const starterAGENTS = `# PiGo Agent Instructions

This file is loaded automatically at startup and added to your system prompt
(along with the built-in default). Edit it to give PiGo persistent rules,
conventions, and project-independent instructions. Delete it to go back to
the built-in default prompt.

## Tools
- read   — read text files (image files: vision hint for text models, data URL for multimodal models)
- write  — create/overwrite files
- edit   — apply exact-text edits
- bash   — run shell commands
- grep / find / ls — search and list files
- vision — analyze an image with the vision sub-agent model (default mimo-v2.5 on opencode-go)

## Vision
You cannot see images directly (unless you are a multimodal model like
mimo-v2.5). To analyze a screenshot, diagram, UI mockup, chart, or any image
file, call the vision tool with an image path and an optional question. The
vision model returns a text description you can act on.

## Docs
Check docs/ for pi design reference & feature specs.
`

// ensureGlobalContext creates ~/.pigo and, on first run, writes a starter
// AGENTS.md the user can edit. It never overwrites an existing file (user
// customizations win). Returns the AGENTS.md path, or "" when it can't be
// created (no home dir, no permissions).
func ensureGlobalContext() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	pigoDir := filepath.Join(home, ".pigo")
	if err := os.MkdirAll(pigoDir, 0755); err != nil {
		return ""
	}
	path := filepath.Join(pigoDir, "AGENTS.md")
	if _, err := os.Stat(path); err == nil {
		return path // already exists — never clobber user content
	}
	if err := os.WriteFile(path, []byte(starterAGENTS), 0644); err != nil {
		return ""
	}
	return path
}

// LoadSystemPrompt re-derives the system prompt from context files.
// main.go calls it after CLI flags (-nc/--no-context-files) are parsed so
// the flag and config.Load() ordering doesn't matter.
func (c *Config) LoadSystemPrompt() {
	home, _ := os.UserHomeDir()
	if c.NoContextFiles {
		c.SystemPrompt = "You are PiGo. Use read/write/edit/bash."
		return
	}
	c.SystemPrompt = loadSystemPrompt(home)
}

// providerEnvKeys maps provider id → primary API key env var. Kept in
// config (duplicated from agent's registry) so config.Load can resolve the
// right key without importing agent (which would create an import cycle).
var providerEnvKeys = map[string]string{
	"deepseek":    "DEEPSEEK_API_KEY",
	"opencode-go": "OPENCODE_API_KEY",
}

// EnvKeyFor returns the primary API key environment variable of a provider.
// Exported so main's --provider flag can re-resolve the key after switching.
func EnvKeyFor(providerID string) string {
	return providerEnvKeys[providerID]
}

func Load() *Config {
	// 0. Ensure ~/.pigo exists with a starter AGENTS.md on first run, so
	//    the user always has a system/agent prompt doc to customize and the
	//    vision tool is documented. Never overwrites an existing file.
	home, _ := os.UserHomeDir()
	ensureGlobalContext()

	// 1. Load .env files (home then cwd, cwd overrides)
	loadEnvFile(filepath.Join(home, ".pigo", ".env"))
	loadEnvFile(".env")

	providerID := getEnv("PIGO_PROVIDER", "deepseek")

	return &Config{
		APIKey:        lookupKey(providerID),
		ProviderID:    providerID,
		Model:         getEnv("PIGO_MODEL", ""),
		BaseURL:       getEnv("PIGO_BASE_URL", ""),
		DSBaseURL:     getEnv("PIGO_DS_BASE_URL", ""),
		ThinkingLevel: getEnv("PIGO_THINKING", "medium"),
		SystemPrompt:  loadSystemPrompt(home),
		WorkDir:       getEnv("PIGO_WORKDIR", ""),
		MaxTurns:      getEnvInt("PIGO_MAX_TURNS", 50),
		AutoRepair:    getEnvBool("PIGO_AUTOREPAIR"),
		VisionModel:   getEnv("PIGO_VISION_MODEL", ""),
	}
}

// loadEnvFile reads KEY=VALUE pairs from a file and sets env vars
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Remove surrounding quotes if present
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[0] == val[len(val)-1] {
			val = val[1 : len(val)-1]
		}
		// .env always overrides shell env
		os.Setenv(key, val)
	}
}

func loadSystemPrompt(home string) string {
	places := []string{
		filepath.Join(home, ".pigo", "AGENTS.md"),
		"AGENTS.md",
		"CLAUDE.md",
	}
	for _, p := range places {
		if data, err := os.ReadFile(p); err == nil {
			return string(data)
		}
	}
	return "You are PiGo. Use read/write/edit/bash."
}

func lookupKey(providerID string) string {
	// Provider's own key first, then generic fallbacks.
	keys := []string{}
	if k := providerEnvKeys[providerID]; k != "" {
		keys = append(keys, k)
	}
	keys = append(keys, "ANTHROPIC_API_KEY", "PIGO_API_KEY")
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n := 0
	for _, c := range v {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n == 0 {
		return fallback
	}
	return n
}

func getEnvBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
