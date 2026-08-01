package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"pigo/agent"
	"strings"
	"time"

	"github.com/peterh/liner"
)

// promptLiner is the liner state owned by runInteractive. /login needs it
// for safe paste handling and rune-aware editing while the tty is in
// liner's raw mode.
var promptLiner *liner.State

// handleLogin implements pi's /login: pick a provider, paste an API key,
// verify it, persist it to ~/.pigo/.env, and apply it to the running agent
// so it takes effect immediately — no restart needed.
func handleLogin() {
	if promptLiner == nil {
		fmt.Printf("%s✗ /login 仅在交互模式可用%s\n", ANSIRed, ANSIReset)
		return
	}

	providers := agent.Providers
	if len(providers) == 0 {
		fmt.Printf("%s✗ 没有可用 provider%s\n", ANSIRed, ANSIReset)
		return
	}

	// ── 1. Provider selection ──
	fmt.Printf("\n%s╭─ /login ──────────────────────────%s\n", ANSICyan, ANSIReset)
	for i, p := range providers {
		def := " "
		if i == 0 {
			def = "▶"
		}
		fmt.Printf(" %s [%d] %s\n", def, i+1, p.Name)
	}
	fmt.Printf("%s╰───────────────────────────────────%s\n", ANSICyan, ANSIReset)
	fmt.Printf("%s选择 provider (回车默认 DeepSeek, q 取消):%s ", ANSIYellow, ANSIReset)

	sel, err := promptLiner.Prompt("▸ ")
	if err != nil {
		fmt.Println()
		return
	}
	sel = strings.TrimSpace(sel)
	if sel == "" {
		sel = "1" // default to the first provider (DeepSeek)
	}
	if strings.EqualFold(sel, "q") || strings.EqualFold(sel, "quit") {
		fmt.Printf("%s已取消%s\n", ANSIGray, ANSIReset)
		return
	}
	// Match by number (1, 2, …) or by ID/name ("deepseek", "DeepSeek").
	var provider *agent.Provider
	if len(sel) == 1 && sel[0] >= '1' && sel[0] <= '9' {
		if n := int(sel[0] - '0'); n >= 1 && n <= len(providers) {
			provider = &providers[n-1]
		}
	} else {
		for i := range providers {
			if strings.EqualFold(sel, providers[i].ID) || strings.EqualFold(sel, providers[i].Name) {
				provider = &providers[i]
				break
			}
		}
	}
	if provider == nil {
		fmt.Printf("%s✗ 未知 provider: %s%s\n", ANSIRed, sel, ANSIReset)
		return
	}

	// ── 2. API key input ──
	fmt.Printf("\n%s粘贴 %s API Key (回车确认, q 取消):%s\n", ANSIYellow, provider.Name, ANSIReset)
	key, err := promptLiner.Prompt("▸ ")
	if err != nil {
		fmt.Println()
		return
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.EqualFold(key, "q") {
		fmt.Printf("%s已取消%s\n", ANSIGray, ANSIReset)
		return
	}

	// ── 3. Verify against the provider's native API ──
	fmt.Printf("%s⏳ 验证 API Key...%s", ANSIGray, ANSIReset)
	ok, verr := verifyProviderKey(provider, key)
	switch {
	case verr != nil:
		fmt.Printf(" %s✗%s 验证出错: %v\n", ANSIRed, ANSIReset, verr)
	case ok:
		fmt.Printf(" %s✓%s API Key 有效\n", ANSIGreen, ANSIReset)
	default:
		fmt.Printf(" %s✗%s API Key 无效\n", ANSIRed, ANSIReset)
	}
	if verr != nil || !ok {
		if !confirmLogin("验证失败，仍要保存并继续吗?") {
			fmt.Printf("%s已取消%s\n", ANSIGray, ANSIReset)
			return
		}
	}

	// ── 4. Persist to ~/.pigo/.env (survives restarts) ──
	home, _ := os.UserHomeDir()
	envPath := filepath.Join(home, ".pigo", ".env")
	if err := updateEnvFile(envPath, provider.EnvKey, key); err != nil {
		fmt.Printf("%s✗ 保存到 %s 失败: %v%s\n", ANSIRed, envPath, err, ANSIReset)
		return
	}
	os.Setenv(provider.EnvKey, key) // visible to this process + child processes

	// ── 5. Apply to the running agent immediately (switches provider + rebuilds clients) ──
	if !ag.SwitchProvider(provider.ID) {
		fmt.Printf("%s✗ 无法切换到 provider %s%s\n", ANSIRed, provider.ID, ANSIReset)
		return
	}

	fmt.Printf("\n%s✓ 已登录 %s%s%s\n", ANSIGreen, ANSIReset, ANSIBold, provider.Name)
	fmt.Printf("  %sAPI Key 已保存到 %s%s\n", ANSIGray, envPath, ANSIReset)
	fmt.Printf("  %s当前模型: %s%s%s   ·   %s/model%s 可切换%s\n",
		ANSIGray, ANSIGreen, ag.Model(), ANSIReset, ANSIYellow, ANSIReset, ANSIReset)
}

// handleLogout removes the stored API key for the ACTIVE provider from
// ~/.pigo/.env and the running agent. Keys set via external shell env vars
// are untouched.
func handleLogout() {
	home, _ := os.UserHomeDir()
	envPath := filepath.Join(home, ".pigo", ".env")
	provider := agent.ProviderByID(ag.ProviderID())
	keyVar := "DEEPSEEK_API_KEY"
	if provider != nil && provider.EnvKey != "" {
		keyVar = provider.EnvKey
	}
	if err := removeEnvKey(envPath, keyVar); err != nil {
		fmt.Printf("%s✗ 移除 API Key 失败: %v%s\n", ANSIRed, err, ANSIReset)
		return
	}
	os.Unsetenv(keyVar)
	ag.SetAPIKey("")
	fmt.Printf("%s✓ 已退出登录%s — 已从 %s 移除 %s%s\n",
		ANSIGreen, ANSIReset, envPath, keyVar, ANSIReset)
}

// confirmLogin asks a y/N question via the interactive prompt.
func confirmLogin(question string) bool {
	if promptLiner == nil {
		return false
	}
	ans, err := promptLiner.Prompt(question + " [y/N] ")
	if err != nil {
		return false
	}
	ans = strings.TrimSpace(ans)
	return strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes")
}

// verifyProviderKey checks the key against the provider's OpenAI-compatible
// chat endpoint with a minimal request (POST {DSBaseURL}/v1/chat/completions,
// max_tokens=1). GET /models is NOT reliable: opencode.ai serves it without
// authenticating. Providers without a native endpoint skip verification.
// Returns ok=true when the key is accepted.
func verifyProviderKey(p *agent.Provider, key string) (bool, error) {
	base := strings.TrimRight(p.DSBaseURL, "/")
	if base == "" {
		return true, nil // no native endpoint — nothing to verify against
	}
	model := p.DefaultModel
	if model == "" {
		model = "deepseek-chat"
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})
	req, err := http.NewRequest("POST", base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain so the connection can be reused
	return resp.StatusCode == 200, nil
}

// updateEnvFile sets key=value in an .env-style file, preserving comments,
// ordering, and unrelated keys. Duplicate keys collapse to the new value.
// Creates the file (0600) if it doesn't exist.
func updateEnvFile(path, key, value string) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	prefix := key + "="

	var content []string
	if data, err := os.ReadFile(path); err == nil {
		content = strings.Split(string(data), "\n")
	}

	out := make([]string, 0, len(content)+1)
	replaced := false
	for _, line := range content {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue // drop duplicate keys
		}
		if trimmed == "" {
			continue // drop blank lines
		}
		out = append(out, line)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0600)
}

// removeEnvKey deletes the key=value line(s) for key from an .env-style
// file. Missing file is not an error.
func removeEnvKey(path, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // nothing to remove
	}
	prefix := key + "="
	out := make([]string, 0, len(data))
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			continue
		}
		out = append(out, line)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0600)
}
