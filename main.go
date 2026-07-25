package main

import (
	"bufio"
	"fmt"
	"os"
	"pigo/agent"
	"pigo/config"
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

func main() {
	cfg := config.Load()
	if cfg.WorkDir == "" {
		cfg.WorkDir, _ = os.Getwd()
	}

	ag = agent.New(cfg)
	ag.SetThinking(agent.ThinkingLevel(cfg.ThinkingLevel))

	// Banner
	fmt.Printf("%s╔══════════════════════════════════════════╗%s\n", ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %s🐹 PiGo%s — %spi in Go%s              %s║%s\n",
		ANSICyan, ANSIReset, ANSIBold, ANSIReset, ANSIGray, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s╠══════════════════════════════════════════╣%s\n", ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %sModel:%s   %-30s %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, cfg.Model, ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %sThink:%s   %-30s %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, cfg.ThinkingLevel, ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %sDir:%s     %-30s %s║%s\n", ANSICyan, ANSIReset, ANSIGray, ANSIReset, shorten(cfg.WorkDir, 30), ANSICyan, ANSIReset)
	fmt.Printf("%s╠══════════════════════════════════════════╣%s\n", ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %s/model  /thinking  /self  /repair%s   %s║%s\n",
		ANSICyan, ANSIReset, ANSIYellow, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s║%s  %s/mode   /help      /quit%s            %s║%s\n",
		ANSICyan, ANSIReset, ANSIYellow, ANSIReset, ANSICyan, ANSIReset)
	fmt.Printf("%s╚══════════════════════════════════════════╝%s\n", ANSICyan, ANSIReset)

	// Handle CLI flags
	if len(os.Args) > 1 {
		arg := os.Args[1]
		switch arg {
		case "--help", "-h":
			showHelp()
			return
		case "--version", "-v":
			fmt.Println("pigo v0.1.0 — pi in Go")
			return
		}
		input := strings.Join(os.Args[1:], " ")
		dispatch(input)
		return
	}

	// Interactive
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n%s>%s ", ANSIGreen, ANSIReset)
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		dispatch(input)
	}
}

func showHelp() {
	fmt.Printf("%s🐹 PiGo%s — %spi in Go%s\n\n", ANSIBold, ANSIReset, ANSIGray, ANSIReset)
	fmt.Printf("Usage: pigo [options] [prompt]\n\n")
	fmt.Printf("%sOptions:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  --help, -h       Show this help\n")
	fmt.Printf("  --version, -v    Show version\n")
	fmt.Printf("  --model <name>   Set model for single-shot\n")
	fmt.Printf("  --thinking <lvl> Set thinking level\n")
	fmt.Printf("\n%sInteractive Commands:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  %s/model <name>%s    Switch model (deepseek-v4-flash, deepseek-v4-pro[1m], deepseek-chat)\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/models%s          List available models\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/thinking <lvl>%s   Set thinking: off / low / medium / high / max\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/self%s            Self-iterate & rebuild PiGo\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/repair <desc>%s   Auto-repair a bug\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/mode%s            Show current mode, model, thinking\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/save <name>%s     Save session\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/load <name>%s     Load session\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/help%s            Show this help\n", ANSIYellow, ANSIReset)
	fmt.Printf("  %s/quit%s            Exit\n", ANSIYellow, ANSIReset)
	fmt.Printf("\n%sExamples:%s\n", ANSICyan, ANSIReset)
	fmt.Printf("  pigo                    Interactive mode\n")
	fmt.Printf("  pigo --help             Show help\n")
	fmt.Printf("  pigo \"Fix the bug\"      Single-shot query\n")
	fmt.Printf("  pigo --model deepseek-chat \"Explain this code\"\n")
}

func dispatch(input string) {
	switch {
	case input == "/quit" || input == "/exit":
		fmt.Printf("%s👋%s\n", ANSIYellow, ANSIReset)
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

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n+3:]
}
