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

// BuildSystemPrompt 根据模式生成提示词
func BuildSystemPrompt(mode Mode, contextInfo string) string {
	return BuildSystemPromptWithDir(mode, contextInfo, "")
}

// BuildSystemPromptWithDir includes the working directory in the prompt.
func BuildSystemPromptWithDir(mode Mode, contextInfo, workDir string) string {
	var sb strings.Builder

	// Always include working directory
	if workDir != "" {
		sb.WriteString(fmt.Sprintf("Working directory: %s\n", workDir))
	}

	// CoT language instruction — reasoning output must be in Chinese
	sb.WriteString("IMPORTANT: 启用思维链（Chain-of-Thought/CoT）推理时，思维过程必须用中文输出。\n\n")

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
		sb.WriteString("Tools: read, write, edit, bash, grep, find, ls.\n")
		sb.WriteString("Be concise. Use edit, not write, for changes.\n\n")
		sb.WriteString("## Images\n")
		sb.WriteString("The `read` tool returns image files as data URLs. If you are a multimodal model, you can see the image contents directly. Use `read` to inspect screenshots, diagrams, or UI mockups.\n\n")
		sb.WriteString("## Rules\n")
		sb.WriteString("- Read files ONLY once per turn — don't re-read unchanged files\n")
		sb.WriteString("- After understanding the code, IMMEDIATELY propose and make edits\n")
		sb.WriteString("- Don't read the same file twice in one response\n")
		sb.WriteString("- If you're unsure how to proceed, ask the user instead of looping\n")
		sb.WriteString("- Max 3 read calls per turn, then you MUST act or respond\n")
		if contextInfo != "" {
			sb.WriteString("\n## Project\n" + contextInfo + "\n")
		}
		sb.WriteString("\n## Docs\n")
		sb.WriteString("Check `docs/` for pi design reference & feature specs.\n")
	}

	return sb.String()
}
