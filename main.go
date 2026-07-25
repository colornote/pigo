package main

import (
	"bufio"
	"fmt"
	"os"
	"pigo/agent"
	"pigo/config"
	"sort"
	"strings"
)

// ANSI color codes for TUI
const (
	ANSIReset  = "\033[0m"
	ANSIRed    = "\033[31m"
	ANSIGreen  = "\033[32m"
	ANSIYellow = "\033[33m"
	ANSICyan   = "\033[36m"
	ANSIGray   = "\033[90m"
	ANSIBold   = "\033[1m"
)

var ag *agent.Agent
var startupCommandLine string

func main() {
	// Save original command line for exit display
	startupCommandLine = "pigo " + strings.Join(os.Args[1:], " ")

	cfg := config.Load()
	if cfg.WorkDir == "" {
		cfg.WorkDir, _ = os.Getwd()
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
	fmt.Printf("%s║%s  %s/mode   /help      /quit%s            %s║%s\n",
		ANSICyan, ANSIReset, ANSIYellow, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s╚══════════════════════════════════════════╝%s\n", ANSICyan, ANSIReset)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n%s>%s ", ANSIGreen, ANSIReset)
		if !scanner.Scan() {
			goodbye()
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		dispatch(input)
	}
}

func goodbye() {
	if ag != nil && !ag.IsEphemeral() {
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
	fmt.Printf("  %s/mode%s             Show current mode, model, thinking\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/session%s          Show current session info\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/save [name]%s      Save and name current session\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/load <id>%s        Load a session by ID prefix\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/resume%s           Browse and pick a session to resume\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/help%s             Show this help\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/quit%s             Exit\n", ANSIYellow, ANSIReset)
	fmt.Printf("\n%sExamples:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  pigo                           Interactive mode\n")
	fmt.Printf("  pigo -c                        Continue last session\n")
	fmt.Printf("  pigo -r                        Browse old sessions\n")
	fmt.Printf("  pigo --no-session \"query\"      Ephemeral one-shot\n")
	fmt.Printf("  pigo --session abc123 \"query\"  Resume specific session\n")
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

	case input == "/mode":
		mode, model, thinking := ag.Mode(), ag.Model(), ag.Thinking()
		fmt.Printf("\n%sMode:%s %s     %sModel:%s %s     %sThinking:%s %s\n",
			ANSIGray, ANSIReset, mode,
			ANSIGray, ANSIReset, model,
			ANSIGray, ANSIReset, thinking)

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
		if err := ag.SelfIterate(); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ANSIRed, ANSIReset, err)
		}
		fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
		if err := ag.Rebuild(); err != nil {
			fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
		} else {
			fmt.Printf("%s✓ Rebuilt!%s Restart to use new version.\n", ANSIGreen, ANSIReset)
		}

	case strings.HasPrefix(input, "/repair"):
		desc := strings.TrimSpace(strings.TrimPrefix(input, "/repair "))
		if desc == "" {
			desc = "fix known issues"
		}
		if err := ag.AutoRepair(desc); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", ANSIRed, ANSIReset, err)
		}
		fmt.Printf("\n%s🔨 Rebuilding...%s\n", ANSIYellow, ANSIReset)
		if err := ag.Rebuild(); err != nil {
			fmt.Fprintf(os.Stderr, "%sRebuild failed:%s %v\n", ANSIRed, ANSIReset, err)
		}

	default:
		_, err := ag.Run(input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n%s✗ %v%s\n", ANSIRed, err, ANSIReset)
			fmt.Fprintf(os.Stderr, "%s💡 Try /repair to auto-fix%s\n", ANSIYellow, ANSIReset)
		}
	}
}

// ─── Session Handlers ──────────────────────────────────────────

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
