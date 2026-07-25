package agent

import (
	"fmt"
	"strings"
)

// Mode
type Mode int

const (
	ModeNormal Mode = iota
	ModeSelfIterate
	ModeAutoRepair
)

var modeNames = map[Mode]string{
	ModeNormal:      "normal",
	ModeSelfIterate: "self-iterate",
	ModeAutoRepair:  "auto-repair",
}

func (m Mode) String() string { return modeNames[m] }

// ThinkingLevel
type ThinkingLevel string

const (
	ThinkOff    ThinkingLevel = "off"
	ThinkLow    ThinkingLevel = "low"
	ThinkMedium ThinkingLevel = "medium"
	ThinkHigh   ThinkingLevel = "high"
	ThinkMax    ThinkingLevel = "max"
)

// DeepSeek 只有一个 base URL
const DeepSeekBaseURL = "https://api.deepseek.com/anthropic"

// DeepSeek 可用模型
var DeepSeekModels = map[string]string{
	"deepseek-v4-flash":      "DeepSeek V4 Flash — 快速推理",
	"deepseek-v4-pro[1m]":    "DeepSeek V4 Pro 1M — 长上下文",
	"deepseek-chat":          "DeepSeek Chat — 通用",
	"deepseek-reasoner":      "DeepSeek Reasoner — 深度推理 · 线上CoT",
}

// CoTModels lists models that support Chain-of-Thought streaming
var CoTModels = map[string]bool{
	"deepseek-reasoner": true,
}

// BuildSystemPrompt 根据模式生成提示词
func BuildSystemPrompt(mode Mode, contextInfo string) string {
	var sb strings.Builder

	switch mode {
	case ModeSelfIterate:
		sb.WriteString("**SELF-ITERATION MODE**\n\n")
		sb.WriteString("You are improving the PiGo codebase to match pi's capabilities.\n\n")
		sb.WriteString("## Reference Docs (read first!)\n")
		sb.WriteString("- `docs/pi-readme.md` — pi's full feature set: CLI, tools, permissions, MCP\n")
		sb.WriteString("- `docs/pi-design.md` — PiGo architecture & feature parity checklist\n\n")
		sb.WriteString("## Rules\n")
		sb.WriteString("1. Read `docs/pi-design.md` for the feature checklist — pick one missing feature\n")
		sb.WriteString("2. Read `docs/pi-readme.md` for pi's implementation details of that feature\n")
		sb.WriteString("3. Read relevant .go source files to understand current state\n")
		sb.WriteString("4. Edit files to implement the feature (one change at a time)\n")
		sb.WriteString("5. Run: go build -o pigo . — fix any errors and retry\n")
		sb.WriteString("6. Run: go vet ./... — confirm clean\n")
		sb.WriteString("7. Run: git add -A && git commit -m \"feat: ...\"\n")
		sb.WriteString("8. Update `docs/pi-design.md` checkboxes for completed features\n")
		sb.WriteString("9. Summarize what changed and what remains\n")

	case ModeAutoRepair:
		sb.WriteString("**AUTO-REPAIR MODE**\n\n")
		sb.WriteString("User reported a PiGo issue. Fix it.\n\n")
		sb.WriteString("## Steps\n")
		sb.WriteString("1. Understand the bug\n")
		sb.WriteString("2. Read relevant source\n")
		sb.WriteString("3. Apply minimal fix\n")
		sb.WriteString("4. Build and verify\n")

	default:
		sb.WriteString("You are PiGo — a coding agent in Go.\n")
		sb.WriteString("Tools: read, write, edit, bash.\n")
		sb.WriteString("Be concise. Use edit for changes.\n")
		if contextInfo != "" {
			sb.WriteString("\n## Project\n" + contextInfo + "\n")
		}
		sb.WriteString("\n## Docs\n")
		sb.WriteString("Check `docs/` for pi design reference & feature specs.\n")
	}

	return sb.String()
}

// FormatModeStatus returns mode status line
func FormatModeStatus(mode Mode, model string, thinking ThinkingLevel) string {
	return fmt.Sprintf("[%s] model=%s thinking=%s", mode, model, thinking)
}
