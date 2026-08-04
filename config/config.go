package config

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	// Tools / ExcludeTools / NoTools — tool filtering flags (pi parity):
	// --tools read,write   only these tools are available
	// --exclude-tools bash disable specific tools
	// --no-tools           disable all tools (text-only)
	Tools        []string
	ExcludeTools []string
	NoTools      bool
}

// starterAGENTS is written to ~/.pigo/AGENTS.md on first run (when the file
// doesn't exist yet). It doubles as user-facing docs and as the default
// system-prompt context: the file is loaded automatically at startup and
// appended to the built-in prompt. Users edit it freely; PiGo never
// overwrites it.
const starterAGENTS = `# PiGo Agent Instructions

This file is loaded automatically at startup and injected into your system
prompt. Edit it to give PiGo persistent rules and project-independent
instructions; it is never overwritten. Delete it to go back to the built-in
default prompt.

## 角色与工作方式

You are PiGo — a coding agent in Go. Work like a focused senior engineer:

- 先理解再动手：读相关文件、确认改动范围，然后立即编辑；不要反复重读
- 每个 turn 最多 3 次 read 调用，之后必须行动或提问
- 修改代码用 edit（精确替换），不要用 write 整文件重写
- 不确定怎么做时，用一句话问用户，而不是空转循环
- 保持简洁：回复直接、要点化，不堆砌客套

## 工具速查

| 工具 | 用途 |
|---|---|
| read | 读文本文件（支持 offset/limit）；图片文件按主模型能力返回 data URL 或 vision 提示 |
| write | 新建/整体覆盖文件（小文件或模板） |
| edit | 对现有文件做精确文本替换（首选修改手段） |
| bash | 执行命令：build、test、git、安装依赖等 |
| grep | 按正则搜代码（优先于 bash grep） |
| find | 按文件名 glob 找文件 |
| ls | 列目录 |
| vision | 分析图片：截图、架构图、UI 稿、图表 |

## 图片 / Vision

你不能直接看到图片（除非主模型是多模态）。需要理解图片内容时，
调用 vision 工具：传图片路径和可选问题，视觉子模型（默认
mimo-v2.5）返回文字描述，你再基于描述继续工作。

示例：vision {path: "docs/architecture.png", prompt: "描述这个架构并指出问题"}

## 工程规范

- Go 项目改动后必须验证：go build -o pigo .、go vet ./...、go test ./...
- 修改后跑相关包的测试，确认不破坏现有行为
- 遵循既有代码风格（本项目为 Go 1.18 兼容，不使用 1.18+ 专属语法）
- 不引入新的第三方依赖，除非确有必要并说明理由

## Git 提交

提交信息用 Conventional Commits 风格，中文描述要点：

feat: 新功能一句话
fix: 修 bug 一句话
refactor: 重构一句话
docs: 文档更新
test: 测试

- 提交前 git add -A，一次提交一个逻辑变更
- 如果改动涉及 docs/pi-design.md 的功能清单，同步更新对应 checkbox

## 交互约定

- 用户输入 @file 或 @图片 时，优先用 read/vision 读取内容再回答
- 用户要求"总结""解释""列出"时，直接输出，无需调用工具
- 用户给的指令不明确时，先确认再执行大改动
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
			return string(data) + PackagesInfo()
		}
	}
	return "You are PiGo. Use read/write/edit/bash." + PackagesInfo()
}

// ─── Package / Extension discovery ─────────────────────────────
//
// PiGo auto-discovers pi-style capability packages under `packages/`
// (project) and `~/.pigo/packages/` (global) and injects a short listing
// into the system prompt, so the agent knows which extensions exist. The
// full instructions live in each package's SKILL.md — progressive disclosure.

// packagesRoots lists where capability packages are discovered: project
// `packages/` and global `~/.pigo/packages/`. Package-level so tests can
// point it at a temp dir.
var packagesRoots = func() []string {
	roots := []string{"packages"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, ".pigo", "packages"))
	}
	return roots
}()

// PackagesInfo returns the markdown listing of available packages, or "" when
// none are found. Exported so agent/Reload can re-derive it identically.
func PackagesInfo() string {
	var lines []string
	seen := map[string]bool{}
	for _, root := range packagesRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			name, desc := packageMeta(dir)
			if name == "" {
				name = e.Name()
			}
			if desc == "" {
				continue // unknown capability — don't advertise it
			}
			key := filepath.Clean(dir)
			if seen[key] {
				continue
			}
			seen[key] = true
			lines = append(lines, fmt.Sprintf("- **%s** — %s (see `%s/SKILL.md` for usage)", name, desc, dir))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n## Packages\nAvailable capability packages (progressive disclosure: read a package's `SKILL.md` for full instructions before using it):\n" + strings.Join(lines, "\n")
}

// packageMeta extracts (name, description) from a package dir: the
// `package.json` pi manifest first, falling back to the SKILL.md frontmatter.
func packageMeta(dir string) (name, desc string) {
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var m struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Pi          struct {
				Description string `json:"description"`
			} `json:"pi"`
		}
		if json.Unmarshal(data, &m) == nil {
			name = m.Name
			desc = m.Pi.Description
			if desc == "" {
				desc = m.Description
			}
		}
	}
	if desc == "" {
		if data, err := os.ReadFile(filepath.Join(dir, "SKILL.md")); err == nil {
			if n, d := skillFrontmatter(data); d != "" {
				if name == "" {
					name = n
				}
				desc = d
			}
		}
	}
	return
}

// skillFrontmatter parses the `--- name: … description: … ---` header of a
// SKILL.md (Agent Skills spec) and returns (name, description).
func skillFrontmatter(data []byte) (name, desc string) {
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return "", ""
	}
	rest := s[3:]
	end := strings.Index(rest, "---")
	if end < 0 {
		return "", ""
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "name:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		case strings.HasPrefix(line, "description:"):
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return
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
