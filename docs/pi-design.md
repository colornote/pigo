# PiGo Design — Feature Parity Checklist

## Architecture
```
pigo/
├── main.go              # CLI / command dispatch
├── config/config.go     # Config + .env loading
├── llm/client.go        # Anthropic-compatible API client
├── llm/deepseek.go      # DeepSeek native API (CoT)
├── llm/usage.go         # Token usage tracking
├── tools/tools.go       # Core tools (read/write/edit/bash ONLY)
├── agent/loop.go        # Core agent loop + tool execution
├── agent/modes.go       # Mode system + model registry
├── session/session.go   # JSONL session persistence
└── docs/pi-readme.md    # pi reference docs
```

## Done ✅
- [x] DeepSeek integration (Anthropic-compatible + native CoT)
- [x] Model switching (`/model`)
- [x] Thinking levels (`/thinking`: off/low/medium/high/max)
- [x] Session persistence (JSONL, `/save`, `/load`, `/resume`)
- [x] Mode system (normal / self-iterate / auto-repair)
- [x] Self-iteration (`/self`) — reads own code, edits, rebuilds
- [x] Auto-repair (`/repair`) — diagnoses bugs from description
- [x] Tool use: read, write, edit, bash
- [x] Footer (model, thinking, session, token usage, cost)
- [x] .env config loading
- [x] Working directory in system prompt
- [x] Orphan tool_use cleanup on resume
- [x] Token usage tracking

## Tools Policy ⚠️ IMPORTANT
- **ONLY 4 tools: read, write, edit, bash**
- DO NOT add grep/find/ls tools — they are redundant with bash
- DO NOT add any other tools — bash can do everything
- Adding extra tools caused infinite loops in testing
- The `argPreview` and related display code for removed tools should be cleaned up

## Known Issues to Fix
- [ ] Remove unused `strconv` import hack in tools.go
- [ ] Remove grep/find/ls display remnants from loop.go (toolArgPreview references)
- [ ] Session auto-save on /quit (currently only saves at prompt)

## pi Features to Implement
- [ ] Compaction (summarize long sessions)
- [ ] Branching (/tree, /fork)
- [ ] Message queue (steering messages)
- [ ] External editor integration
- [ ] Image support
- [ ] Git context auto-loading
- [ ] MCP support (via extension)
- [ ] Extension system

## Self-Iteration Rules
- Read ALL .go files first to understand state
- Check this file for the checklist
- Implement missing features, fix bugs
- Run `go build -o pigo .` after changes
- Run `go vet ./...`
- DO NOT add redundant tools (grep/find/ls)
- Update this file after changes
