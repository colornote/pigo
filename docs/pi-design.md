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
- [x] Tool filtering (`--tools`/`-t`, `--exclude-tools`/`-xt`, `--no-tools`/`-nt`) — pi Tool Options parity. Allowlist/denylist applied to the tool schema sent to the API, to tool execution (disabled tools report back "disabled" instead of running), and to the system prompt ("Available tools: …"), so the model stops calling removed tools. Re-applied on `/reload`.
- [x] `!cmd` / `!!cmd` editor shortcuts — run a shell command from the prompt line (pi parity): `!cmd` forwards the output to the LLM as a user message, `!!cmd` just shows it. 30s timeout, process-group kill.
- [x] Bash session env vars — `PI_SESSION_ID`, `PI_SESSION_FILE`, `PI_PROVIDER`, `PI_MODEL`, `PI_REASONING_LEVEL` exported to every bash tool command (pi parity; `PI_SESSION_FILE` unset for ephemeral sessions).
- [x] `/new` — start a fresh session (in-memory history reset; old session stays persisted).
- [x] ESC interrupt + follow-up steering — fixed: `runWithESC` now matches wrapped `context.Canceled` via `errors.Is` (the streaming HTTP error is `"API: Get …: context canceled"`, which a bare `==` check missed — the follow-up flow was dead code), and the retry sends only the follow-up (the original prompt is already in history, so re-sending it duplicated the user message in the session JSONL and model context).
- [x] Multimodal vision — opencode-go `mimo-v2.5` / `mimo-v2.5-pro` (verified against `opencode.ai/zen/go/v1/models`). `read` returns image files (png/jpg/jpeg/gif/webp/bmp ≤5MB) as base64 data URLs; the agent loop converts them to Anthropic `image` content blocks (OpenAI protocol: `image_url`) so vision models see the picture.
- [x] Vision sub-agent tool (`vision`) — a global tool that calls a multimodal model (default `mimo-v2.5` on opencode-go, configurable via `PIGO_VISION_MODEL` / `PIGO_VISION_BASE_URL`, auth `OPENCODE_API_KEY`) to analyze an image and return a text description to the MAIN agent. Text models (DeepSeek) never see raw base64: `read` returns a `[Image: … use the vision tool …]` hint for text main models (`ImageModeHint`) and a base64 data URL only for multimodal main models (`ImageModeDataURL`). Runner injected by `agent.New`/`Reload` (`tools.VisionTool.Runner`), tools package stays free of llm imports.
- [x] Persistent structured memory (`~/.pigo/memory.md`, ACE-style) — `/compact` now asks the model for itemized bullets only (`## Decisions` / `## Artifacts` / `## Commands` / `## Open Issues`, every bullet self-contained), appends the result as a timestamped entry to `~/.pigo/memory.md`, and `buildSysPrompt` injects the logbook into the normal-mode system prompt. Durable knowledge survives across sessions; self-iterate/auto-repair modes stay unsteered. (Harness engineering: Pattern 2 "file system as persistent memory" + ACE context engineering.)
- [x] Verifier-grounded auto-repair — `/repair` (and the `r`-key / auto-repair triggers) now run a fix→verify→refine loop: after the model edits, `verifyRepo()` runs `go build -o pigo .` + `go vet ./...`; on failure the errors are fed back with "analyze the ROOT CAUSE, do not repeat the same approach", up to 3 rounds. A fix is accepted only when build+vet pass. (Harness engineering: Self-Harness/AHE evidence-driven, verifier-grounded edits.)

## Tools Policy
- **8 tools: read, write, edit, bash, grep, find, ls, vision**
- grep/find/ls were re-added (commit 705ad4c) — they're useful for the model
- No evidence of infinite loops with current prompt constraints

## Known Issues to Fix
- [x] Session auto-save on /quit (done)
- [x] Compaction (summarize long sessions) — `/compact [instructions]` + auto-compact at ~85% context; summary recorded as a `compaction` session entry and re-injected on resume
- [x] Handle SIGINT (Ctrl+C) gracefully in cooked mode (auto-save session — done via signal.Notify + goodbye)

## Backlog — 有价值但未实现（来源：Harness Engineering for Self-Improvement, Lilian Weng 2026-07）
按实现性价比排序，均与现有架构兼容，小步可落地。

- [ ] **通用 sub-agent 工具**（Pattern 3: Sub-agent and Backend Jobs）
  - 动机：vision 子代理已验证 runner 注入模式（`tools.VisionTool.Runner` + `agent.runVision`），可抽象为通用子代理，让主代理并行探索多假设、隔离子任务，不污染主上下文。文章：并行要显式且可检视，子代理输出写文件而非只存在于 transient chat。
  - 实现要点：
    - 新工具 `spawn`（参数：prompt / tools 白名单 / 是否独立 model），`tools.SubAgentTool` 复用 VisionTool 的 runner 注入结构
    - 子代理独立 `llm.Client` + 独立消息列表，跑完把结论写入 `~/.pigo/subagents/<id>.md`（结果落盘 = 可恢复、可审计）
    - 配套 `list_agents`（列出子代理及其状态/文件）、`wait`（阻塞等待完成）、`resume`（从落盘结果恢复）
    - 权限继承主代理的 tool filter；上下文超长时子代理内部可先 compaction

- [ ] **`/self` 可编辑面白名单（editable surface 边界）**
  - 动机：文章明确警告"若程序被允许编辑 OS，抽象边界即被打破；权限控制必须位于自改进循环之外"。当前 `/self` 可改整个仓库，风险面过大。
  - 实现要点：
    - 默认只允许 `agent/ tools/ config/ session/ llm/ main.go docs/pi-design.md`；禁止 `.env`、`~/.pigo/`、`go.sum`、`pigo` 二进制
    - 白名单写入 system prompt（self-iterate 模式）；越界路径的 write/edit 工具直接拒绝
    - `--self-scope <dir>` 覆盖（默认仍是保守白名单）

- [ ] **失败案例保留与回顾（负结果优先）**
  - 动机：文章 Future Challenges #3 —— 学习失败是压缩任务搜索空间的最好方式，harness 应让失败尝试易于保存。session JSONL 已天然保留失败历史，但目前 `/repair` 不回顾。
  - 实现要点：
    - `/repair` 前注入最近 N 条历史失败（同会话内 extractToolResults 里匹配 error/失败模式，或读取 `~/.pigo/memory.md` 的 Open Issues）
    - 修复成功后把"bug 描述 → 根因 → 修复"追加到 memory.md（复用 `saveMemory`），形成可复用知识
    - 可选：`/failures` 命令列出历史失败

- [ ] **Memory 条目去重与淘汰（自管理记忆）**
  - 动机：ACE 研究强调 curator 需定期 refine/dedup 条目，防止记忆膨胀稀释上下文；MCE 更进一步把 context 管理本身作为优化目标。
  - 实现要点：
    - 当 memory.md 超过阈值时，用一次 LLM 调用合并/去重旧条目（curator pass），而非简单截断
    - 可选：条目加 identifier，支持 `/forget <id>` 精确删除

- [ ] **组件级可观测性（AHE: Component Observability）**
  - 动机：文章指出 harness 演进的瓶颈是可观测性——失败时要能定位是哪个组件（system prompt / tool description / tool implementation / middleware / skill / sub-agent config / long-term memory）的责任，每次编辑应是文件级、可证伪的声明。
  - 实现要点：
    - 为 memory / system prompt / tool 描述加来源标注（如 `<!-- source: memory.md @2026-07-04 -->`），便于追溯
    - 可选：`/repair` 时把失败归因到组件并记录（轻量版 AHE manifesto）

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
