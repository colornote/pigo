package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"pigo/agent"
	"pigo/config"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

var ag *agent.Agent
var startupCommandLine string

// ESC interrupt support
var (
	escCancel context.CancelFunc
	escDone  chan struct{}
)

// Local aliases for brevity (imported from agent package)
const (
	ANSIReset  = agent.ANSIReset
	ANSIRed    = agent.ANSIRed
	ANSIGreen  = agent.ANSIGreen
	ANSIYellow = agent.ANSIYellow
	ANSICyan   = agent.ANSICyan
	ANSIGray   = agent.ANSIGray
	ANSIBold   = agent.ANSIBold
)

func main() {
	// Save original command line for exit display
	startupCommandLine = "pigo " + strings.Join(os.Args[1:], " ")

	cfg := config.Load()
	if cfg.WorkDir == "" {
		cfg.WorkDir, _ = os.Getwd()
	}

	// Handle Ctrl+C gracefully — auto-save session before exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		goodbye()
		os.Exit(0)
	}()

	ag = agent.New(cfg)
	ag.SetThinking(agent.ThinkingLevel(cfg.ThinkingLevel))

	// CLI argument parsing (supports flags before prompt)
	args := os.Args[1:]
	promptParts := []string{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help", "-h":
			showHelp()
			goodbye()
			return
		case "--version", "-v":
			fmt.Println("pigo v0.1.0 — pi in Go")
			goodbye()
			return
		case "--continue", "-c":
			cfg.Continue = true
		case "--resume", "-r":
			handleResume()
			goodbye()
			return
		case "--no-session":
			cfg.NoSession = true
			ag = agent.New(cfg)
			ag.SetThinking(agent.ThinkingLevel(cfg.ThinkingLevel))
		case "--name", "-n":
			i++
			if i < len(args) {
				cfg.SessionName = args[i]
			}
		case "--session":
			i++
			if i < len(args) {
				cfg.SessionPath = args[i]
			}
		case "--model":
			i++
			if i < len(args) {
				if _, ok := agent.DeepSeekModels[args[i]]; ok {
					ag.SwitchModel(args[i])
				}
			}
		case "--thinking":
			i++
			if i < len(args) {
				ag.SetThinking(agent.ThinkingLevel(args[i]))
			}
		default:
			if strings.HasPrefix(arg, "-") {
				continue
			}
			promptParts = append(promptParts, arg)
		}
	}

	// Handle --continue
	if cfg.Continue {
		handleContinue()
		if len(promptParts) == 0 {
			// Dropped into interactive mode
			runInteractive()
			goodbye()
			return
		}
	}

	// Handle --session (load specific session)
	if cfg.SessionPath != "" {
		handleLoadSession(cfg.SessionPath)
		if len(promptParts) == 0 {
			runInteractive()
			goodbye()
			return
		}
	}

	// If there's a prompt on the CLI, dispatch it (non-interactive)
	if len(promptParts) > 0 {
		input := strings.Join(promptParts, " ")
		dispatch(input)
		goodbye()
		return
	}

	// Otherwise, interactive mode
	runInteractive()
	goodbye()
}

func runInteractive() {
	// Banner
	fmt.Printf("%s╔══════════════════════════════════════════╗%s\n", ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %s🐹 PiGo%s — %spi in Go%s              %s║%s\n",
		ANSICyan, ANSIReset, ANSIBold, ANSIReset, ANSIGray, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s╠══════════════════════════════════════════╣%s\n", ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %sModel:%s   %-30s %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, ag.Model(), ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %sThink:%s   %-30s %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, ag.Thinking(), ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %sDir:%s     %-30s %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, shorten(workDir(), 30), ANSICyan, ANSIReset)
	// Session info
	if s := ag.Session(); s != nil {
		sid := s.ID
		if len(sid) > 12 {
			sid = sid[:12]
		}
		fmt.Printf("%s║%s  %sSession:%s %-28s %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, sid, ANSICyan, ANSIReset)
	} else if ag.IsEphemeral() {
		fmt.Printf("%s║%s  %sSession:%s %s(ephemeral)%s         %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, ANSIGray, ANSIReset, ANSICyan, ANSIReset)
	}
	fmt.Printf("%s╠══════════════════════════════════════════╣%s\n", ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %s/model  /thinking  /self  /repair%s   %s║%s\n",
		ANSICyan, ANSIReset, ANSIYellow, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %s/session /save  /load  /resume%s      %s║%s\n",
		ANSICyan, ANSIReset, ANSIYellow, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %s/mode   /multiline  /reload  /quit%s  %s║%s\n",
		ANSICyan, ANSIReset, ANSIYellow, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s╚══════════════════════════════════════════╝%s\n", ANSICyan, ANSIReset)

	// Show footer on startup
	ag.Footer()

	// Multi-line hint
	fmt.Printf("\n%s  ESC → 打断+追加提示  │  \\ → 续行  │  \\e → 编辑器  │  ``` → 代码块  │  /multiline → 全屏编辑%s\n",
		ANSIGray, ANSIReset)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n%s▸%s ", ANSIGreen, ANSIReset)
		if !scanner.Scan() {
			goodbye()
			break
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Empty line — skip
		if trimmed == "" {
			continue
		}

		// ── Triple-backtick block mode ──
		if trimmed == "```" {
			input := readBacktickBlock(scanner)
			if input == "" {
				continue
			}
			dispatch(input)
			continue
		}

		// ── \e suffix: open editor ──
		if strings.HasSuffix(trimmed, "\\e") {
			prefix := strings.TrimSuffix(trimmed, "\\e")
			prefix = strings.TrimSpace(prefix)
			input := openEditor(prefix)
			if input == "" {
				continue
			}
			dispatch(input)
			continue
		}

		// ── \ continuation: line-numbered multi-line ──
		if strings.HasSuffix(trimmed, "\\") {
			input := readContinuation(scanner, strings.TrimSuffix(trimmed, "\\"))
			if input == "" {
				continue
			}
			dispatch(input)
			continue
		}

		// ── Normal single-line ──
		dispatch(trimmed)
	}
}

// readContinuation reads multi-line input with \ line continuations.
// First line already had its trailing \ stripped.
func readContinuation(scanner *bufio.Scanner, firstLine string) string {
	var lines []string
	lines = append(lines, strings.TrimSpace(firstLine))
	lineNo := 2

	for {
		fmt.Printf("%s%2d│%s ", ANSIGray, lineNo, ANSIReset)
		if !scanner.Scan() {
			goodbye()
			os.Exit(0)
		}
		next := scanner.Text()
		trimmed := strings.TrimSpace(next)

		// Empty line in continuation: end the block
		if trimmed == "" {
			break
		}

		// \e mid-block: open editor with accumulated lines
		if strings.HasSuffix(trimmed, "\\e") {
			prefix := strings.TrimSuffix(trimmed, "\\e")
			if strings.TrimSpace(prefix) != "" {
				lines = append(lines, strings.TrimSpace(prefix))
			}
			accumulated := strings.Join(lines, "\n")
			result := openEditor(accumulated)
			if result != "" {
				return result
			}
			// If editor returned empty (user cancelled), keep going
			fmt.Printf("%s  (继续输入，空行结束)%s\n", ANSIGray, ANSIReset)
			continue
		}

		if strings.HasSuffix(trimmed, "\\") {
			lines = append(lines, strings.TrimSuffix(trimmed, "\\"))
			lineNo++
			continue
		}
		lines = append(lines, next)
		break
	}

	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	return strings.Join(lines, "\n")
}

// readBacktickBlock reads a triple-backtick code block.
// The opening ``` has already been consumed.
func readBacktickBlock(scanner *bufio.Scanner) string {
	fmt.Printf("%s``` 代码块模式 — 再输入 ``` 结束%s\n", ANSIYellow, ANSIReset)
	var lines []string
	lineNo := 1

	for {
		fmt.Printf("%s%2d│%s ", ANSIGray, lineNo, ANSIReset)
		if !scanner.Scan() {
			goodbye()
			os.Exit(0)
		}
		next := scanner.Text()
		trimmed := strings.TrimSpace(next)

		if trimmed == "```" {
			if len(lines) == 0 {
				return ""
			}
			result := strings.Join(lines, "\n")
			fmt.Printf("%s✓ 代码块 (%d 行)%s\n", ANSIGreen, len(lines), ANSIReset)
			return result
		}

		// \e mid-block: open editor with accumulated lines
		if strings.HasSuffix(trimmed, "\\e") {
			prefix := strings.TrimSuffix(trimmed, "\\e")
			if strings.TrimSpace(prefix) != "" {
				lines = append(lines, strings.TrimSpace(prefix))
			}
			accumulated := strings.Join(lines, "\n")
			result := openEditor(accumulated)
			if result != "" {
				return result
			}
			fmt.Printf("%s  (继续输入代码块，``` 结束)%s\n", ANSIGray, ANSIReset)
			continue
		}

		lines = append(lines, next)
		lineNo++
	}
}

// openEditor opens the user's preferred editor for multi-line input.
// seed is pre-filled content (may be empty).
func openEditor(seed string) string {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "nano"
	}

	tmpFile, err := os.CreateTemp("", "pigo-input-*.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s✗ 无法创建临时文件: %v%s\n", ANSIRed, err, ANSIReset)
		return ""
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if seed != "" {
		tmpFile.WriteString(seed)
	}
	tmpFile.Close()

	fmt.Printf("%s📝 打开编辑器: %s %s%s\n", ANSIYellow, editor, tmpPath, ANSIReset)

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s✗ 编辑器退出异常: %v%s\n", ANSIRed, err, ANSIReset)
		return ""
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s✗ 无法读取编辑内容: %v%s\n", ANSIRed, err, ANSIReset)
		return ""
	}

	content := strings.TrimSpace(string(data))
	if content == "" || content == seed {
		return ""
	}

	lines := strings.Count(content, "\n") + 1
	fmt.Printf("%s✓ 已读取 %d 行%s\n", ANSIGreen, lines, ANSIReset)
	return content
}

func goodbye() {
	if ag != nil && !ag.IsEphemeral() {
		// Auto-save session before exit
		ag.SaveSession("")
		s := ag.Session()
		if s != nil {
			shortID := s.ID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			fmt.Printf("\n%s📋 Session: %s%s%s%s  (%d messages)%s\n",
				ANSIGray, ANSIReset, ANSIYellow, shortID, ANSIGray, s.Count(), ANSIReset)
			fmt.Printf("%s🔄 Resume:  %spigo --session %s%s\n",
				ANSIGray, ANSIYellow, shortID, ANSIReset)
		}
	}
	fmt.Printf("\n%s🚪 启动命令: %s%s%s%s\n", ANSIGray, ANSIReset, ANSIYellow, startupCommandLine, ANSIReset)
}

func showHelp() {
	fmt.Printf("%s🐹 PiGo%s — %spi in Go%s\n\n", ANSIBold, ANSIReset, ANSIGray, ANSIReset)
	fmt.Printf("Usage: pigo [options] [prompt]\n\n")
	fmt.Printf("%sOptions:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  --help, -h        Show this help\n")
	fmt.Printf("  --version, -v     Show version\n")
	fmt.Printf("  --model <name>    Set model for single-shot\n")
	fmt.Printf("  --thinking <lvl>  Set thinking level\n")
	fmt.Printf("  --continue, -c    Continue most recent session\n")
	fmt.Printf("  --resume, -r      Browse and select from past sessions\n")
	fmt.Printf("  --session <id>    Load specific session by ID prefix\n")
	fmt.Printf("  --name <name>     Set session display name\n")
	fmt.Printf("  --no-session      Ephemeral mode (don't save)\n")
	fmt.Printf("\n%sInteractive Commands:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  %s/model <name>%s     Switch model\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/models%s           List available models\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/thinking <lvl>%s   Set thinking: off/low/medium/high/max\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/self%s             Self-iterate & rebuild PiGo\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/repair <desc>%s    Auto-repair a bug\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/autorepair [on|off]%s Toggle auto-repair on error\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/mode%s             Show current mode, model, thinking\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/reload%s           Reload context files, tools, and config\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/multiline%s        Open editor for multi-line input\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/session%s          Show current session info\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/save [name]%s      Save and name current session\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/load <id>%s        Load a session by ID prefix\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/resume%s           Browse and pick a session to resume\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/help%s             Show this help\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/quit%s             Exit\n", ANSIYellow, ANSIReset)
	fmt.Printf("\n%sMulti-line Input:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  %s\\%s at end of line       Continue on next line\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s\\e%s at end of line      Open editor with current content\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s```%s on its own line    Start code block (``` to end)\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/multiline%s            Open empty editor for input\n", ANSIYellow, ANSIReset)
	fmt.Printf("\n%sExamples:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  pigo                           Interactive mode\n")
	fmt.Printf("  pigo -c                        Continue last session\n")
	fmt.Printf("  pigo -r                        Browse old sessions\n")
	fmt.Printf("  pigo --no-session \"query\"      Ephemeral one-shot\n")
	fmt.Printf("  pigo --session abc123 \"query\"  Resume specific session\n")
}

// ─── ESC Interrupt Support ─────────────────────────────────────

// startESCListener reads stdin byte-by-byte while in raw mode.
// On ESC or Ctrl+C, it cancels the context to interrupt the API call.
func startESCListener(cancel context.CancelFunc) {
	var buf [1]byte
	for {
		select {
		case <-escDone:
			return
		default:
		}
		// Set short deadline so we can check escDone periodically
		os.Stdin.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := os.Stdin.Read(buf[:])
		if err != nil {
			// Timeout or error — loop back to check escDone
			continue
		}
		if n > 0 && (buf[0] == 0x1b || buf[0] == 0x03) {
			cancel()
			return
		}
	}
}

// runWithESC wraps an agent API call with ESC interrupt support.
// Puts terminal in raw mode, listens for ESC/Ctrl+C, restores on return.
// If cancelled, prompts the user for a follow-up instruction.
func runWithESC(input string) {
	ctx, cancel := context.WithCancel(context.Background())
	escCancel = cancel
	escDone = make(chan struct{})

	// Switch to raw mode for ESC detection
	oldState, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
	if rawErr == nil {
		go startESCListener(cancel)
	}

	result, err := ag.Run(ctx, input)

	// Signal ESC listener to stop, then restore terminal
	if rawErr == nil {
		close(escDone)
		term.Restore(int(os.Stdin.Fd()), oldState)
	}

	if err != nil {
		if err == context.Canceled {
			fmt.Fprintf(os.Stderr, "\n%s⏎ 打断%s — 追加提示词 (回车跳过): %s", ANSIYellow, ANSIReset, ANSIGray)
			// Read follow-up input in cooked mode
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				followUp := strings.TrimSpace(scanner.Text())
				if followUp != "" {
					fmt.Fprintf(os.Stderr, "%s", ANSIReset)
					fmt.Printf("\n%s▸%s %s\n", ANSIGreen, ANSIReset, followUp)
					// Re-run with follow-up appended as steering message
					runWithESC(input + "\n\n[用户追加]" + followUp)
					return
				}
			}
			fmt.Fprintf(os.Stderr, "%s", ANSIReset)
		} else {
			fmt.Fprintf(os.Stderr, "\n%s✗ %v%s\n", ANSIRed, err, ANSIReset)
			handleRunError(err)
		}
		return
	}

	_ = result
}

func runSelf() {
	ctx, cancel := context.WithCancel(context.Background())
	escCancel = cancel
	escDone = make(chan struct{})

	oldState, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
	if rawErr == nil {
		go startESCListener(cancel)
	}

	if err := ag.SelfIterate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ANSIRed, ANSIReset, err)
	}

	if rawErr == nil {
		close(escDone)
		term.Restore(int(os.Stdin.Fd()), oldState)
	}

	fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
	if err := ag.Rebuild(); err != nil {
		fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
	} else {
		fmt.Printf("%s✓ Rebuilt!%s Restart to use new version.\n", ANSIGreen, ANSIReset)
	}
}

func runRepair(desc string) {
	ctx, cancel := context.WithCancel(context.Background())
	escCancel = cancel
	escDone = make(chan struct{})

	oldState, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
	if rawErr == nil {
		go startESCListener(cancel)
	}

	if err := ag.AutoRepair(ctx, desc); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ANSIRed, ANSIReset, err)
	}

	if rawErr == nil {
		close(escDone)
		term.Restore(int(os.Stdin.Fd()), oldState)
	}

	fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
	if err := ag.Rebuild(); err != nil {
		fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
	}
}

func dispatch(input string) {
	switch {
	case input == "/quit" || input == "/exit":
		goodbye()
		os.Exit(0)

	case input == "/help":
		showHelp()

	case input == "/models":
		fmt.Printf("\n%sAvailable Models:%s\n", ANSIBold, ANSIReset)
		for name, desc := range agent.DeepSeekModels {
			mark := " "
			if name == ag.Model() {
				mark = "▶"
			}
			cotTag := "  "
			if agent.CoTModels[name] {
				cotTag = "🧠"
			}
			fmt.Printf(" %s %s %-28s %s\n", mark, cotTag, name, desc)
		}
		fmt.Printf("\n%s🧠 = 支持线上 CoT 思考链%s\n", ANSICyan, ANSIReset)

	case strings.HasPrefix(input, "/model "):
		name := strings.TrimSpace(strings.TrimPrefix(input, "/model "))
		if _, ok := agent.DeepSeekModels[name]; !ok {
			fmt.Printf("%sUnknown model:%s %s. Use %s/models%s to list.\n", ANSIRed, ANSIReset, name, ANSIYellow, ANSIReset)
			return
		}
		ag.SwitchModel(name)
		fmt.Printf("%s✓%s Model: %s%s%s\n", ANSIGreen, ANSIReset, ANSIBold, name, ANSIReset)

	case strings.HasPrefix(input, "/thinking "):
		level := strings.TrimSpace(strings.TrimPrefix(input, "/thinking "))
		ag.SetThinking(agent.ThinkingLevel(level))
		fmt.Printf("%s✓%s Thinking: %s%s%s\n", ANSIGreen, ANSIReset, ANSIBold, level, ANSIReset)

	case input == "/autorepair" || strings.HasPrefix(input, "/autorepair "):
		arg := strings.TrimSpace(strings.TrimPrefix(input, "/autorepair "))
		switch arg {
		case "on":
			ag.SetAutoRepair(true)
			fmt.Printf("%s✓%s Auto-repair: %sON%s\n", ANSIGreen, ANSIReset, ANSIBold, ANSIReset)
		case "off":
			ag.SetAutoRepair(false)
			fmt.Printf("%s✓%s Auto-repair: %sOFF%s\n", ANSIGreen, ANSIReset, ANSIGray, ANSIReset)
		default:
			status := "OFF"
			if ag.AutoRepairEnabled() {
				status = "ON"
			}
			fmt.Printf("%sAuto-repair:%s %s\n", ANSIGray, ANSIReset, status)
		}

	case input == "/mode":
		mode, model, thinking := ag.Mode(), ag.Model(), ag.Thinking()
		fmt.Printf("\n%sMode:%s %s     %sModel:%s %s     %sThinking:%s %s\n",
			ANSIGray, ANSIReset, mode,
			ANSIGray, ANSIReset, model,
			ANSIGray, ANSIReset, thinking)

	case input == "/reload":
		summary, err := ag.Reload()
		if err != nil {
			fmt.Printf("%s✗%s %v\n", ANSIRed, ANSIReset, err)
		} else {
			fmt.Printf("%s✓%s %s\n", ANSIGreen, ANSIReset, summary)
		}

	case input == "/multiline":
		result := openEditor("")
		if result == "" {
			fmt.Printf("%s  已取消%s\n", ANSIGray, ANSIReset)
		} else {
			dispatch(result)
		}

	// ─── Session Commands ─────────────────────────────────────
	case input == "/session":
		s := ag.Session()
		if s == nil {
			if ag.IsEphemeral() {
				fmt.Printf("\n%sNo session — ephemeral mode%s\n", ANSIGray, ANSIReset)
			} else {
				fmt.Printf("\n%sNo active session — send a message or /save to create one%s\n", ANSIGray, ANSIReset)
			}
			return
		}
		fmt.Printf("\n%s╭─ Session ──────────────────────────%s\n", ANSICyan, ANSIReset)
		fmt.Printf("%s│%s ID:       %s\n", ANSICyan, ANSIReset, s.ID)
		fmt.Printf("%s│%s Name:     %s\n", ANSICyan, ANSIReset, s.Name)
		fmt.Printf("%s│%s File:     %s\n", ANSICyan, ANSIReset, s.FilePath)
		fmt.Printf("%s│%s Messages: %d\n", ANSICyan, ANSIReset, s.Count())
		fmt.Printf("%s│%s Created:  %s\n", ANSICyan, ANSIReset, truncate(s.CreatedAt, 19))
		fmt.Printf("%s│%s Updated:  %s\n", ANSICyan, ANSIReset, truncate(s.UpdatedAt, 19))
		fmt.Printf("%s╰───────────────────────────────────%s\n", ANSICyan, ANSIReset)

	case strings.HasPrefix(input, "/save"):
		name := strings.TrimSpace(strings.TrimPrefix(input, "/save "))
		if err := ag.SaveSession(name); err != nil {
			fmt.Printf("%s✗ %v%s\n", ANSIRed, err, ANSIReset)
			return
		}
		fmt.Printf("%s✓ Session saved%s\n", ANSIGreen, ANSIReset)

	case strings.HasPrefix(input, "/load "):
		idPrefix := strings.TrimSpace(strings.TrimPrefix(input, "/load "))
		handleLoadSession(idPrefix)

	case input == "/resume":
		handleResume()

	case input == "/self":
		runSelf()

	case strings.HasPrefix(input, "/repair"):
		desc := strings.TrimSpace(strings.TrimPrefix(input, "/repair "))
		if desc == "" {
			desc = "fix known issues"
		}
		runRepair(desc)

	default:
		runWithESC(input)
	}
}

// ─── Session Handlers ──────────────────────────────────────────

// ─── Auto-Repair Trigger ──────────────────────────────────────
var autoRepairAttempt int

func handleRunError(err error) {
	errMsg := err.Error()

	if ag.AutoRepairEnabled() {
		if autoRepairAttempt >= 3 {
			fmt.Fprintf(os.Stderr, "%s⚠ Auto-repair limit reached (3 attempts). Use /repair manually.%s\n", ANSIYellow, ANSIReset)
			autoRepairAttempt = 0
			return
		}
		autoRepairAttempt++
		fmt.Fprintf(os.Stderr, "%s🔧 Auto-repair [%d/3]...%s\n", ANSIYellow, autoRepairAttempt, ANSIReset)
		if err := ag.AutoRepair(context.Background(), errMsg); err != nil {
			fmt.Fprintf(os.Stderr, "%sRepair error:%s %v\n", ANSIRed, ANSIReset, err)
			return
		}
		fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
		if err := ag.Rebuild(); err != nil {
			fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
			return
		}
		fmt.Fprintf(os.Stderr, "%s✓ Rebuilt — retry your command%s\n", ANSIGreen, ANSIReset)
		autoRepairAttempt = 0 // reset on success
	} else {
		fmt.Fprintf(os.Stderr, "%s💡 %sr%s to auto-repair, any key to continue%s\n", ANSIGray, ANSIYellow, ANSIGray, ANSIReset)
		// Read single key (non-blocking scan)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			key := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if key == "r" {
				if err := ag.AutoRepair(context.Background(), errMsg); err != nil {
					fmt.Fprintf(os.Stderr, "%sRepair error:%s %v\n", ANSIRed, ANSIReset, err)
					return
				}
				fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
				if err := ag.Rebuild(); err != nil {
					fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
				} else {
					fmt.Fprintf(os.Stderr, "%s✓ Rebuilt — retry your command%s\n", ANSIGreen, ANSIReset)
				}
			}
		}
	}
}

func handleContinue() {
	s, err := ag.SessionManager().Latest(workDir())
	if err != nil {
		fmt.Printf("%sNo sessions to continue%s\n", ANSIGray, ANSIReset)
		return
	}
	if err := ag.ResumeSession(s); err != nil {
		fmt.Printf("%s✗ %v%s\n", ANSIRed, err, ANSIReset)
		return
	}
	fmt.Printf("%s✓ Resumed session %s (%d messages)%s\n",
		ANSIGreen, s.ID[:8], s.Count(), ANSIReset)
}

func handleLoadSession(idPrefix string) {
	s, err := ag.SessionManager().LoadByID(workDir(), idPrefix)
	if err != nil {
		fmt.Printf("%s✗ Session '%s' not found%s\n", ANSIRed, idPrefix, ANSIReset)
		return
	}
	if err := ag.ResumeSession(s); err != nil {
		fmt.Printf("%s✗ %v%s\n", ANSIRed, err, ANSIReset)
		return
	}
	fmt.Printf("%s✓ Loaded session %s (%d messages)%s\n",
		ANSIGreen, s.ID[:8], s.Count(), ANSIReset)
	// Print last few messages as context
	entries := s.Messages()
	start := 0
	if len(entries) > 4 {
		start = len(entries) - 4
	}
	fmt.Println()
	for _, e := range entries[start:] {
		prefix := fmt.Sprintf("%s[%s]%s", ANSIGray, e.Role[:4], ANSIReset)
		content := e.Content
		if len(content) > 100 {
			content = content[:97] + "..."
		}
		fmt.Printf("%s %s\n", prefix, content)
	}
}

func handleResume() {
	sessions, err := ag.SessionManager().List(workDir())
	if err != nil || len(sessions) == 0 {
		fmt.Printf("%sNo saved sessions%s\n", ANSIGray, ANSIReset)
		return
	}

	fmt.Printf("\n%s╭─ Sessions ──────────────────────────%s\n", ANSICyan, ANSIReset)
	// Sort by updated desc
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})

	for i, s := range sessions {
		mark := ""
		if i == 0 {
			mark = "▶"
		}
		id := s.ID
		if len(id) > 8 {
			id = id[:8]
		}
		name := s.Name
		if name == "" {
			name = "(unnamed)"
		}
		firstMsg := ""
		if len(s.Entries) > 0 {
			firstMsg = s.Entries[0].Content
			if len(firstMsg) > 50 {
				firstMsg = firstMsg[:47] + "..."
			}
		}
		fmt.Printf("%s %s %-10s %-20s %s\n", mark, id, name, truncate(s.UpdatedAt, 19), firstMsg)
	}
	fmt.Printf("%s╰─────────────────────────────────────%s\n", ANSICyan, ANSIReset)
	fmt.Printf("\n%sType /load <id> to resume one%s\n", ANSIYellow, ANSIReset)
}

// truncate safely truncates string s to n chars.
func truncate(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func workDir() string {
	wd, _ := os.Getwd()
	return wd
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n+3:]
}
