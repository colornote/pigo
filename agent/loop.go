package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"pigo/config"
	"pigo/llm"
	"pigo/session"
	"pigo/tools"
	"strings"

	"golang.org/x/term"
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
	noSession      bool     // ephemeral mode — don't save
	autoRepair     bool     // auto-trigger repair on error
	gitContext     string   // cached git project context
	messageIDs     []string // parallel IDs for messages ↔ session entries
}

func New(cfg *config.Config) *Agent {
	client := llm.New(cfg.APIKey, cfg.BaseURL, cfg.Model)
	dsClient := llm.NewDeepSeekClient(cfg.APIKey, cfg.DSBaseURL)
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadTool{})
	reg.Register(&tools.WriteTool{})
	reg.Register(&tools.EditTool{})
	reg.Register(&tools.BashTool{})
	reg.Register(&tools.LsTool{})
	reg.Register(&tools.GrepTool{})
	reg.Register(&tools.FindTool{})

	// Session manager rooted at ~/.pigo/sessions, overridable via
	// PIGO_SESSION_DIR env var or the --session-dir CLI flag.
	home, _ := os.UserHomeDir()
	sessDir := filepath.Join(home, ".pigo", "sessions")
	if d := os.Getenv("PIGO_SESSION_DIR"); d != "" {
		sessDir = d
	}
	if cfg.SessionDir != "" {
		sessDir = cfg.SessionDir
	}
	sm := session.NewManager(sessDir)

	a := &Agent{
		cfg:            cfg,
		client:         client,
		deepseekClient: dsClient,
		registry:       reg,
		messages:       []llm.Message{},
		mode:           ModeNormal,
		thinking:       ThinkMedium,
		sessionMan:     sm,
		noSession:      cfg.NoSession,
		autoRepair:     cfg.AutoRepair,
		messageIDs:     []string{},
	}
	a.SetThinking(ThinkingLevel(cfg.ThinkingLevel))
	a.refreshGitContext()
	return a
}

func (a *Agent) SetMode(mode Mode)       { a.mode = mode }
func (a *Agent) Mode() Mode              { return a.mode }
func (a *Agent) SetAutoRepair(on bool)   { a.autoRepair = on }
func (a *Agent) AutoRepairEnabled() bool { return a.autoRepair }

func (a *Agent) SetThinking(level ThinkingLevel) {
	if validThinking[level] {
		a.thinking = level
	}
}
func (a *Agent) Thinking() ThinkingLevel { return a.thinking }

func (a *Agent) SwitchModel(name string) {
	normalized := NormalizeModel(name)
	a.cfg.Model = normalized
	a.client = llm.New(a.cfg.APIKey, a.cfg.BaseURL, normalized)
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
// An empty name falls back to the --name/-n CLI value so `pigo --name "task"`
// names the first session it creates.
func (a *Agent) InitSession(name string) error {
	if a.noSession {
		return nil
	}
	if name == "" {
		name = a.cfg.SessionName
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

	// Batch consecutive tool results into a single user message.
	// Anthropic requires all tool_results for one assistant turn
	// to be in the same user message.
	var pendingToolResults []interface{}

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		a.messages = append(a.messages, llm.Message{
			Role:    "user",
			Content: pendingToolResults,
		})
		pendingToolResults = nil
	}

	for _, entry := range s.Entries {
		a.messageIDs = append(a.messageIDs, entry.ID)
		switch entry.Role {
		case "user":
			flushToolResults()
			a.messages = append(a.messages, llm.Message{
				Role: "user",
				Content: []llm.TextContent{
					{Type: "text", Text: entry.Content},
				},
			})
		case "assistant":
			flushToolResults()
			// Try to parse structured content (JSON array of text/tool_use blocks);
			// fallback to plain text for legacy entries.
			var structured []interface{}
			if err := json.Unmarshal([]byte(entry.Content), &structured); err == nil {
				a.messages = append(a.messages, llm.Message{
					Role:    "assistant",
					Content: structured,
				})
			} else {
				a.messages = append(a.messages, llm.Message{
					Role: "assistant",
					Content: []llm.TextContent{
						{Type: "text", Text: entry.Content},
					},
				})
			}
		case "compaction":
			flushToolResults()
			// A compaction summary of earlier conversation: re-inject it as
			// a user message so the model retains context of what happened.
			a.messages = append(a.messages, llm.Message{
				Role: "user",
				Content: []llm.TextContent{
					{Type: "text", Text: "[Compacted summary of earlier conversation]\n" + entry.Content},
				},
			})
		case "tool":
			// Batch tool results: they'll be flushed together as one user message
			// when the next non-tool entry arrives.
			if entry.ToolUseID != "" {
				pendingToolResults = append(pendingToolResults, map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": entry.ToolUseID,
					"content":     entry.Content,
				})
			} else {
				// Legacy format — no tool_use_id, convert to text
				pendingToolResults = append(pendingToolResults, llm.TextContent{
					Type: "text", Text: entry.Content,
				})
			}
		}
	}
	// Flush any remaining tool results at end
	flushToolResults()

	// Clean orphan tool_uses anywhere in the message history.
	// This handles sessions that were saved mid-turn before tools ran,
	// or sessions damaged by previous bugs.
	a.cleanOrphanToolUses()

	return nil
}

// cleanOrphanToolUses scans all messages and strips tool_use blocks
// that don't have matching tool_result blocks in the next message.
func (a *Agent) cleanOrphanToolUses() {
	i := 0
	for i < len(a.messages) {
		msg := a.messages[i]
		if msg.Role != "assistant" {
			i++
			continue
		}

		toolUseIDs := collectToolUseIDs(msg.Content)
		if len(toolUseIDs) == 0 {
			i++
			continue
		}

		// Last message - don't strip, it's the current turn
		if i == len(a.messages)-1 {
			return
		}

		// Check if the next message has matching tool_results
		hasResults := i+1 < len(a.messages) && a.messages[i+1].Role == "user"
		if hasResults {
			toolResultIDs := collectToolResultIDs(a.messages[i+1].Content)
			for _, id := range toolUseIDs {
				if !toolResultIDs[id] {
					hasResults = false
					break
				}
			}
		}

		if hasResults {
			i += 2
			continue
		}

		// Orphan: strip tool_uses
		orphans := make(map[string]bool)
		for _, id := range toolUseIDs {
			orphans[id] = true
		}
		cleaned := stripToolUses(msg.Content, orphans)
		if cleaned == nil {
			// Remove the entire assistant message
			a.messages = append(a.messages[:i], a.messages[i+1:]...)
			// Also remove the paired user message (tool_results) if present
			if i < len(a.messages) && a.messages[i].Role == "user" && len(collectToolResultIDs(a.messages[i].Content)) > 0 {
				a.messages = append(a.messages[:i], a.messages[i+1:]...)
			}
			// Don't advance i — the next message shifted into position i
		} else {
			a.messages[i].Content = cleaned
			i++
		}
	}
}

func collectToolUseIDs(content interface{}) []string {
	list, _ := content.([]interface{})
	var ids []string
	for _, block := range list {
		switch b := block.(type) {
		case llm.ToolUseContent:
			ids = append(ids, b.ID)
		case map[string]interface{}:
			if t, _ := b["type"].(string); t == "tool_use" {
				if id, _ := b["id"].(string); id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func collectToolResultIDs(content interface{}) map[string]bool {
	result := make(map[string]bool)
	list, _ := content.([]interface{})
	for _, block := range list {
		switch b := block.(type) {
		case map[string]interface{}:
			if t, _ := b["type"].(string); t == "tool_result" {
				if id, _ := b["tool_use_id"].(string); id != "" {
					result[id] = true
				}
			}
		}
	}
	return result
}

func stripToolUses(content interface{}, orphans map[string]bool) interface{} {
	list, _ := content.([]interface{})
	var cleaned []interface{}
	for _, block := range list {
		switch b := block.(type) {
		case llm.ToolUseContent:
			if orphans[b.ID] {
				continue
			}
			cleaned = append(cleaned, b)
		case map[string]interface{}:
			if t, _ := b["type"].(string); t == "tool_use" {
				if id, _ := b["id"].(string); orphans[id] {
					continue
				}
				cleaned = append(cleaned, b)
			} else {
				cleaned = append(cleaned, b)
			}
		default:
			cleaned = append(cleaned, b)
		}
	}
	if len(cleaned) == 0 {
		return nil // remove entire message
	}
	return cleaned
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
	if len([]rune(dir)) > 24 {
		dir = "…" + TruncRunesRight(dir, 23)
	}

	// Session indicator
	sessIndicator := ""
	if !a.noSession && a.session != nil {
		sessID := a.session.ID
		if len(sessID) > 8 {
			sessID = sessID[:8]
		}
		sessIndicator = " · session " + sessID
	}
	// Build footer as colored segments for CJK-aware width truncation.
	segs := []colorSeg{
		{fmt.Sprintf("DeepSeek %s", displayModel), ANSIBold},
		{" | ", ANSIGray},
		{fmt.Sprintf("think:%s", thinking), ANSIGray},
		{" | ", ANSIGray},
		{dir, ANSIGray},
		{sessIndicator, ANSIGray},
		{" | ", ANSIGray},
		{fmt.Sprintf("◫ %s/%s (%.1f%%) AC", formatTokens(usage.InputTokens), formatTokens(ctxWindow), pct), ANSIGray},
	}
	cacheTotal := usage.CacheHitTokens + usage.CacheWriteTokens
	if cacheTotal > 0 {
		segs = append(segs, colorSeg{fmt.Sprintf(" | cache in: %s", formatBytes(cacheTotal*4)), ANSIGray})
	}
	segs = append(segs, colorSeg{fmt.Sprintf(" | $%.2f", cost), ANSIYellow})

	// Always print inline — avoids cursor-positioning issues
	// across different terminals (some don't support \033[s/\033[u)
	termWidth, _ := getTerminalSize()
	w := termWidth
	if w < 1 {
		w = 60
	}
	fmt.Fprint(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "%s%s%s\n%s\n", ANSIGray, strings.Repeat("─", w), ANSIReset, renderSegs(segs, termWidth))

}

// ─── Core Run Loop ──────────────────────────────────────────────

func (a *Agent) Run(ctx context.Context, prompt string) (string, error) {
	// Clean up any orphan tool_uses from previous interrupted turns.
	// This handles the case where the user types a message while
	// the agent's last response had pending tool calls.
	a.cleanOrphanToolUses()

	// Auto-compact when the conversation approaches the context limit.
	if a.shouldAutoCompact() {
		fmt.Fprintf(os.Stderr, "%s🧹 Context at capacity — compacting...%s\n", ANSIYellow, ANSIReset)
		if err := a.Compact(ctx, ""); err != nil {
			fmt.Fprintf(os.Stderr, "%s⚠ compact failed: %v%s\n", ANSIYellow, err, ANSIReset)
		} else {
			fmt.Fprintf(os.Stderr, "%s✓ Context compacted — earlier messages summarized%s\n", ANSIGreen, ANSIReset)
		}
	}

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
		return a.runCoT(ctx, prompt)
	}

	return a.runStandardLoop(ctx)
}

// runStandardLoop runs the tool-calling agent loop (non-CoT path).
// Uses streaming API for real-time text display.
func (a *Agent) runStandardLoop(ctx context.Context) (string, error) {
	for turn := 0; turn < a.cfg.MaxTurns; turn++ {
		// Check for cancellation before each turn
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		sysPrompt := BuildSystemPromptWithDir(a.mode, a.cfg.SystemPrompt, a.cfg.WorkDir)
		if a.gitContext != "" {
			sysPrompt += "\n\n" + a.gitContext
		}
		maxTok := thinkingTokens[a.thinking]

		req := &llm.Request{
			Model:     a.cfg.Model,
			MaxTokens: maxTok,
			System:    sysPrompt,
			Messages:  a.messages,
			Tools:     a.buildToolDefs(),
		}

		// Track streaming state
		var (
			textBuf    strings.Builder
			toolUses   []llm.ContentBlock
			firstToken bool
			inThinking bool
		)

		onThinking := func(thinking string) {
			if !inThinking {
				inThinking = true
				// Clear "thinking..." indicator and show header
				fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 30))
				fmt.Fprintf(os.Stderr, "%s💭 思考中 · Thinking%s\n%s%s\n",
					ANSICyan, ANSIReset,
					ANSIGray, strings.Repeat("─", 50))
			}
			fmt.Fprint(os.Stderr, thinking)
		}

		onText := func(text string) {
			if !firstToken {
				firstToken = true
				if inThinking {
					// Close thinking section
					fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
					fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 50))
					fmt.Fprintf(os.Stderr, "%s💡 回答 · Answer%s\n", ANSIGreen, ANSIReset)
				} else {
					// Clear the "thinking..." line
					fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 30))
				}
			}
			fmt.Print(text)
			textBuf.WriteString(text)
		}

		onTool := func(name, id string) {
			if !firstToken {
				firstToken = true
				if inThinking {
					fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
					fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 50))
				} else {
					fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 30))
				}
			}
		}

		// Show "thinking..." indicator on stderr while waiting
		fmt.Fprintf(os.Stderr, "%s⏳ thinking...%s", ANSIGray, ANSIReset)

		resp, err := a.client.SendStreamWithContext(ctx, req, onText, onTool, onThinking)

		// Clear thinking indicator if still showing
		if !firstToken {
			if inThinking {
				// Thinking was displayed but no text/tool followed — close thinking section
				fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
				fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 50))
			} else {
				fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 30))
			}
		}

		if err != nil {
			return "", fmt.Errorf("API: %w", err)
		}

		// Collect text and tool_use blocks from the response
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

		// Ensure clean separation after streamed text
		if textBuf.Len() > 0 {
			fmt.Println()
		}

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

		// Persist full structured content (text + tool_use blocks) as JSON
		// so tool_use_id references survive session resume.
		assistantJSON, _ := json.Marshal(assistantContent)
		a.saveEntry("assistant", string(assistantJSON), "")

		if len(toolUses) == 0 {
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
					"content": fmt.Sprintf("Unknown: %s", tu.Name),
				}
				a.saveEntry("tool", fmt.Sprintf("Unknown tool: %s", tu.Name), tu.ID)
				continue
			}
			// Tool header line
			fmt.Fprintf(os.Stderr, "\n%s %s", ANSICyan, tu.Name)

			// Bash: preview the command being run
			if tu.Name == "bash" {
				cmd, _ := tu.Input["command"].(string)
				fmt.Fprintf(os.Stderr, " %s$ %s%s", ANSIGray, truncDisplay(cmd, 120), ANSIReset)
			}

			// Edit: preview the change (before → after snippet)
			if tu.Name == "edit" {
				oldText, _ := tu.Input["oldText"].(string)
				newText, _ := tu.Input["newText"].(string)
				displayEditPreview(oldText, newText)
			}

			// Read/Write/Grep/Find/Ls: show a short arg preview
			if argPreview := toolArgPreview(tu); argPreview != "" {
				fmt.Fprintf(os.Stderr, " %s%s%s", ANSIGray, argPreview, ANSIReset)
			}

			// Let ESC/Ctrl+C cancel propagate into the bash command so an
			// interrupt kills long-running commands immediately (not just at
			// the next turn boundary).
			if tu.Name == "bash" {
				if bt, ok := tool.(*tools.BashTool); ok {
					bt.Ctx = ctx
				}
			}

			result := tool.Execute(tu.Input)
			resultJSON := result.Output

			if !result.Success {
				resultJSON = result.Output + "\nError: " + result.Error
				fmt.Fprintf(os.Stderr, " %s✗%s", ANSIRed, ANSIReset)
			} else {
				fmt.Fprintf(os.Stderr, " %s✓%s", ANSIGreen, ANSIReset)
			}

			// Bash: show command output inline with gutter
			if tu.Name == "bash" && result.Output != "" {
				fmt.Fprint(os.Stderr, "\n")
				displayBashOutput(result.Output)
			}

			fmt.Println()

			toolResults[i] = map[string]interface{}{
				"type": "tool_result", "tool_use_id": tu.ID,
				"content": resultJSON,
			}
			a.saveEntry("tool", resultJSON, tu.ID)
		}

		// Append tool results to messages
		a.messages = append(a.messages, llm.Message{
			Role:    "user",
			Content: toolResults,
		})
	}
	return "", fmt.Errorf("max turns exceeded")
}

// ─── CoT Path ───────────────────────────────────────────────────

func (a *Agent) runCoT(ctx context.Context, prompt string) (string, error) {
	dsMessages := a.buildDSMessages(prompt)

	req := &llm.DSRequest{
		Model:     a.cfg.Model,
		Messages:  dsMessages,
		MaxTokens: thinkingTokens[a.thinking],
	}

	// All CoT UI output goes to stderr so reasoning and content
	// are interleaved correctly (stdout/stderr otherwise race).
	sep := strings.Repeat("─", 50)
	fmt.Fprintf(os.Stderr, "\n%s💭 思考链 · Chain of Thought%s\n", ANSICyan, ANSIReset)
	fmt.Fprintf(os.Stderr, "%s\n", sep)

	reasoningStarted := false
	contentStarted := false

	finalContent, err := a.deepseekClient.SendStreamWithContext(ctx, req,
		func(reasoning string) {
			if !reasoningStarted {
				reasoningStarted = true
				fmt.Fprint(os.Stderr, ANSIGray)
			}
			fmt.Fprint(os.Stderr, reasoning)
		},
		func(content string) {
			if !contentStarted {
				contentStarted = true
				if reasoningStarted {
					fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
				}
				fmt.Fprintf(os.Stderr, "%s\n", sep)
				fmt.Fprintf(os.Stderr, "%s💡 回答 · Answer%s\n", ANSIGreen, ANSIReset)
			}
			fmt.Fprint(os.Stderr, content)
		},
	)

	if err != nil {
		return "", fmt.Errorf("CoT API: %w", err)
	}

	// Persist the completed turn so multi-turn CoT conversations retain
	// the assistant's answer (otherwise only the user prompts accumulate
	// and buildDSMessages sends history without replies).
	a.messages = append(a.messages, llm.Message{
		Role:    "assistant",
		Content: []llm.TextContent{{Type: "text", Text: finalContent}},
	})
	a.saveEntry("assistant", finalContent, "")

	// Close out any open formatting
	if reasoningStarted && !contentStarted {
		fmt.Fprintf(os.Stderr, "%s\n", ANSIReset)
		fmt.Fprintf(os.Stderr, "%s\n", sep)
	}
	fmt.Fprintln(os.Stderr)

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

// ─── Compaction ────────────────────────────────────────────────

// Compact summarizes older messages into a single summary message while
// keeping a recent tail verbatim, freeing context window space. The summary
// is recorded as a "compaction" session entry so it survives resume.
func (a *Agent) Compact(ctx context.Context, customInstr string) error {
	const keep = 8 // recent messages retained verbatim

	if len(a.messages) <= keep+2 {
		return fmt.Errorf("conversation too short to compact (need more than %d messages, have %d)", keep+2, len(a.messages))
	}

	cut := len(a.messages) - keep

	// Advance the cut point so the retained tail never starts with orphaned
	// tool_result blocks (their tool_use was summarized away).
	for cut < len(a.messages) && isToolResultMessage(a.messages[cut]) {
		cut++
	}
	if cut == len(a.messages) {
		return fmt.Errorf("cannot compact: no safe cut point")
	}

	old := a.messages[:cut]
	tail := a.messages[cut:]
	transcript := a.renderTranscript(old)

	instr := customInstr
	if instr == "" {
		instr = "Preserve key decisions, file paths, commands, and unresolved issues."
	}

	req := &llm.Request{
		Model:     a.cfg.Model,
		MaxTokens: 2048,
		System:    "You are a meticulous conversation summarizer for a coding agent.",
		Messages: []llm.Message{
			{
				Role: "user",
				Content: []llm.TextContent{{
					Type: "text",
					Text: fmt.Sprintf(
						"Summarize the following coding-agent conversation.\n\n%s\n\n"+
							"Write a concise summary that preserves:\n"+
							"- The overall task and goal\n"+
							"- Files read, created, and modified\n"+
							"- Commands run and their outcomes\n"+
							"- Key decisions and rationale\n"+
							"- Current state and what remains to be done\n\n"+
							"%s\n\nSummary:",
						transcript, instr),
				}},
			},
		},
	}

	resp, err := a.client.SendWithContext(ctx, req)
	if err != nil {
		return fmt.Errorf("summarize: %w", err)
	}

	summary := ""
	for _, block := range resp.Content {
		if block.Type == "text" {
			summary += block.Text
		}
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("summarize: empty summary returned")
	}

	// Rebuild the message list: summary message + retained tail.
	newMessages := []llm.Message{
		{
			Role: "user",
			Content: []llm.TextContent{{
				Type: "text",
				Text: "[Compacted summary of earlier conversation]\n" + summary,
			}},
		},
	}
	newMessages = append(newMessages, tail...)
	a.messages = newMessages

	// Record the compaction in the session so it survives resume.
	a.saveEntry("compaction", summary, "")

	return nil
}

// shouldAutoCompact reports whether the conversation should be summarized
// before the next turn because it is approaching the context window limit.
func (a *Agent) shouldAutoCompact() bool {
	if len(a.messages) < 14 {
		return false
	}
	window := llm.GetContextWindow(a.cfg.Model)
	if window <= 0 {
		return false
	}
	return a.estimateTokens() > int(float64(window)*0.85)
}

// estimateTokens roughly estimates the token count of the message history
// (chars/4 is a common approximation). Used only for compaction decisions.
// Tool-result content is counted too — it is the largest token consumer in
// tool-calling sessions and ignoring it delays compaction past the window.
func (a *Agent) estimateTokens() int {
	total := 0
	for _, m := range a.messages {
		switch m.Role {
		case "user":
			if results := extractToolResults(m.Content); len(results) > 0 {
				for _, r := range results {
					total += len(r)
				}
			} else {
				total += len(extractTextContent(m.Content))
			}
		default:
			total += len(extractTextContent(m.Content))
		}
	}
	return total / 4
}

// renderTranscript renders messages as a compact text transcript for
// summarization. Tool results are truncated to keep the prompt small.
func (a *Agent) renderTranscript(msgs []llm.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			if results := extractToolResults(m.Content); len(results) > 0 {
				for _, r := range results {
					sb.WriteString(fmt.Sprintf("[tool result] %s\n", truncateForSummary(r, 300)))
				}
			} else {
				sb.WriteString(fmt.Sprintf("[user] %s\n", extractTextContent(m.Content)))
			}
		case "assistant":
			text := extractTextContent(m.Content)
			if text != "" {
				sb.WriteString(fmt.Sprintf("[assistant] %s\n", text))
			}
			for _, id := range collectToolUseIDs(m.Content) {
				sb.WriteString(fmt.Sprintf("[tool call: %s]\n", id))
			}
		}
	}
	s := sb.String()
	if len(s) > 60000 {
		s = s[:60000] + "\n...[transcript truncated]"
	}
	return s
}

// isToolResultMessage reports whether the message is a user message that
// contains tool_result blocks.
func isToolResultMessage(m llm.Message) bool {
	if m.Role != "user" {
		return false
	}
	return len(collectToolResultIDs(m.Content)) > 0
}

// extractToolResults returns the content strings of tool_result blocks.
func extractToolResults(content interface{}) []string {
	list, _ := content.([]interface{})
	var out []string
	for _, block := range list {
		switch b := block.(type) {
		case map[string]interface{}:
			if t, _ := b["type"].(string); t == "tool_result" {
				if c, ok := b["content"].(string); ok {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

// truncateForSummary truncates a string for inclusion in a summary prompt.
func truncateForSummary(s string, maxLen int) string {
	// Take first line only for compactness
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// ─── Helpers ───────────────────────────────────────────────────

func (a *Agent) buildDSMessages(currentPrompt string) []llm.DSMessage {
	var out []llm.DSMessage

	sysPrompt := BuildSystemPromptWithDir(a.mode, a.cfg.SystemPrompt, a.cfg.WorkDir)
	if a.gitContext != "" {
		sysPrompt += "\n\n" + a.gitContext
	}
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

func (a *Agent) SelfIterate(ctx context.Context) error {
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

	return a.RunCommand(ctx, prompt)
}

func (a *Agent) AutoRepair(ctx context.Context, bugDesc string) error {
	a.SetMode(ModeAutoRepair)
	fmt.Println("🔧 Auto-Repair Mode — fixing:", bugDesc)
	return a.RunCommand(ctx, "Bug report: "+bugDesc+"\n\nFix it and rebuild.")
}

func (a *Agent) Rebuild() error {
	fmt.Println("\n🔨 Rebuilding...")
	cmd := exec.Command("go", "build", "-o", "pigo", ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *Agent) RunCommand(ctx context.Context, prompt string) error {
	_, err := a.Run(ctx, prompt)
	return err
}

// ─── Reload ─────────────────────────────────────────────────────

// Reload re-reads context files (AGENTS.md, CLAUDE.md, docs/) and re-registers tools.
// It keeps the current session, message history, model, and thinking level intact.
// Returns a summary of what was reloaded.
func (a *Agent) Reload() (string, error) {
	home, _ := os.UserHomeDir()
	var reloaded []string

	// 1. Re-read AGENTS.md / CLAUDE.md context files (skipped with -nc)
	if !a.cfg.NoContextFiles {
		newCtx := loadContextFiles(home)
		if newCtx != a.cfg.SystemPrompt {
			a.cfg.SystemPrompt = newCtx
			reloaded = append(reloaded, "context files (AGENTS.md/CLAUDE.md/docs)")
		}
	}

	// 2. Re-register tools (in case new tools were added)
	a.registry = tools.NewRegistry()
	a.registry.Register(&tools.ReadTool{})
	a.registry.Register(&tools.WriteTool{})
	a.registry.Register(&tools.EditTool{})
	a.registry.Register(&tools.BashTool{})
	a.registry.Register(&tools.LsTool{})
	a.registry.Register(&tools.GrepTool{})
	a.registry.Register(&tools.FindTool{})
	reloaded = append(reloaded, "tools")

	// 3. Re-read config (API key might have changed in .env)
	newAPIKey := lookupKeyFromEnv()
	if newAPIKey != "" && newAPIKey != a.cfg.APIKey {
		a.cfg.APIKey = newAPIKey
		a.client = llm.New(a.cfg.APIKey, a.cfg.BaseURL, a.cfg.Model)
		a.deepseekClient = llm.NewDeepSeekClient(a.cfg.APIKey, a.cfg.DSBaseURL)
		reloaded = append(reloaded, "API credentials")
	}

	// 4. Refresh git context
	a.refreshGitContext()
	reloaded = append(reloaded, "git context")

	if len(reloaded) == 0 {
		return "nothing changed — already up to date", nil
	}

	return strings.Join(reloaded, ", ") + " — reloaded", nil
}

// refreshGitContext gathers project structure & recent git history
// for AI context. Cached; call on startup and /reload.
func (a *Agent) refreshGitContext() {
	// Only if we're in a git repo
	if _, err := exec.Command("git", "rev-parse", "--git-dir").Output(); err != nil {
		a.gitContext = ""
		return
	}

	var parts []string

	// Branch
	if out, err := exec.Command("git", "branch", "--show-current").Output(); err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			parts = append(parts, "## Git Branch: "+branch)
		}
	}

	// Recent commits
	if out, err := exec.Command("git", "log", "--oneline", "-10").Output(); err == nil {
		commits := strings.TrimSpace(string(out))
		if commits != "" {
			parts = append(parts, "## Recent Commits\n```\n"+commits+"\n```")
		}
	}

	// Working tree status
	if out, err := exec.Command("git", "status", "--short").Output(); err == nil {
		status := strings.TrimSpace(string(out))
		if status != "" {
			if len(status) > 600 {
				status = status[:600] + "\n... (truncated)"
			}
			parts = append(parts, "## Working Tree (git status --short)\n```\n"+status+"\n```")
		} else {
			parts = append(parts, "## Working Tree: clean ✓")
		}
	}

	// Unstaged diff stat (uncommitted changes)
	if out, err := exec.Command("git", "diff", "--stat").Output(); err == nil {
		diff := strings.TrimSpace(string(out))
		if diff != "" {
			if len(diff) > 400 {
				diff = diff[:400] + "\n..."
			}
			parts = append(parts, "## Unstaged Changes (git diff --stat)\n```\n"+diff+"\n```")
		}
	}

	// Staged diff stat
	if out, err := exec.Command("git", "diff", "--staged", "--stat").Output(); err == nil {
		staged := strings.TrimSpace(string(out))
		if staged != "" {
			if len(staged) > 400 {
				staged = staged[:400] + "\n..."
			}
			parts = append(parts, "## Staged Changes (git diff --staged --stat)\n```\n"+staged+"\n```")
		}
	}

	a.gitContext = strings.Join(parts, "\n\n")
}

func loadContextFiles(home string) string {
	var parts []string

	// Global AGENTS.md
	if home != "" {
		if data, err := os.ReadFile(filepath.Join(home, ".pigo", "AGENTS.md")); err == nil {
			parts = append(parts, string(data))
		}
	}

	// Project AGENTS.md / CLAUDE.md
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if data, err := os.ReadFile(name); err == nil {
			parts = append(parts, string(data))
			break // only one project-level context file
		}
	}

	// Docs reference
	if _, err := os.Stat("docs"); err == nil {
		parts = append(parts, "Check `docs/` for pi design reference & feature specs.")
	}

	if len(parts) == 0 {
		return "You are PiGo — a coding agent in Go.\nTools: read, write, edit, bash.\nBe concise. Use edit for changes.\n\n## Docs\nCheck `docs/` for pi design reference & feature specs.\n"
	}

	return strings.Join(parts, "\n\n")
}

// lookupKeyFromEnv reads API key from environment variables.
func lookupKeyFromEnv() string {
	for _, k := range []string{"DEEPSEEK_API_KEY", "ANTHROPIC_API_KEY", "PIGO_API_KEY"} {
		if v := os.Getenv(k); v != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ─── Tool Display Helpers ──────────────────────────────────────

// truncDisplay truncates a string for terminal display (rune-safe).
func truncDisplay(s string, maxLen int) string {
	// Take first line only for preview
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen-3]) + "..."
	}
	return s
}

// displayEditPreview shows a compact before→after diff hint.
func displayEditPreview(oldText, newText string) {
	ol := truncDisplay(oldText, 40)
	nl := truncDisplay(newText, 40)
	if ol == nl {
		return
	}
	fmt.Fprintf(os.Stderr, " %s-%s%s %s+%s%s",
		ANSIRed, ol, ANSIReset,
		ANSIGreen, nl, ANSIReset)
}

// displayBashOutput prints command output with a clean gutter.
// Lines are indented with a vertical bar; output is capped at 50 lines.
func displayBashOutput(output string) {
	lines := strings.Split(output, "\n")
	limit := 50
	truncated := len(lines) > limit
	if truncated {
		lines = lines[:limit]
	}
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		// Replace control characters (except tab) with a dot
		cleaned := strings.Map(func(r rune) rune {
			if r < 32 && r != '\t' {
				return '.'
			}
			return r
		}, line)
		fmt.Fprintf(os.Stderr, "%s  │%s %s\n", ANSIGray, ANSIReset, cleaned)
	}
	if truncated {
		fmt.Fprintf(os.Stderr, "%s  │%s %s... (%d lines total, showing first %d)%s\n",
			ANSIGray, ANSIReset, ANSIGray, len(lines)+1, limit, ANSIReset)
	}
}

// toolArgPreview returns a short preview of tool arguments.
func toolArgPreview(tu llm.ContentBlock) string {
	switch tu.Name {
	case "read":
		path, _ := tu.Input["path"].(string)
		return filepath.Base(path)
	case "write":
		path, _ := tu.Input["path"].(string)
		return filepath.Base(path)
	case "grep":
		pattern, _ := tu.Input["pattern"].(string)
		return truncDisplay(pattern, 60)
	case "find":
		pattern, _ := tu.Input["pattern"].(string)
		return truncDisplay(pattern, 40)
	case "ls":
		path, _ := tu.Input["path"].(string)
		if path == "" || path == "." {
			return "."
		}
		return filepath.Base(path)
	}
	return ""
}

func getTerminalSize() (int, int) {
	// syscall-based — no subprocess spawn on every footer
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0
	}
	return cols, rows
}

// TruncRunes truncates a string to at most n runes (not bytes), avoiding
// cutting multi-byte UTF-8 characters (CJK, emoji) mid-sequence.
func TruncRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// TruncRunesRight keeps the last n runes of a string (rune-safe).
func TruncRunesRight(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
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

// validThinking is the set of accepted thinking levels (used by SetThinking).
var validThinking = map[ThinkingLevel]bool{
	ThinkOff: true, ThinkLow: true, ThinkMedium: true, ThinkHigh: true, ThinkMax: true,
}
