<p align="center">
  <img alt="PiGo" src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" />
  <img alt="Platform" src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square" />
  <img alt="Models" src="https://img.shields.io/badge/models-DeepSeek-4D6BFE?style=flat-square" />
</p>

# 🐹 PiGo — pi in Go

**A minimal, self-evolving coding agent for the terminal — written in Go, tailored for DeepSeek, built for old machines.**

PiGo is a single-binary reimplementation of [pi](https://pi.dev) — the minimal terminal coding harness — in pure Go. It keeps pi's philosophy (a small tool loop of `read` / `write` / `edit` / `bash`, sessions, compaction, self-iteration) but drops the Node.js runtime entirely.

> **[中文版 README](README.zh-CN.md)** · English

## 为什么有 PiGo / Why PiGo

| | |
|---|---|
| 🖥️ **Old machines** | pi runs on Node.js ≥ 20 — my machine is too old to install a recent Node.js, and most mainstream coding agents quietly dropped support for old systems. PiGo needs nothing but a Go toolchain (or a prebuilt binary) and compiles to a tiny static binary. |
| 🧠 **Self-evolving** | `/self` makes the agent read its own source code, propose improvements, edit files, rebuild, and iterate. PiGo improves itself. |
| 🎯 **DeepSeek-first** | Optimized for the DeepSeek family — Anthropic-compatible chat API for tool calling, plus the native DeepSeek API for `deepseek-reasoner` with **online Chain-of-Thought (CoT) streaming**. |
| 📖 **pi-inspired** | Feature set and session format closely follow pi (see `docs/pi-readme.md`, the full pi reference). |

---

## Features

- **Interactive TUI** — CJK-aware line editor (`liner`), multi-line input, code blocks, external editor, paste detection
- **Tool calling loop** — `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls` with live streaming output
- **Chain-of-Thought** — `deepseek-reasoner` streams thinking to `stderr` in real time; inline ` 回复`/`<reply>` tag parsing with cross-chunk reassembly
- **Thinking levels** — `off` / `low` / `medium` / `high` / `max`
- **Self-iteration** — `/self` reads the whole codebase, improves it, and rebuilds
- **Auto-repair** — `/repair <desc>` diagnoses and fixes bugs; `/autorepair on` triggers it automatically on errors
- **Sessions** — JSONL persistence, `/save`, `/load`, `/resume`, auto-save on quit, context **compaction** at ~85% context window
- **ESC interrupt** — press `ESC`/`Ctrl+C` mid-response to stop and steer with a follow-up message
- **Token dashboard** — footer shows model, thinking level, session, context fill %, cache hits, and running USD cost
- **Git context** — branch, recent commits, and working-tree status are auto-injected into the system prompt
- **Print mode** — `pigo -p "prompt"`, `@file` arguments, and piped-stdin merging for scripting

---

## Requirements

- **Runtime**: macOS or Linux. No Node.js, no npm, no runtime dependencies.
- **Build**: Go 1.21+ (`go build`). Prebuilt binaries can skip Go entirely.
- **API**: a [DeepSeek](https://platform.deepseek.com) API key (Anthropic-compatible endpoint).

## Installation

### Option 1 — Build from source

```bash
git clone https://github.com/colornote/pigo.git
cd pigo
make build          # → ./pigo
sudo make install   # → /usr/local/bin/pigo
# or, no sudo:
make install-local  # → $(go env GOPATH)/bin
```

### Option 2 — Download a release binary

Grab the `pigo` binary for your platform from the [Releases](https://github.com/colornote/pigo/releases) page, put it on your `PATH`, and `chmod +x pigo`.

---

## Quick Start

```bash
# 1. Configure your API key (also reads DEEPSEEK_API_KEY / ANTHROPIC_API_KEY)
cp .env.example ~/.pigo/.env
$EDITOR ~/.pigo/.env        # set DEEPSEEK_API_KEY=sk-...

# 2. Start an interactive session in your project
cd /path/to/project
pigo
```

You'll see the startup banner with your model, working directory, and session, then a `▸` prompt. Just type your request — PiGo reads files, runs commands, and edits code for you.

One-shot usage:

```bash
pigo "Explain this repo"
pigo @main.go "Review this file"
cat README.md | pigo -p "Summarize this text"
pigo -c "continue from where we left off"
```

---

## Configuration

Config is loaded from `~/.pigo/.env` (global) and `./.env` (project, overrides). Project-level context files `AGENTS.md` / `CLAUDE.md` are injected into the system prompt.

| Variable | Default | Description |
|---|---|---|
| `PIGO_PROVIDER` | `deepseek` | Active provider: `deepseek` or `opencode-go` |
| `DEEPSEEK_API_KEY` / `OPENCODE_API_KEY` / `ANTHROPIC_API_KEY` / `PIGO_API_KEY` | — | API key (provider's own key wins) |
| `PIGO_MODEL` | provider default (`deepseek-v4-flash`) | Default model |
| `PIGO_BASE_URL` | per provider | Anthropic-compatible endpoint for tool calling |
| `PIGO_DS_BASE_URL` | per provider | Native OpenAI-compatible endpoint (CoT / key verification) |
| `PIGO_THINKING` | `medium` | Default thinking level: `off/low/medium/high/max` |
| `PIGO_MAX_TURNS` | `50` | Max agent loop iterations per request |
| `PIGO_WORKDIR` | cwd | Working directory (shown to the model) |
| `PIGO_SESSION_DIR` | `~/.pigo/sessions` | Session storage directory |
| `PIGO_AUTOREPAIR` | `false` | Auto-repair on error (`true`) |
| `PIGO_DEBUG` | — | Dump API requests to stderr (`1`) |

## Providers

| Provider | ID | API key | Models |
|---|---|---|---|
| DeepSeek | `deepseek` (default) | `DEEPSEEK_API_KEY` | `deepseek-v4-flash`, `deepseek-v4-pro[1m]`, `deepseek-chat`, `deepseek-reasoner` 🧠 |
| OpenCode Go | `opencode-go` | `OPENCODE_API_KEY` | 16 curated open models incl. DeepSeek V4, Kimi, GLM, Qwen, MiniMax, Grok (`/models` to list) |

[OpenCode Go](https://opencode.ai/go) is a low-cost subscription ($5 first month, then $10/month) giving reliable access to popular open coding models; it is API-key compatible like any other provider.

- Switch providers with `/provider <id>`, `--provider <id>`, or `PIGO_PROVIDER`
- `/model <name>` auto-switches provider when the model belongs to another one
- `/login` walks through provider selection, key verification, and persistence

## Models

| Model | Description | CoT |
|---|---|---|
| `deepseek-v4-flash` | V4 Flash — fast, default | |
| `deepseek-v4-pro[1m]` | V4 Pro 1M — long context | |
| `deepseek-v4-pro` | alias → `deepseek-v4-pro[1m]` (deepseek) / real model (opencode-go) | |
| `deepseek-chat` | Chat — general | |
| `deepseek-reasoner` | Reasoner — deep reasoning | 🧠 online CoT streaming |

OpenCode Go adds `deepseek-v4-pro`, `kimi-k2.7-code`, `kimi-k2.6`, `kimi-k3`, `qwen3.7-max/plus`, `qwen3.6-plus`, `glm-5.2/5.1`, `minimax-m3/m2.7`, `mimo-v2.5(-pro)`, `hy3`, `grok-4.5` — run `/models` or `pigo --list-models` for the full list. All opencode-go models route through the Anthropic-compatible endpoint so the full tool loop works.

### Vision (multimodal)

Two ways to work with images:

**1. Vision sub-agent tool (recommended with text models like DeepSeek).**
The `vision` tool is a global bridge to the multimodal `mimo-v2.5` model on
opencode-go. Your main agent (e.g. DeepSeek) calls it like any other tool; the
vision model reads the image and returns a text description the main agent
continues from:

```bash
# Set the vision key once:
#   export OPENCODE_API_KEY=oc-...   (or /login opencode-go, then /reload)

# Then just point the main agent at an image:
pigo "Read screenshot.png and describe the UI"
pigo "What does docs/architecture.png show? Can you spot any issues?"
```

The main agent uses `vision <path> [question]`; no provider switch needed.
When the vision tool runs, you'll see the sub-agent's output streamed live
on stderr — its reasoning (💭 视觉推理) and answer (💡 视觉回答) — while the
same text is fed back to the main agent as the tool result.

**2. Direct multimodal model.** `mimo-v2.5` / `mimo-v2.5-pro` as the *main*
model see images via the `read` tool:

```bash
pigo --model mimo-v2.5 "Read screenshot.png and describe the UI"
```

Configuration (optional — defaults shown):

| Variable | Default | Description |
|---|---|---|
| `OPENCODE_API_KEY` | — | Vision model auth (also used by opencode-go) |
| `PIGO_VISION_MODEL` | `mimo-v2.5` | Vision sub-agent model |
| `PIGO_VISION_BASE_URL` | `https://opencode.ai/zen/go` | Vision endpoint |

Switch at runtime with `/model <name>`; list with `/models`. Only models flagged 🧠 (native `reasoning_content` CoT) use the native CoT path — everything else uses the Anthropic-compatible tool-calling loop.

---

## Usage

### CLI options

```
Usage: pigo [options] [@files...] [prompt]

  --help, -h        Show help
  --version, -v     Show version
  --model <name>    Set model for single-shot
  --thinking <lvl>  Set thinking level
  --print, -p       Non-interactive: print response and exit
  --continue, -c    Continue most recent session
  --resume, -r      Browse and select from past sessions
  --session <id>    Load specific session by ID prefix
  --name <name>     Set session display name
  --no-session      Ephemeral mode (don't save)
```

### Interactive commands

| Command | Description |
|---|---|
| `/model <name>` | Switch model (auto-switches provider if needed) |
| `/models` | List models of the active provider |
| `/provider [id]` | Show / switch provider |
| `/thinking <lvl>` | Set thinking: `off/low/medium/high/max` |
| `/self` | 🔁 **Self-iterate**: read source → improve → rebuild |
| `/repair <desc>` | 🔧 Auto-repair a bug from a description |
| `/autorepair [on\|off]` | Toggle automatic repair on errors |
| `/mode` | Show mode, model, thinking |
| `/reload` | Reload context files, tools, config |
| `/compact [instr]` | Summarize old messages to free context |
| `/session` | Show current session info |
| `/save [name]` | Save / name current session |
| `/load <id>` | Load a session by ID prefix |
| `/resume` | Browse and pick a session to resume |
| `/multiline` | Open editor for multi-line input |
| `/help`, `/quit` | Help / exit |

### Multi-line input

| Shortcut | Meaning |
|---|---|
| `\` at end of line | Continue on the next line (line-numbered) |
| `\e` at end of line | Open the external editor with current content |
| ` ``` ` on its own line | Start a code block (` ``` ` again to end) |
| `ESC` / `Ctrl+C` during a run | Interrupt the model, then append a follow-up prompt |

---

## Sessions

PiGo saves every conversation as a JSONL file under `~/.pigo/sessions/<project-slug>/` (inspired by pi's session format). Each entry carries an ID and parent ID, so sessions could support tree-style branching.

- Sessions auto-save on `/quit`, `Ctrl+C`, and `Ctrl+D`
- `/save [name]`, `/load <id>`, `/resume`, `--continue`, `--session <id>` restore full history
- `/compact` summarizes older turns (recorded as a `compaction` entry that survives resume); auto-compaction kicks in at ~85% context window
- Orphan `tool_use` blocks from interrupted runs are cleaned up automatically on resume

## Self-iteration & Auto-repair

```bash
# Self-iterate: agent reads all .go files, proposes changes,
# edits them, runs `go build -o pigo .`, and iterates.
pigo
> /self

# Auto-repair a bug
> /repair "the footer truncation is misaligned for CJK text"

# Or let it fix errors automatically:
> /autorepair on
```

Self-iteration uses the `docs/pi-design.md` feature-parity checklist and `docs/pi-readme.md` (the full pi reference) as its roadmap — PiGo literally reads its own blueprint and closes gaps.

---

## Architecture

```
pigo/
├── main.go              # CLI, TUI, ESC interrupts, command dispatch
├── agent/
│   ├── loop.go          # Core agent loop, sessions, compaction, footer
│   ├── modes.go         # Modes, model registry, system prompts
│   └── tui.go           # CJK-aware width/truncation helpers
├── config/config.go     # Config + .env loading
├── llm/
│   ├── client.go        # DeepSeek Anthropic-compatible API (tool calling)
│   ├── deepseek.go      # DeepSeek native API (CoT streaming)
│   └── usage.go         # Token tracking & USD cost
├── session/session.go   # JSONL session persistence
├── tools/tools.go       # read / write / edit / bash / grep / find / ls
├── termios_*.go         # Platform-specific termios (darwin / linux)
└── docs/                # pi reference + PiGo design blueprint
```

### Design highlights

- **Two API paths**: the Anthropic-compatible Messages API (`/v1/messages`) for tool-calling on all models; the native DeepSeek Chat Completions API (`/v1/chat/completions`) with `reasoning_content` for `deepseek-reasoner` CoT.
- **Inline CoT parsing**: some DeepSeek models emit reasoning wrapped in ` 回复 … /回复` or `<reply>…</reply>` tags inside regular content. `llm/deepseek.go` routes those to the thinking display, never swallows tagless content, and reassembles tags split across SSE chunks (covered by `llm/deepseek_test.go`).
- **CJK-first TUI**: rune-safe truncation and display-width math — the Chinese UI, prompt status, footer, and session listing align correctly in CJK terminals.
- **Terminal safety**: raw-mode ESC listening via `select()`, terminal restore on every exit path, `stdinDrain` after raw→cooked transitions — no orphaned raw ttys.

---

## Comparison with pi

| | pi | PiGo |
|---|---|---|
| Runtime | Node.js ≥ 20 | **Static Go binary** |
| Install | npm / curl | `make install` or download |
| Language | TypeScript | Go 1.21+ |
| Provider focus | multi-provider | **DeepSeek** (Anthropic-compatible + native CoT) |
| Sessions | JSONL + branching | JSONL + ID tree structure |
| Compaction | ✓ | ✓ (`/compact` + auto) |
| Self-iteration | via extensions | built-in `/self` |
| Extensions/Skills/MCP | ✓ | planned (see Roadmap) |

## Roadmap

- [ ] Prebuilt release binaries + GitHub Actions CI
- [ ] `/login`-style credential management
- [ ] Context-file `@` fuzzy file picker (pi parity)
- [ ] JSON / RPC output modes for scripting
- [ ] MCP server / extensions
- [ ] Windows support

## Development

```bash
make build    # go build -o pigo .
make test     # go test ./...
make clean    # remove binary
```

Then run `pigo` and try `/self` — it will start improving itself.

## License

[MIT](LICENSE)
