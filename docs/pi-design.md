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
- [x] Print mode (`-p`/`--print`) + `@file` CLI args + piped stdin merge
- [x] `--session-dir <dir>` — custom session storage directory (CLI override of `PIGO_SESSION_DIR`)
- [x] `--api-key <key>` — override API key (overrides env vars)
- [x] `--no-context-files`/`-nc` — disable AGENTS.md/CLAUDE.md loading
- [x] `--list-models` — list available models and exit
- [x] `/name <name>` — set session display name (`--name`/`-n` at startup now actually names the first session)
- [x] `--session <path|id>` — full `.jsonl` path support (not just ID prefix)
- [x] `-p` with no prompt/stdin errors instead of silently entering interactive mode
- [x] `goodbye()` no longer creates an empty session on `--help`/`--version`/`--list-models`
- [x] `/login` / `/logout` — pick provider → paste API key → verify against native API (`GET /models`) → persist to `~/.pigo/.env` (0600) → hot-apply via `agent.SetAPIKey()` (session/messages/model untouched). DeepSeek only for now; provider registry in `agent/providers.go` is extensible.
- [x] Multimodal vision — opencode-go `mimo-v2.5` / `mimo-v2.5-pro` (verified against `opencode.ai/zen/go/v1/models`). `read` returns image files (png/jpg/jpeg/gif/webp/bmp ≤5MB) as base64 data URLs; the agent loop converts them to Anthropic `image` content blocks (OpenAI protocol: `image_url`) so vision models see the picture.
- [x] Vision sub-agent tool (`vision`) — a global tool that calls a multimodal model (default `mimo-v2.5` on opencode-go, configurable via `PIGO_VISION_MODEL` / `PIGO_VISION_BASE_URL`, auth `OPENCODE_API_KEY`) to analyze an image and return a text description to the MAIN agent. Text models (DeepSeek) never see raw base64: `read` returns a `[Image: … use the vision tool …]` hint for text main models (`ImageModeHint`) and a base64 data URL only for multimodal main models (`ImageModeDataURL`). Runner injected by `agent.New`/`Reload` (`tools.VisionTool.Runner`), tools package stays free of llm imports.

## Tools Policy
- **8 tools: read, write, edit, bash, grep, find, ls, vision**
- grep/find/ls were re-added (commit 705ad4c) — they're useful for the model
- No evidence of infinite loops with current prompt constraints

## Known Issues to Fix
- [x] Session auto-save on /quit (done)
- [x] Compaction (summarize long sessions) — `/compact [instructions]` + auto-compact at ~85% context; summary recorded as a `compaction` session entry and re-injected on resume
- [x] Handle SIGINT (Ctrl+C) gracefully in cooked mode (auto-save session — done via signal.Notify + goodbye)

## Notes
- Inline CoT tag parsing (`llm/deepseek.go`): fixed a bug where an empty opening-tag
  string caused all content deltas to be routed to the thinking display (answers were
  swallowed). Parser now accepts Chinese (` 回复`/` /回复`, ` 思考`/` /思考`) and HTML
  (`<reply>`/`</reply>`) tag pairs, never swallows tagless content, and reassembles
  tags split across SSE chunks via a pending buffer. Covered by `llm/deepseek_test.go`.
- Tool registry (`tools/tools.go`): `Registry.List()` returns tools in registration
  order (was map iteration order), so the tool schema sent to the API is identical
  across runs and calls. The registered set is unchanged — read/write/edit/bash/
  grep/find/ls. System prompt now lists all 7 tools so the model uses grep/find/ls
  instead of falling back to bash.
- Bash output gutter (`agent/loop.go` `displayBashOutput`): truncation message now
  reports the real total line count (was counting the truncated slice).
- Image data URLs in tool results: session JSONL stores the raw data URL
  (`a.saveEntry("tool", resultJSON, …)`), and `ResumeSession` rebuilds the image
  content block via `toolResultContent()` — so images survive session resume
  and compaction. `estimateTokens()` ignores image blocks (no text to count),
  which keeps base64 blobs out of the compaction transcript.
