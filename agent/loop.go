package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"pigo/config"
	"pigo/llm"
	"pigo/session"
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
	session        *session.Session
	sessionMan     *session.Manager
	noSession      bool // ephemeral mode — don't save
	messageIDs     []string // parallel IDs for messages ↔ session entries
}

func New(cfg *config.Config) *Agent {
	client := llm.New(cfg.APIKey, cfg.BaseURL, cfg.Model)
	dsClient := llm.NewDeepSeekClient(cfg.APIKey, "https://api.deepseek.com")
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadTool{})
	reg.Register(&tools.WriteTool{})
	reg.Register(&tools.EditTool{})
	reg.Register(&tools.BashTool{})

	// Session manager rooted at ~/.pigo/sessions
	home, _ := os.UserHomeDir()
	sessDir := filepath.Join(home, ".pigo", "sessions")
	if d := os.Getenv("PIGO_SESSION_DIR"); d != "" {
		sessDir = d
	}
	sm := session.NewManager(sessDir)

	return &Agent{
		cfg:            cfg,
		client:         client,
		deepseekClient: dsClient,
		registry:       reg,
		messages:       []llm.Message{},
		mode:           ModeNormal,
		thinking:       ThinkingLevel(cfg.ThinkingLevel),
		sessionMan:     sm,
		noSession:      cfg.NoSession,
		messageIDs:     []string{},
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
	a.client.TotalUsage = llm.Usage{}
	a.deepseekClient.TotalUsage = llm.Usage{}
}

func (a *Agent) Model() string { return a.cfg.Model }

// ─── Session Management ──────────────────────────────────────────

// Session returns the current session (nil if ephemeral).
func (a *Agent) Session() *session.Session { return a.session }

// IsEphemeral returns true if running without session persistence.
func (a *Agent) IsEphemeral() bool { return a.noSession }

// SessionManager returns the session manager.
func (a *Agent) SessionManager() *session.Manager { return a.sessionMan }

// InitSession creates a new session for this agent.
func (a *Agent) InitSession(name string) error {
	if a.noSession {
		return nil
	}
	s, err := a.sessionMan.Create(a.cfg.WorkDir, name)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	a.session = s
	return nil
}

// ResumeSession resumes from an existing session by loading its entries into history.
func (a *Agent) ResumeSession(s *session.Session) error {
	a.session = s
	a.messages = nil
	a.messageIDs = nil

	for _, entry := range s.Entries {
		a.messageIDs = append(a.messageIDs, entry.ID)
		switch entry.Role {
		case "user":
			a.messages = append(a.messages, llm.Message{
				Role: "user",
				Content: []llm.TextContent{
					{Type: "text", Text: entry.Content},
				},
			})
		case "assistant":
			// Try to parse structured content; fallback to plain text
			a.messages = append(a.messages, llm.Message{
				Role: "assistant",
				Content: []llm.TextContent{
					{Type: "text", Text: entry.Content},
				},
			})
		case "tool":
			// Tool results are stored as user messages in Anthropic format
			if entry.ToolUseID != "" {
				// Proper tool_result with tool_use_id
				a.messages = append(a.messages, llm.Message{
					Role: "user",
					Content: []interface{}{
						map[string]interface{}{
							"type":        "tool_result",
							"tool_use_id": entry.ToolUseID,
							"content":     entry.Content,
						},
					},
				})
			} else {
				// Legacy format — no tool_use_id, convert to text message
				a.messages = append(a.messages, llm.Message{
					Role: "user",
					Content: []llm.TextContent{
						{Type: "text", Text: entry.Content},
					},
				})
			}
		}
	}
	return nil
}

// saveEntry persists a message to the session JSONL.
func (a *Agent) saveEntry(role, content string, toolUseID string) {
	if a.noSession || a.session == nil {
		return
	}
	parentID := ""
	if len(a.messageIDs) > 0 {
		parentID = a.messageIDs[len(a.messageIDs)-1]
	}
	if err := a.session.AddEntry(parentID, role, content, toolUseID); err != nil {
		fmt.Fprintf(os.Stderr, "%s⚠ session write: %v%s\n", ANSIYellow, err, ANSIReset)
		return
	}
	a.messageIDs = append(a.messageIDs, a.session.LastID())
	// Keep meta in sync (name might have been set)
	a.session.WriteMeta()
}

// ─── Usage & Footer ─────────────────────────────────────────────

func (a *Agent) TotalUsage() llm.Usage {
	return llm.Usage{
		InputTokens:      a.client.TotalUsage.InputTokens + a.deepseekClient.TotalUsage.InputTokens,
		OutputTokens:     a.client.TotalUsage.OutputTokens + a.deepseekClient.TotalUsage.OutputTokens,
		CacheHitTokens:   a.client.TotalUsage.CacheHitTokens + a.deepseekClient.TotalUsage.CacheHitTokens,
		CacheMissTokens:  a.client.TotalUsage.CacheMissTokens + a.deepseekClient.TotalUsage.CacheMissTokens,
		CacheWriteTokens: a.client.TotalUsage.CacheWriteTokens + a.deepseekClient.TotalUsage.CacheWriteTokens,
	}
}

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

	displayModel := model
	if strings.Contains(model, "v4-pro") {
		displayModel = "V4 Pro 1M"
	} else if strings.Contains(model, "flash") {
		displayModel = "V4 Flash"
	} else if strings.Contains(model, "chat") {
		displayModel = "Chat"
	} else if strings.Contains(model, "reasoner") {
		displayModel = "Reasoner"
	}

	dir := a.cfg.WorkDir
	if len(dir) > 24 {
		dir = "…" + dir[len(dir)-23:]
	}

	// Session indicator
	sessIndicator := ""
	if !a.noSession && a.session != nil {
		sessID := a.session.ID
		if len(sessID) > 8 {
			sessID = sessID[:8]
		}
		sessIndicator = fmt.Sprintf(" · %ssession %s%s", ANSIGray, sessID, ANSIReset)
	}

	fmt.Fprint(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "%s%s%s\n", ANSIGray, strings.Repeat("─", 60), ANSIReset)

	fmt.Fprintf(os.Stderr, "%sDeepSeek %s%s | %sthink:%s%s | %s%s%s%s | %s◫ %s%s/%s %s(%.1f%%) AC%s",
		ANSIBold, displayModel, ANSIReset,
		ANSIGray, ANSIReset, thinking,
		ANSIGray, ANSIReset, dir,
		sessIndicator,
		ANSIGray, ANSIReset,
		formatTokens(usage.InputTokens),
		formatTokens(ctxWindow),
		ANSIGray, pct, ANSIReset,
	)

	cacheTotal := usage.CacheHitTokens + usage.CacheWriteTokens
	if cacheTotal > 0 {
		fmt.Fprintf(os.Stderr, " %s|%s cache in: %s", ANSIGray, ANSIReset, formatBytes(cacheTotal*4))
	}

	fmt.Fprintf(os.Stderr, " %s|%s %s$%.2f%s\n",
		ANSIGray, ANSIReset,
		ANSIYellow, cost, ANSIReset,
	)
}

// ─── Core Run Loop ──────────────────────────────────────────────

func (a *Agent) Run(prompt string) (string, error) {
	// Init session on first message
	if !a.noSession && a.session == nil {
		a.InitSession("")
	}

	// Save user message
	a.saveEntry("user", prompt, "")

	a.messages = append(a.messages, llm.Message{
		Role: "user",
		Content: []llm.TextContent{
			{Type: "text", Text: prompt},
		},
	})

	if a.useCoT() {
		return a.runCoT(prompt)
	}

	return a.runStandardLoop()
}

// runStandardLoop runs the tool-calling agent loop (non-CoT path).
func (a *Agent) runStandardLoop() (string, error) {
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

		spinnerStop := make(chan bool)
		spinnerDone := make(chan bool)
		go showSpinner(spinnerStop, spinnerDone)

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
		<-spinnerDone

		if err != nil {
			return "", fmt.Errorf("API: %w", err)
		}

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

		// Build assistant message and save
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
		a.saveEntry("assistant", assistantText, "")

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
				a.saveEntry("tool", fmt.Sprintf("Unknown tool: %s", tu.Name), tu.ID)
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
			a.saveEntry("tool", resultJSON, tu.ID)
		}
		a.messages = append(a.messages, llm.Message{Role: "user", Content: toolResults})
	}
	return "", fmt.Errorf("max turns exceeded")
}

// RunFromSession continues a loaded session (resume).
func (a *Agent) RunFromSession(prompt string) (string, error) {
	return a.Run(prompt)
}

// ─── CoT Path ───────────────────────────────────────────────────

func (a *Agent) runCoT(prompt string) (string, error) {
	dsMessages := a.buildDSMessages(prompt)

	req := &llm.DSRequest{
		Model:     a.cfg.Model,
		Messages:  dsMessages,
		MaxTokens: thinkingTokens[a.thinking],
	}

	fmt.Printf("\n%s💭 思考链 · Chain of Thought%s\n", ANSICyan, ANSIReset)
	fmt.Print(strings.Repeat("─", 50) + "\n")

	reasoningStarted := false
	contentStarted := false

	finalContent, err := a.deepseekClient.SendStream(req,
		func(reasoning string) {
			if !reasoningStarted {
				reasoningStarted = true
				fmt.Fprintf(os.Stderr, "%s", ANSIGray)
			}
			fmt.Fprint(os.Stderr, reasoning)
		},
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

	if !contentStarted && reasoningStarted {
		fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
		fmt.Print(strings.Repeat("─", 50) + "\n")
	}
	fmt.Println()

	a.Footer()
	return finalContent, nil
}

// ─── Session-aware save ────────────────────────────────────────

// SaveSession explicitly saves the current session (useful for /save command).
// If no session exists yet, creates one.
func (a *Agent) SaveSession(name string) error {
	if a.noSession {
		return fmt.Errorf("ephemeral mode — sessions disabled")
	}
	if a.session == nil {
		if err := a.InitSession(name); err != nil {
			return err
		}
		return nil // InitSession already writes meta & flush
	}
	if name != "" {
		a.session.Name = name
	}
	if err := a.session.Flush(); err != nil {
		return err
	}
	return a.session.WriteMeta()
}

// ─── Helpers ───────────────────────────────────────────────────

func (a *Agent) buildDSMessages(currentPrompt string) []llm.DSMessage {
	var out []llm.DSMessage

	sysPrompt := BuildSystemPrompt(a.mode, a.cfg.SystemPrompt)
	if sysPrompt != "" {
		out = append(out, llm.DSMessage{
			Role:    "system",
			Content: sysPrompt,
		})
	}

	for i, msg := range a.messages {
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

	out = append(out, llm.DSMessage{Role: "user", Content: currentPrompt})
	return out
}

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

func (a *Agent) isReasonerModel() bool {
	return strings.Contains(a.cfg.Model, "reasoner")
}

func (a *Agent) useCoT() bool {
	return a.isReasonerModel() && a.thinking != ThinkOff
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

func showSpinner(stop chan bool, done chan bool) {
	chars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	for {
		select {
		case <-stop:
			fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 20))
			done <- true
			return
		default:
			fmt.Fprintf(os.Stderr, "\r%s %sthinking...%s ", chars[i%len(chars)], ANSIGray, ANSIReset)
			i++
			time.Sleep(80 * time.Millisecond)
		}
	}
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	if n < 1000000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	if n < 10_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	return fmt.Sprintf("%dM", n/1_000_000)
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

var thinkingTokens = map[ThinkingLevel]int{
	ThinkOff:    2048,
	ThinkLow:    4096,
	ThinkMedium: 8192,
	ThinkHigh:   16384,
	ThinkMax:    32768,
}
