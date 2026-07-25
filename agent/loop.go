package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"pigo/config"
	"pigo/llm"
	"pigo/tools"
	"strings"
	"time"
)

// ANSI colors for TUI
const (
	ANSIReset  = "\033[0m"
	ANSIRed    = "\033[31m"
	ANSIGreen  = "\033[32m"
	ANSIYellow = "\033[33m"
	ANSICyan   = "\033[36m"
	ANSIGray   = "\033[90m"
	ANSIBold   = "\033[1m"
)

type Agent struct {
	cfg            *config.Config
	client         *llm.Client
	deepseekClient *llm.DeepSeekClient // native API for CoT/reasoner
	registry       *tools.Registry
	messages       []llm.Message
	mode           Mode
	thinking       ThinkingLevel
}

func New(cfg *config.Config) *Agent {
	client := llm.New(cfg.APIKey, cfg.BaseURL, cfg.Model)
	// Native DeepSeek API for reasoner/CoT support
	dsClient := llm.NewDeepSeekClient(cfg.APIKey, "https://api.deepseek.com")
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadTool{})
	reg.Register(&tools.WriteTool{})
	reg.Register(&tools.EditTool{})
	reg.Register(&tools.BashTool{})

	return &Agent{
		cfg:            cfg,
		client:         client,
		deepseekClient: dsClient,
		registry:       reg,
		messages:       []llm.Message{},
		mode:           ModeNormal,
		thinking:       ThinkingLevel(cfg.ThinkingLevel),
	}
}

func (a *Agent) SetMode(mode Mode)  { a.mode = mode }
func (a *Agent) Mode() Mode         { return a.mode }

func (a *Agent) SetThinking(level ThinkingLevel) {
	if _, ok := map[ThinkingLevel]bool{ThinkOff: true, ThinkLow: true, ThinkMedium: true, ThinkHigh: true, ThinkMax: true}[level]; ok {
		a.thinking = level
	}
}
func (a *Agent) Thinking() ThinkingLevel { return a.thinking }

func (a *Agent) SwitchModel(name string) {
	a.cfg.Model = name
	a.client = llm.New(a.cfg.APIKey, a.cfg.BaseURL, name)
	// Reset usage on model switch
	a.client.TotalUsage = llm.Usage{}
	a.deepseekClient.TotalUsage = llm.Usage{}
}

func (a *Agent) Model() string { return a.cfg.Model }

// TotalUsage returns combined usage from both clients
func (a *Agent) TotalUsage() llm.Usage {
	return llm.Usage{
		InputTokens:      a.client.TotalUsage.InputTokens + a.deepseekClient.TotalUsage.InputTokens,
		OutputTokens:     a.client.TotalUsage.OutputTokens + a.deepseekClient.TotalUsage.OutputTokens,
		CacheHitTokens:   a.client.TotalUsage.CacheHitTokens + a.deepseekClient.TotalUsage.CacheHitTokens,
		CacheMissTokens:  a.client.TotalUsage.CacheMissTokens + a.deepseekClient.TotalUsage.CacheMissTokens,
		CacheWriteTokens: a.client.TotalUsage.CacheWriteTokens + a.deepseekClient.TotalUsage.CacheWriteTokens,
	}
}

// Footer prints the status footer line
func (a *Agent) Footer() {
	usage := a.TotalUsage()
	model := a.Model()
	thinking := a.Thinking()
	ctxWindow := llm.GetContextWindow(model)
	pct := 0.0
	if ctxWindow > 0 {
		pct = float64(usage.InputTokens) / float64(ctxWindow) * 100
	}
	cost := usage.CostUSD(model)

	// Model display name (prettify)
	displayModel := model
	if strings.Contains(model, "[1m]") {
		displayModel = "V4 Pro 1M"
	} else if strings.Contains(model, "flash") {
		displayModel = "V4 Flash"
	} else if strings.Contains(model, "chat") {
		displayModel = "Chat"
	} else if strings.Contains(model, "reasoner") {
		displayModel = "Reasoner"
	}

	// Short dir
	dir := a.cfg.WorkDir
	if len(dir) > 24 {
		dir = "…" + dir[len(dir)-23:]
	}

	fmt.Fprintf(os.Stderr, "\n%s── %sDeepSeek %s%s | %sthink:%s%s | %s%s%s | %s◫ %s%s/%dM %s(%.1f%%)%s",
		ANSIGray,
		ANSIBold, displayModel, ANSIReset,
		ANSIGray, ANSIReset, thinking,
		ANSIGray, ANSIReset, dir,
		ANSIGray, ANSIReset,
		formatTokens(usage.InputTokens),
		ctxWindow/1000,
		ANSIGray, pct, ANSIReset,
	)

	// Cache info
	cacheTotal := usage.CacheHitTokens + usage.CacheWriteTokens
	if cacheTotal > 0 {
		fmt.Fprintf(os.Stderr, " %s|%s cache: %s", ANSIGray, ANSIReset, formatBytes(cacheTotal*4))
	}

	// Cost
	fmt.Fprintf(os.Stderr, " %s|%s %s$%.2f%s",
		ANSIGray, ANSIReset,
		ANSIYellow, cost, ANSIReset,
	)
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

func formatBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%dK", n/1024)
	}
	return fmt.Sprintf("%dM", n/(1024*1024))
}

// isReasonerModel checks if the current model supports native CoT/reasoning
func (a *Agent) isReasonerModel() bool {
	return strings.Contains(a.cfg.Model, "reasoner")
}

// useCoT returns true when CoT chain should be used (reasoner model + thinking on)
func (a *Agent) useCoT() bool {
	return a.isReasonerModel() && a.thinking != ThinkOff
}

// Spinner for thinking indicator
func showSpinner(stop chan bool) {
	chars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	for {
		select {
		case <-stop:
			fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 20))
			return
		default:
			fmt.Fprintf(os.Stderr, "\r%s %sthinking...%s ", chars[i%len(chars)], ANSIGray, ANSIReset)
			i++
			time.Sleep(80 * time.Millisecond)
		}
	}
}

func (a *Agent) Run(prompt string) (string, error) {
	a.messages = append(a.messages, llm.Message{
		Role: "user",
		Content: []llm.TextContent{
			{Type: "text", Text: prompt},
		},
	})

	// ─── CoT Path: reasoner model with thinking enabled ───
	if a.useCoT() {
		return a.runCoT(prompt)
	}

	// ─── Standard Path: tool-capable models ───
	for turn := 0; turn < a.cfg.MaxTurns; turn++ {
		sysPrompt := BuildSystemPrompt(a.mode, a.cfg.SystemPrompt)
		maxTok := thinkingTokens[a.thinking]

		req := &llm.Request{
			Model:     a.cfg.Model,
			MaxTokens: maxTok,
			System:    sysPrompt,
			Messages:  a.messages,
			Tools:     a.buildToolDefs(),
		}

		// Start spinner while waiting for first token
		spinnerStop := make(chan bool)
		go showSpinner(spinnerStop)

		textStreamed := false
		var resp *llm.Response
		var err error

		resp, err = a.client.SendStream(req,
			func(text string) {
				if !textStreamed {
					close(spinnerStop)
					textStreamed = true
				}
				fmt.Print(text)
			},
			func(name, id string) {
				if !textStreamed {
					close(spinnerStop)
					textStreamed = true
				}
				fmt.Fprintf(os.Stderr, "\n%s%s %s%s\n", ANSIGreen, ANSIBold, name, ANSIReset)
			},
		)

		if !textStreamed {
			close(spinnerStop)
		}

		if err != nil {
			return "", fmt.Errorf("API: %w", err)
		}

		// Process response content
		toolUses := []llm.ContentBlock{}
		var textParts []string

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				textParts = append(textParts, block.Text)
			case "tool_use":
				toolUses = append(toolUses, block)
			}
		}

		assistantText := strings.Join(textParts, "")

		// Build assistant message for history
		var assistantContent []interface{}
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				assistantContent = append(assistantContent, llm.TextContent{Type: "text", Text: block.Text})
			case "tool_use":
				assistantContent = append(assistantContent, llm.ToolUseContent{Type: "tool_use", ID: block.ID, Name: block.Name, Input: block.Input})
			}
		}

		a.messages = append(a.messages, llm.Message{Role: "assistant", Content: assistantContent})

		if len(toolUses) == 0 {
			if assistantText != "" {
				fmt.Println()
			}
			a.Footer()
			return assistantText, nil
		}

		// Execute tools
		toolResults := make([]interface{}, len(toolUses))
		for i, tu := range toolUses {
			tool := a.registry.Get(tu.Name)
			if tool == nil {
				toolResults[i] = map[string]interface{}{
					"type": "tool_result", "tool_use_id": tu.ID,
					"content": fmt.Sprintf("Unknown: %s", tu.Name), "is_error": true,
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "\n%s %s", ANSICyan, tu.Name)
			result := tool.Execute(tu.Input)
			resultJSON := result.Output
			if !result.Success {
				resultJSON = result.Output + "\nError: " + result.Error
				fmt.Fprintf(os.Stderr, " %s%s%s", ANSIRed, "✗", ANSIReset)
			} else {
				fmt.Fprintf(os.Stderr, " %s%s%s", ANSIGreen, "✓", ANSIReset)
			}
			fmt.Println()
			toolResults[i] = map[string]interface{}{
				"type": "tool_result", "tool_use_id": tu.ID,
				"content": resultJSON, "is_error": !result.Success,
			}
		}
		a.messages = append(a.messages, llm.Message{Role: "user", Content: toolResults})
	}
	return "", fmt.Errorf("max turns exceeded")
}

// runCoT executes a Chain-of-Thought run using the native DeepSeek API.
// It streams reasoning_content (the "thinking chain") in real-time,
// then displays the final answer.
func (a *Agent) runCoT(prompt string) (string, error) {
	// Build messages for native API
	dsMessages := a.buildDSMessages(prompt)

	req := &llm.DSRequest{
		Model:     a.cfg.Model,
		Messages:  dsMessages,
		MaxTokens: thinkingTokens[a.thinking],
	}

	// Show CoT header
	fmt.Printf("\n%s💭 思考链 · Chain of Thought%s\n", ANSICyan, ANSIReset)
	fmt.Print(strings.Repeat("─", 50) + "\n")

	reasoningStarted := false
	contentStarted := false

	finalContent, err := a.deepseekClient.SendStream(req,
		// onReasoning — called for each reasoning_content chunk
		func(reasoning string) {
			if !reasoningStarted {
				reasoningStarted = true
				fmt.Fprintf(os.Stderr, "%s", ANSIGray)
			}
			fmt.Fprint(os.Stderr, reasoning)
		},
		// onContent — called for each normal content chunk
		func(content string) {
			if reasoningStarted && !contentStarted {
				contentStarted = true
				fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
				fmt.Print(strings.Repeat("─", 50) + "\n")
				fmt.Printf("%s💡 回答 · Answer%s\n", ANSIGreen, ANSIReset)
			}
			fmt.Print(content)
		},
	)

	if err != nil {
		return "", fmt.Errorf("CoT API: %w", err)
	}

	// If no content was streamed (e.g. pure reasoning without final answer),
	// still close the reasoning section cleanly
	if !contentStarted && reasoningStarted {
		fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
		fmt.Print(strings.Repeat("─", 50) + "\n")
	}
	fmt.Println()

	a.Footer()
	return finalContent, nil
}

// buildDSMessages converts the agent's internal message history to
// DeepSeek native format (flat role+content, no content blocks).
func (a *Agent) buildDSMessages(currentPrompt string) []llm.DSMessage {
	var out []llm.DSMessage

	// System prompt
	sysPrompt := BuildSystemPrompt(a.mode, a.cfg.SystemPrompt)
	if sysPrompt != "" {
		out = append(out, llm.DSMessage{
			Role:    "system",
			Content: sysPrompt,
		})
	}

	// Convert conversation history (skip the last user message we just appended)
	for i, msg := range a.messages {
		// Skip the last user message — we'll add it as the final prompt
		if i == len(a.messages)-1 && msg.Role == "user" {
			continue
		}

		switch msg.Role {
		case "user":
			text := extractTextContent(msg.Content)
			out = append(out, llm.DSMessage{Role: "user", Content: text})
		case "assistant":
			text := extractTextContent(msg.Content)
			out = append(out, llm.DSMessage{Role: "assistant", Content: text})
		}
	}

	// Add current prompt
	out = append(out, llm.DSMessage{Role: "user", Content: currentPrompt})

	return out
}

// extractTextContent extracts plain text from mixed content blocks
func extractTextContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, item := range c {
			switch v := item.(type) {
			case llm.TextContent:
				parts = append(parts, v.Text)
			case map[string]interface{}:
				if t, ok := v["type"].(string); ok && t == "text" {
					if txt, ok := v["text"].(string); ok {
						parts = append(parts, txt)
					}
				}
			}
		}
		return strings.Join(parts, "")
	case []llm.TextContent:
		var parts []string
		for _, v := range c {
			parts = append(parts, v.Text)
		}
		return strings.Join(parts, "")
	}
	return fmt.Sprintf("%v", content)
}

func (a *Agent) buildToolDefs() []llm.Tool {
	var defs []llm.Tool
	for _, t := range a.registry.List() {
		defs = append(defs, llm.Tool{
			Name: t.Name(), Description: t.Description(), InputSchema: t.Schema(),
		})
	}
	return defs
}

func (a *Agent) SelfIterate() error {
	a.SetMode(ModeSelfIterate)
	fmt.Println("🔁 Self-Iteration Mode — improving PiGo...")

	var srcFiles []string
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") {
			srcFiles = append(srcFiles, path)
		}
		return nil
	})

	prompt := fmt.Sprintf(`Read all Go source files and improve PiGo:
%s

After making changes, run: go build -o pigo .
If it fails, fix errors. Summarize changes.`, strings.Join(srcFiles, "\n"))

	return a.RunCommand(prompt)
}

func (a *Agent) AutoRepair(bugDesc string) error {
	a.SetMode(ModeAutoRepair)
	fmt.Println("🔧 Auto-Repair Mode — fixing:", bugDesc)
	return a.RunCommand("Bug report: " + bugDesc + "\n\nFix it and rebuild.")
}

func (a *Agent) Rebuild() error {
	fmt.Println("\n🔨 Rebuilding...")
	cmd := exec.Command("go", "build", "-o", "pigo", ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *Agent) RunCommand(prompt string) error {
	_, err := a.Run(prompt)
	return err
}

type Turn struct {
	UserMessage string
	Assistant   string
}

// thinking tokens
var thinkingTokens = map[ThinkingLevel]int{
	ThinkOff:    2048,
	ThinkLow:    4096,
	ThinkMedium: 8192,
	ThinkHigh:   16384,
	ThinkMax:    32768,
}
