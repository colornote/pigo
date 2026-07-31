package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"pigo/agent"
	"pigo/config"
	"pigo/session"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/peterh/liner"
	"golang.org/x/term"

	"golang.org/x/sys/unix"
)

var ag *agent.Agent
var startupCommandLine string

// ESC interrupt support
var escDone chan struct{}

const version = "v0.1.0"

// origTermState captures the terminal state at startup so goodbye() can
// restore it. liner.NewLiner() puts the tty in raw mode; the exit paths
// (/quit, Ctrl+C, Ctrl+D, SIGINT) call os.Exit(0), which skips the deferred
// ls.Close() that would restore the terminal. Without this, pigo leaves the
// tty raw — no echo, Enter broken — and the next run appears to auto-exit.
var origTermState *term.State

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

	// Capture the terminal state before anything (liner, raw mode) changes it,
	// so goodbye() can always restore a clean terminal on exit.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		if st, err := term.GetState(int(os.Stdin.Fd())); err == nil {
			origTermState = st
		}
	}

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
			fmt.Println("pigo " + version + " — pi in Go")
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
			recreateAgent(cfg)
		case "--api-key":
			i++
			if i < len(args) {
				cfg.APIKey = args[i]
				recreateAgent(cfg)
			}
		case "--session-dir":
			i++
			if i < len(args) {
				cfg.SessionDir = args[i]
				recreateAgent(cfg)
			}
		case "--no-context-files", "-nc":
			cfg.NoContextFiles = true
			cfg.LoadSystemPrompt()
		case "--list-models":
			printModels()
			goodbye()
			return
		case "-p", "--print":
			// Non-interactive: print response and exit (stdin is merged below)
			cfg.Print = true
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
			if strings.HasPrefix(arg, "@") {
				// @file argument: include file contents in the prompt (pi: `pi @file "msg"`)
				promptParts = append(promptParts, fileArgPrompt(arg[1:]))
				continue
			}
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

	// If there's a prompt on the CLI, dispatch it (non-interactive).
	// Merge piped stdin into the prompt like pi -p does:
	//   cat README.md | pigo -p "Summarize this text"
	if len(promptParts) > 0 {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			if data, err := io.ReadAll(os.Stdin); err == nil {
				if s := strings.TrimSpace(string(data)); s != "" {
					promptParts = append(promptParts, "\n\n[stdin]\n"+s)
				}
			}
		}
		input := strings.Join(promptParts, " ")
		dispatch(input, nil)
		goodbye()
		return
	}

	// Print mode with piped stdin only (no CLI args): stdin is the prompt.
	if cfg.Print && !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			dispatch(strings.TrimSpace(string(data)), nil)
			goodbye()
			return
		}
		fmt.Fprintf(os.Stderr, "%s✗ --print requires a prompt or piped stdin%s\n", ANSIRed, ANSIReset)
		goodbye()
		return
	}

	// -p with no prompt and no piped stdin: error out instead of silently
	// dropping into interactive mode (pi -p requires a prompt or stdin).
	if cfg.Print && len(promptParts) == 0 {
		fmt.Fprintf(os.Stderr, "%s✗ --print requires a prompt or piped stdin%s\n", ANSIRed, ANSIReset)
		goodbye()
		return
	}

	// Otherwise, interactive mode
	runInteractive()
	goodbye()
}

// recreateAgent rebuilds the agent after config-affecting flags change
// (--no-session, --api-key, --session-dir), preserving the current
// model and thinking level across the rebuild.
func recreateAgent(cfg *config.Config) {
	prevModel := ag.Model()
	prevThinking := ag.Thinking()
	ag = agent.New(cfg)
	ag.SwitchModel(prevModel)
	ag.SetThinking(prevThinking)
}

// printBanner renders the startup banner: a compact two-column info grid.
// No box drawing — avoids CJK width misalignment entirely.
func printBanner() {
	model := ag.Model()
	think := string(ag.Thinking())
	dir := shorten(workDir(), 20)
	sess := "(ephemeral)"
	if s := ag.Session(); s != nil {
		sess = truncate(s.ID, 12)
	} else if ag.IsEphemeral() {
		sess = "(ephemeral)"
	}

	// Two-column grid. Left column padded to a fixed display width so the
	// right column starts at the same offset on every row (CJK-aware).
	left := func(label, val string) string {
		return agent.PadDisplay(fmt.Sprintf("%s  %s", label, val), 26)
	}
	right := func(label, val string) string {
		return fmt.Sprintf("%s  %s", label, val)
	}
	lbl := func(s string) string { return fmt.Sprintf("%s%s%s", ANSIGray, s, ANSIReset) }
	sep := fmt.Sprintf("%s%s%s", ANSIGray, strings.Repeat("─", 40), ANSIReset)
	cmds := fmt.Sprintf("%s/model /thinking /self /repair%s  ·  %s/session /save /load /resume%s  ·  %s/mode /multiline /reload /quit%s",
		ANSIYellow, ANSIReset, ANSIYellow, ANSIReset, ANSIYellow, ANSIReset)

	fmt.Println()
	fmt.Printf("  %sπ%s %sPiGo%s %s%s%s   %s·  pi in Go — coding agent%s\n",
		ANSICyan, ANSIReset, ANSIBold, ANSIReset,
		ANSIGray, version, ANSIReset, ANSIGray, ANSIReset)
	fmt.Printf("  %s  %s\n", lbl(left("model", model)), right("dir", dir))
	fmt.Printf("  %s  %s\n", lbl(left("think", think)), right("session", sess))
	fmt.Println(sep)
	fmt.Println(cmds)
	fmt.Println()
}

func runInteractive() {
	printBanner()

	// Show footer on startup
	ag.Footer()

	// Multi-line hint
	fmt.Printf("\n%s  ESC → 打断+追加提示  │  \\ → 续行  │  \\e → 编辑器  │  ``` → 代码块  │  /multiline → 全屏编辑%s\n",
		ANSIGray, ANSIReset)

	// Liner-based line editor: rune-safe backspace for CJK input
	// (macOS termios lacks IUTF8 — cooked mode deletes bytes, corrupting
	// multi-byte UTF-8. liner deletes whole glyphs and is cross-platform.)
	ls := liner.NewLiner()
	defer ls.Close()
	ls.SetCtrlCAborts(true)
	ls.SetTabCompletionStyle(liner.TabPrints)

	// Persistent command history
	home, _ := os.UserHomeDir()
	histFile := filepath.Join(home, ".pigo", "history")
	if f, err := os.Open(histFile); err == nil {
		ls.ReadHistory(f)
		f.Close()
	}
	defer func() {
		if f, err := os.Create(histFile); err == nil {
			ls.WriteHistory(f)
			f.Close()
		}
	}()

	// reader is kept for stdinDrain after raw-mode ESC interrupts
	reader := bufio.NewReaderSize(os.Stdin, 65536)
	for {
		// Prompt must be plain text — liner counts glyphs for cursor
		// positioning, rejects control characters (incl. \n), and has no
		// ANSI escape filtering. Print the separator ourselves.
		fmt.Println()
		line, err := ls.Prompt("▸ " + promptStatus() + " ")
		if err != nil {
			if err == liner.ErrPromptAborted || err == io.EOF {
				goodbye()
				os.Exit(0)
			}
			fmt.Fprintf(os.Stderr, "%s✗ input error: %v%s\n", ANSIRed, err, ANSIReset)
			goodbye()
			break
		}
		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)

		// Empty line — skip
		if trimmed == "" {
			continue
		}

		// ── Triple-backtick block mode ──
		if trimmed == "```" {
			input := readBacktickBlock(ls)
			if input == "" {
				continue
			}
			dispatch(input, reader)
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
			dispatch(input, reader)
			continue
		}

		// ── \ continuation: line-numbered multi-line ──
		if strings.HasSuffix(trimmed, "\\") {
			input := readContinuation(ls, strings.TrimSuffix(trimmed, "\\"))
			if input == "" {
				continue
			}
			dispatch(input, reader)
			continue
		}

		// Save non-trivial input to history
		if !strings.HasPrefix(trimmed, "/") {
			ls.AppendHistory(trimmed)
		}
		dispatch(trimmed, reader)
	}
}

// readContinuation reads multi-line input with \ line continuations.
// First line already had its trailing \ stripped.
func readContinuation(ls *liner.State, firstLine string) string {
	var lines []string
	lines = append(lines, strings.TrimSpace(firstLine))
	lineNo := 2

	for {
		fmt.Println()
		next, err := ls.Prompt(fmt.Sprintf("%2d│ ", lineNo))
		if err != nil {
			goodbye()
			os.Exit(0)
		}
		next = strings.TrimRight(next, "\r\n")
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
func readBacktickBlock(ls *liner.State) string {
	fmt.Printf("%s``` 代码块模式 — 再输入 ``` 结束%s\n", ANSIYellow, ANSIReset)
	var lines []string
	lineNo := 1

	for {
		fmt.Println()
		next, err := ls.Prompt(fmt.Sprintf("%2d│ ", lineNo))
		if err != nil {
			goodbye()
			os.Exit(0)
		}
		next = strings.TrimRight(next, "\r\n")
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
	// Restore the terminal to its pre-pigo state. os.Exit() paths (/quit,
	// Ctrl+C, Ctrl+D, SIGINT) skip the deferred liner.Close(), which is the
	// only other place the tty gets restored — without this the terminal is
	// left in raw mode and the shell/next pigo run misbehaves.
	if origTermState != nil {
		term.Restore(int(os.Stdin.Fd()), origTermState)
	}
	if ag != nil && !ag.IsEphemeral() {
		// Auto-save session before exit — but only if one was actually
		// created (i.e. at least one message exchanged). Without this
		// check, --help/--version/--list-models and empty interactive
		// sessions leave behind empty session files on every invocation.
		if s := ag.Session(); s != nil {
			ag.SaveSession("")
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
	fmt.Printf("Usage: pigo [options] [@files...] [prompt]\n\n")
	fmt.Printf("%sOptions:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  --help, -h        Show this help\n")
	fmt.Printf("  --version, -v     Show version\n")
	fmt.Printf("  --model <name>    Set model for single-shot\n")
	fmt.Printf("  --thinking <lvl>  Set thinking level\n")
	fmt.Printf("  --print, -p       Non-interactive: print response and exit\n")
	fmt.Printf("  --continue, -c    Continue most recent session\n")
	fmt.Printf("  --resume, -r      Browse and select from past sessions\n")
	fmt.Printf("  --session <id>    Load specific session by ID prefix or .jsonl path\n")
	fmt.Printf("  --session-dir <d> Custom session storage directory\n")
	fmt.Printf("  --name <name>     Set session display name\n")
	fmt.Printf("  --no-session      Ephemeral mode (don't save)\n")
	fmt.Printf("  --no-context-files Disable AGENTS.md/CLAUDE.md loading (-nc)\n")
	fmt.Printf("  --api-key <key>   Override API key (overrides env vars)\n")
	fmt.Printf("  --list-models     List available models and exit\n")
	fmt.Printf("\n%sFile Arguments:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  %s@file%s             Include file contents in the prompt\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %spiped stdin%s         cat file | pigo -p \"prompt\" merges stdin\n", ANSIYellow, ANSIReset)
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
	fmt.Printf("  %s/compact [instr]%s   Summarize old messages to free context\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/session%s          Show current session info\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/name <name>%s      Set session display name\n", ANSIYellow, ANSIReset)
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
	fmt.Printf("  pigo @code.ts @test.ts \"Review\"  Include files in prompt\n")
	fmt.Printf("  cat README.md | pigo -p \"Summarize\"  Non-interactive + stdin\n")
}

// fileArgPrompt loads a @file CLI argument and formats its contents for
// inclusion in the prompt, mirroring pi's `pi @file "message"` behavior.
func fileArgPrompt(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("[@%s: %v]", path, err)
	}
	// Binary/image files can't be inlined — reference the path so the
	// model can read them with the read tool if needed.
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".pdf":
		return fmt.Sprintf("[@%s: binary file — use the read tool if needed]", path)
	}
	content := string(data)
	if len(content) > 40000 {
		content = content[:40000] + "\n...[truncated]"
	}
	return fmt.Sprintf("<file path=%q>\n%s\n</file>", path, content)
}

// ─── ESC Interrupt Support ─────────────────────────────────────

// makeRawInputOnly puts stdin in raw mode for byte-by-byte reading,
// but keeps output processing (OPOST/ONLCR) enabled so \n still maps
// to \r\n — avoids the "staircase" indentation on macOS/Linux.
// Also preserves IUTF8 for multi-byte character handling.
func makeRawInputOnly(fd int) (*term.State, error) {
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	// Re-enable output post-processing: \n → \r\n
	// Also preserve IUTF8 for multi-byte character backspace
	if tios, err := unix.IoctlGetTermios(fd, termiosGet); err == nil {
		tios.Oflag |= unix.OPOST | unix.ONLCR
		tios.Iflag |= iutf8Flag
		unix.IoctlSetTermios(fd, termiosSet, tios)
	}
	return oldState, nil
}

// stdinDrain resets the reader buffer and drains leftover bytes from stdin
// after a raw→cooked terminal transition. Prevents stale input from
// leaking into the next prompt and fixes the "extra Enter needed" bug.
// Bounded by a hard deadline so a continuous input flood (held key,
// paste) can never block the UI forever.
func stdinDrain(reader *bufio.Reader) {
	reader.Reset(os.Stdin)
	// Drain leftover bytes from kernel tty buffer after raw→cooked transition
	fd := int(os.Stdin.Fd())
	buf := make([]byte, 4096)
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		var rset unix.FdSet
		rset.Set(fd)
		tv := unix.Timeval{Sec: 0, Usec: 30000}
		n, _ := unix.Select(fd+1, &rset, nil, nil, &tv)
		if n <= 0 {
			return
		}
		os.Stdin.Read(buf)
	}
}

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
		// Poll stdin with select() so we can check escDone between keys.
		// SetReadDeadline is a no-op on ttys (only sockets/pipes support it) —
		// a bare Read would block forever and leak this goroutine every run.
		fd := int(os.Stdin.Fd())
		var rset unix.FdSet
		rset.Set(fd)
		tv := unix.Timeval{Sec: 0, Usec: 100000}
		n, err := unix.Select(fd+1, &rset, nil, nil, &tv)
		if err != nil || n == 0 {
			continue // timeout or error — loop back to check escDone
		}
		n, err = os.Stdin.Read(buf[:])
		if err != nil || n == 0 {
			continue
		}
		if buf[0] == 0x1b || buf[0] == 0x03 {
			cancel()
			return
		}
		// Check immediately after consuming a byte: during a key flood the
		// top-of-loop check is never reached until input stops, which would
		// stall the close(escDone) handshake and freeze the UI after a run.
		select {
		case <-escDone:
			return
		default:
		}
	}
}

// waitListenerDone waits for the ESC listener to exit, with a safety timeout.
// The listener drains stdin in 100ms select slices, so it must exit promptly;
// a timeout here guards against a stray stall.
func waitListenerDone(listenerDone <-chan struct{}) {
	select {
	case <-listenerDone:
	case <-time.After(2 * time.Second):
	}
}

// runWithESC wraps an agent API call with ESC interrupt support.
// Puts terminal in raw mode, listens for ESC/Ctrl+C, restores on return.
// If cancelled, prompts the user for a follow-up instruction.
func runWithESC(input string, reader *bufio.Reader) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	escDone = make(chan struct{})

	// Switch to raw mode for ESC detection
	oldState, rawErr := makeRawInputOnly(int(os.Stdin.Fd()))
	listenerDone := make(chan struct{})
	if rawErr == nil {
		go func() {
			startESCListener(cancel)
			close(listenerDone)
		}()
	}

	result, err := ag.Run(ctx, input)

	// Signal ESC listener to stop and wait for it to exit.
	if rawErr == nil {
		close(escDone)
		waitListenerDone(listenerDone)
	}

	if err == context.Canceled {
		// ESC interrupt: restore the terminal to its ORIGINAL cooked state
		// (captured at startup, before liner put the tty in raw mode).
		// Restoring to oldState would go back to liner's raw mode, where
		// Enter never completes a line — the follow-up prompt would hang.
		restoreState := oldState
		if origTermState != nil {
			restoreState = origTermState
		}
		if rawErr == nil {
			term.Restore(int(os.Stdin.Fd()), restoreState)
			os.Stdin.SetReadDeadline(time.Time{})
		}
		fmt.Fprintf(os.Stderr, "\n%s⏎ 打断%s — 追加提示词 (回车跳过): %s", ANSIYellow, ANSIReset, ANSIGray)
		// Read follow-up input in cooked mode (canonical: echo on, Enter
		// completes the line). No stdinDrain here — the user often starts
		// typing their follow-up the instant they press ESC, and a drain
		// would swallow those keystrokes.
		scanner := bufio.NewScanner(os.Stdin)
		followUp := ""
		if scanner.Scan() {
			followUp = strings.TrimSpace(scanner.Text())
		}
		fmt.Fprintf(os.Stderr, "%s", ANSIReset)
		if followUp != "" {
			fmt.Printf("\n%s▸%s %s\n", ANSIGreen, ANSIReset, followUp)
		}

		// Return to liner's raw steady state before the next prompt/run.
		// (liner only applies raw mode once in NewLiner — if we leave the
		// tty cooked, the next Prompt would run with tty echo + liner echo
		// doubling every keystroke.)
		if rawErr == nil {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}
		if reader != nil {
			stdinDrain(reader)
		}

		if followUp != "" {
			// Re-run with follow-up appended as steering message
			runWithESC(input+"\n\n[用户追加]"+followUp, reader)
		}
		return
	}

	// Normal completion or other error: restore to the pre-ESC state
	// (liner's raw mode — the interactive loop expects it), then drain.
	if rawErr == nil {
		term.Restore(int(os.Stdin.Fd()), oldState)
		os.Stdin.SetReadDeadline(time.Time{})
		if reader != nil {
			stdinDrain(reader)
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%s✗ %v%s\n", ANSIRed, err, ANSIReset)
		handleRunError(err)
		return
	}

	_ = result
}

// runWithRawTerminal executes fn with stdin in raw mode and an active
// ESC/Ctrl+C interrupt listener, then restores the terminal and drains
// leftover input. Shared by /self and /repair (which both run agent
// commands and rebuild afterwards).
func runWithRawTerminal(reader *bufio.Reader, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	escDone = make(chan struct{})

	oldState, rawErr := makeRawInputOnly(int(os.Stdin.Fd()))
	listenerDone := make(chan struct{})
	if rawErr == nil {
		go func() {
			startESCListener(cancel)
			close(listenerDone)
		}()
	}

	fn(ctx)

	if rawErr == nil {
		close(escDone)
		waitListenerDone(listenerDone)
		term.Restore(int(os.Stdin.Fd()), oldState)
		os.Stdin.SetReadDeadline(time.Time{})
		if reader != nil {
			stdinDrain(reader)
		}
	}
}

func runSelf(reader *bufio.Reader) {
	runWithRawTerminal(reader, func(ctx context.Context) {
		if err := ag.SelfIterate(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ANSIRed, ANSIReset, err)
		}
	})

	fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
	if err := ag.Rebuild(); err != nil {
		fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
	} else {
		fmt.Printf("%s✓ Rebuilt!%s Restart to use new version.\n", ANSIGreen, ANSIReset)
	}
}

func runRepair(desc string, reader *bufio.Reader) {
	runWithRawTerminal(reader, func(ctx context.Context) {
		if err := ag.AutoRepair(ctx, desc); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ANSIRed, ANSIReset, err)
		}
	})

	fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
	if err := ag.Rebuild(); err != nil {
		fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
	}
}

func dispatch(input string, reader *bufio.Reader) {
	switch {
	case input == "/quit" || input == "/exit":
		goodbye()
		os.Exit(0)

	case input == "/help":
		showHelp()

	case input == "/models":
		printModels()

	case input == "/model":
		fmt.Printf("%sCurrent model:%s %s%s%s\n", ANSIBold, ANSIReset, ANSIGreen, ag.Model(), ANSIReset)
		printModels()
		fmt.Printf("%sUse %s/model <name>%s to switch.%s\n", ANSICyan, ANSIYellow, ANSICyan, ANSIReset)

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
			dispatch(result, reader)
		}

	case input == "/compact" || strings.HasPrefix(input, "/compact "):
		instr := strings.TrimSpace(strings.TrimPrefix(input, "/compact "))
		fmt.Printf("%s🧹 Compacting context...%s\n", ANSIYellow, ANSIReset)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		err := ag.Compact(ctx, instr)
		cancel()
		if err != nil {
			fmt.Printf("%s✗ compact: %v%s\n", ANSIRed, err, ANSIReset)
		} else {
			fmt.Printf("%s✓ Context compacted — earlier messages summarized%s\n", ANSIGreen, ANSIReset)
		}

	// ─── Session Commands ─────────────────────────────────────
	case strings.HasPrefix(input, "/name "):
		name := strings.TrimSpace(strings.TrimPrefix(input, "/name "))
		s := ag.Session()
		if s == nil {
			if ag.IsEphemeral() {
				fmt.Printf("%s✗ ephemeral mode — no session%s\n", ANSIRed, ANSIReset)
			} else {
				fmt.Printf("%s✗ no active session — send a message or /save first%s\n", ANSIRed, ANSIReset)
			}
			return
		}
		if name == "" {
			fmt.Printf("%sCurrent session name:%s %s\n", ANSIGray, ANSIReset, s.Name)
			return
		}
		if err := ag.SaveSession(name); err != nil {
			fmt.Printf("%s✗ %v%s\n", ANSIRed, err, ANSIReset)
			return
		}
		fmt.Printf("%s✓ Session named %s%s%s%s\n", ANSIGreen, ANSIReset, ANSIBold, name, ANSIReset)

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
		runSelf(reader)

	case strings.HasPrefix(input, "/repair"):
		desc := strings.TrimSpace(strings.TrimPrefix(input, "/repair "))
		if desc == "" {
			desc = "fix known issues"
		}
		runRepair(desc, reader)

	default:
		runWithESC(input, reader)
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
		fmt.Fprintf(os.Stderr, "%s💡 %sr%s to auto-repair, Enter to continue%s\n", ANSIGray, ANSIYellow, ANSIGray, ANSIReset)
		if readKey(10*time.Second) == "r" {
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

// readKey reads a single key in raw mode and returns a normalized action.
// Waits up to timeout; returns "continue" on timeout/error/any other key.
func readKey(timeout time.Duration) string {
	fd := int(os.Stdin.Fd())
	oldState, err := makeRawInputOnly(fd)
	if err != nil {
		return "continue"
	}
	defer term.Restore(fd, oldState)

	// Wait for input with a real timeout via select(). SetReadDeadline is a
	// no-op on ttys (only sockets/pipes support it) — a bare Read blocks
	// forever, hanging the process at the repair prompt.
	var rset unix.FdSet
	rset.Set(fd)
	tv := unix.Timeval{
		Sec:  int64(timeout / time.Second),
		Usec: int32((timeout % time.Second) / time.Microsecond),
	}
	n, err := unix.Select(fd+1, &rset, nil, nil, &tv)
	if err != nil || n == 0 {
		return "continue"
	}
	var buf [1]byte
	if _, err := os.Stdin.Read(buf[:]); err != nil {
		return "continue"
	}
	if buf[0] == 'r' || buf[0] == 'R' {
		return "r"
	}
	return "continue"
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
	var s *session.Session
	var err error
	// --session <path|id> / /load: a full .jsonl path (or anything path-like)
	// loads directly; otherwise treat it as an ID prefix within the project.
	if strings.HasSuffix(idPrefix, ".jsonl") || strings.Contains(idPrefix, "/") {
		s, err = ag.SessionManager().Load(idPrefix)
	} else {
		s, err = ag.SessionManager().LoadByID(workDir(), idPrefix)
	}
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
		if len([]rune(content)) > 100 {
			content = truncate(content, 97) + "..."
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
			firstMsg = truncate(s.Entries[0].Content, 50)
			if len([]rune(firstMsg)) >= 50 {
				firstMsg += "..."
			}
		}
		fmt.Printf("%s %s %s %s %s\n", mark, id, agent.PadDisplay(name, 20), truncate(s.UpdatedAt, 19), firstMsg)
	}
	fmt.Printf("%s╰─────────────────────────────────────%s\n", ANSICyan, ANSIReset)
	fmt.Printf("\n%sType /load <id> to resume one%s\n", ANSIYellow, ANSIReset)
}

// truncate safely truncates string s to n runes (handles CJK/emoji).
func truncate(s string, n int) string {
	return agent.TruncRunes(s, n)
}

func workDir() string {
	wd, _ := os.Getwd()
	return wd
}

// promptStatus returns a compact model·thinking indicator for the prompt line.
func promptStatus() string {
	return agent.ShortModelName(ag.Model()) + "·" + shortThinking(string(ag.Thinking()))
}

func shortThinking(t string) string {
	switch t {
	case "low":
		return "lo"
	case "medium":
		return "med"
	case "high":
		return "hi"
	}
	return t
}

// printModels lists available models with the active one marked.
// Shared by /models, /model, and --list-models.
func printModels() {
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
}

func shorten(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return "..." + agent.TruncRunesRight(s, n-3)
}
