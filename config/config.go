package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	APIKey        string
	Model         string
	BaseURL       string
	ThinkingLevel string
	SystemPrompt  string
	WorkDir       string
	MaxTurns      int
	NoSession     bool   // ephemeral mode
	SessionName   string // optional session display name
	SessionPath   string // specific session file to load
	Continue      bool   // continue latest session
}

func Load() *Config {
	// 1. Load .env files (home then cwd, cwd overrides)
	home, _ := os.UserHomeDir()
	loadEnvFile(filepath.Join(home, ".pigo", ".env"))
	loadEnvFile(".env")

	return &Config{
		APIKey:        lookupKey(),
		Model:         getEnv("PIGO_MODEL", "deepseek-v4-flash"),
		BaseURL:       getEnv("PIGO_BASE_URL", "https://api.deepseek.com/anthropic"),
		ThinkingLevel: getEnv("PIGO_THINKING", "medium"),
		SystemPrompt:  loadSystemPrompt(home),
		WorkDir:       getEnv("PIGO_WORKDIR", ""),
		MaxTurns:      getEnvInt("PIGO_MAX_TURNS", 50),
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

func lookupKey() string {
	for _, k := range []string{"DEEPSEEK_API_KEY", "ANTHROPIC_API_KEY", "PIGO_API_KEY"} {
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
