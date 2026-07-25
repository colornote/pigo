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
- [x] Session auto-save on /quit
- [x] Mode system (normal / self-iterate / auto-repair)
- [x] Self-iteration (`/self`) — reads own code, edits, rebuilds
- [x] Auto-repair (`/repair`) — diagnoses bugs from description
- [x] Tool use: read, write, edit, bash, grep, find, ls
- [x] Footer (model, thinking, session, token usage, cost)
- [x] .env config loading
- [x] Working directory in system prompt
- [x] Orphan tool_use cleanup on resume
- [x] Token usage tracking
- [x] ESC interrupt + follow-up steering messages (message queue lite)
- [x] External editor integration (`\e` suffix, `/multiline`)
- [x] Git context auto-loading (branch, commits, status in system prompt)
- [x] Multi-line input (`\` continuation, `\`\`\`` code blocks)

## Tools Policy
- **7 tools: read, write, edit, bash, grep, find, ls**
- grep/find/ls were re-added (commit 705ad4c) — they're useful for the model
- No evidence of infinite loops with current prompt constraints

## Known Issues to Fix
- [x] Session auto-save on /quit (done)
- [ ] Compaction (summarize long sessions) — needed for long-running agents
- [ ] Handle SIGINT (Ctrl+C) gracefully in cooked mode (auto-save session)
