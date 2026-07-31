<p align="center">
  <img alt="PiGo" src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" />
  <img alt="Platform" src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square" />
  <img alt="Models" src="https://img.shields.io/badge/models-DeepSeek-4D6BFE?style=flat-square" />
</p>

# 🐹 PiGo — pi in Go

**一个为终端打造的极简自进化编码智能体 —— 用 Go 编写，为 DeepSeek 优化，专为老机器而生。**

PiGo 是 [pi](https://pi.dev)（极简终端编码工具）的纯 Go 单二进制重实现。它保留了 pi 的核心理念（`read` / `write` / `edit` / `bash` 的小工具循环、会话、压缩、自进化），但彻底摆脱了 Node.js 运行时。

> **[English README](README.md)** · 中文版

---

## 为什么有 PiGo

| | |
|---|---|
| 🖥️ **老机器友好** | pi 需要 Node.js ≥ 20 —— 我的机器太老，装不了新版 Node.js，而主流编码智能体也都悄悄放弃了对旧系统的支持。PiGo 只需要一个 Go 工具链（或直接下载预编译二进制），编译出小巧的静态二进制即可运行。 |
| 🧠 **自进化** | `/self` 让智能体阅读自己的源码、提出改进方案、编辑文件、重新编译，然后继续迭代。PiGo 会不断自我完善。 |
| 🎯 **DeepSeek 优先** | 针对 DeepSeek 系列模型深度优化 —— 使用 Anthropic 兼容 API 进行工具调用，同时使用 DeepSeek 原生 API 支持 `deepseek-reasoner` 的**在线思维链（CoT）流式输出**。 |
| 📖 **pi 风格** | 功能与会话格式紧密参考 pi（详见 `docs/pi-readme.md`，完整的 pi 参考文档）。 |

---

## 功能特性

- **交互式 TUI** — 支持 CJK（中日韩文字）的编辑器（基于 `liner`）、多行输入、代码块、外部编辑器、粘贴检测
- **工具调用循环** — `read`、`write`、`edit`、`bash`、`grep`、`find`、`ls`，支持实时流式输出
- **思维链（CoT）** — `deepseek-reasoner` 实时将思考过程流式输出到 `stderr`；支持内联 ` 回复`/`<reply>` 标签解析与跨分片重组
- **思考级别** — `off` / `low` / `medium` / `high` / `max`
- **自进化** — `/self` 读取整个代码库、自我改进并重新编译
- **自动修复** — `/repair <描述>` 诊断并修复 bug；`/autorepair on` 出错时自动触发修复
- **会话** — JSONL 持久化，`/save`、`/load`、`/resume`，退出自动保存，上下文窗口 ~85% 时自动**压缩**
- **ESC 打断** — 响应中按 `ESC`/`Ctrl+C` 停止，并追加后续指令继续引导
- **用量面板** — 底部显示模型、思考级别、会话、上下文占用 %、缓存命中、累计 USD 费用
- **Git 上下文** — 自动把分支、最近提交、工作区状态注入系统提示词
- **打印模式** — `pigo -p "提示词"`、`@file` 参数、管道 stdin 合并，方便脚本化

---

## 环境要求

- **运行时**：macOS 或 Linux。无需 Node.js、无需 npm、无运行时依赖。
- **构建**：Go 1.21+（`go build`）。使用预编译二进制可完全跳过 Go 环境。
- **API**：一个 [DeepSeek](https://platform.deepseek.com) API 密钥（Anthropic 兼容端点）。

## 安装

### 方式一：从源码构建

```bash
git clone https://github.com/colornote/pigo.git
cd pigo
make build          # → ./pigo
sudo make install   # → /usr/local/bin/pigo
# 或无需 sudo：
make install-local  # → $(go env GOPATH)/bin
```

### 方式二：下载发布版二进制

从 [Releases](https://github.com/colornote/pigo/releases) 页面下载对应平台的 `pigo` 二进制，放到你的 `PATH` 中并 `chmod +x pigo`。

---

## 快速上手

```bash
# 1. 配置 API 密钥（也会读取 DEEPSEEK_API_KEY / ANTHROPIC_API_KEY）
cp .env.example ~/.pigo/.env
$EDITOR ~/.pigo/.env        # 设置 DEEPSEEK_API_KEY=sk-...

# 2. 在项目目录启动交互会话
cd /path/to/project
pigo
```

启动后会显示包含模型、工作目录、会话信息的横幅，然后出现 `▸` 提示符。直接输入你的需求 —— PiGo 会帮你读文件、跑命令、改代码。

单次使用：

```bash
pigo "解释一下这个仓库"
pigo @main.go "审查这个文件"
cat README.md | pigo -p "总结这段文字"
pigo -c "从上次的进度继续"
```

---

## 配置

配置从 `~/.pigo/.env`（全局）和 `./.env`（项目，覆盖全局）加载。项目级上下文文件 `AGENTS.md` / `CLAUDE.md` 会被注入系统提示词。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DEEPSEEK_API_KEY` / `ANTHROPIC_API_KEY` / `PIGO_API_KEY` | — | API 密钥（任意一个即可） |
| `PIGO_MODEL` | `deepseek-v4-flash` | 默认模型 |
| `PIGO_BASE_URL` | `https://api.deepseek.com/anthropic` | 工具调用的 Anthropic 兼容端点 |
| `PIGO_DS_BASE_URL` | `https://api.deepseek.com` | CoT/推理用的 DeepSeek 原生端点 |
| `PIGO_THINKING` | `medium` | 默认思考级别：`off/low/medium/high/max` |
| `PIGO_MAX_TURNS` | `50` | 每次请求的智能体循环最大轮数 |
| `PIGO_WORKDIR` | 当前目录 | 工作目录（展示给模型） |
| `PIGO_SESSION_DIR` | `~/.pigo/sessions` | 会话存储目录 |
| `PIGO_AUTOREPAIR` | `false` | 出错时自动修复（`true`） |
| `PIGO_DEBUG` | — | 把 API 请求输出到 stderr（`1`） |

## 模型

| 模型 | 说明 | CoT |
|---|---|---|
| `deepseek-v4-flash` | V4 Flash — 快速，默认 | |
| `deepseek-v4-pro[1m]` | V4 Pro 1M — 长上下文 | |
| `deepseek-v4-pro` | 别名 → `deepseek-v4-pro[1m]` | |
| `deepseek-chat` | Chat — 通用 | |
| `deepseek-reasoner` | Reasoner — 深度推理 | 🧠 在线 CoT 流式 |

运行时用 `/model <名称>` 切换；`/models` 列出。只有 `deepseek-reasoner`（且思考级别 ≠ off）走原生 CoT 路径 —— 其他模型全部走 Anthropic 兼容的工具调用循环。

---

## 使用说明

### CLI 选项

```
Usage: pigo [options] [@files...] [prompt]

  --help, -h        显示帮助
  --version, -v     显示版本
  --model <名称>     单次运行设置模型
  --thinking <级别>  设置思考级别
  --print, -p       非交互模式：打印响应后退出
  --continue, -c    继续最近的会话
  --resume, -r      浏览并选择历史会话
  --session <id>    按 ID 前缀加载指定会话
  --name <名称>      设置会话显示名称
  --no-session      临时模式（不保存）
```

### 交互命令

| 命令 | 说明 |
|---|---|
| `/model <名称>` | 切换模型 |
| `/models` | 列出可用模型 |
| `/thinking <级别>` | 设置思考级别：`off/low/medium/high/max` |
| `/self` | 🔁 **自进化**：读源码 → 改进 → 重新编译 |
| `/repair <描述>` | 🔧 根据描述自动修复 bug |
| `/autorepair [on\|off]` | 出错时自动修复开关 |
| `/mode` | 显示模式、模型、思考级别 |
| `/reload` | 重新加载上下文文件、工具、配置 |
| `/compact [指令]` | 总结旧消息释放上下文 |
| `/session` | 显示当前会话信息 |
| `/save [名称]` | 保存 / 命名当前会话 |
| `/load <id>` | 按 ID 前缀加载会话 |
| `/resume` | 浏览并选择要恢复的会话 |
| `/multiline` | 打开编辑器进行多行输入 |
| `/help`、`/quit` | 帮助 / 退出 |

### 多行输入

| 快捷键 | 说明 |
|---|---|
| 行尾输入 `\` | 在下一行继续输入（带行号） |
| 行尾输入 `\e` | 用外部编辑器打开当前内容 |
| 单独一行输入 ` ``` ` | 开始代码块（再次输入 ` ``` ` 结束） |
| 运行中按 `ESC` / `Ctrl+C` | 打断模型，然后追加后续提示词 |

---

## 会话

PiGo 把每段对话以 JSONL 文件保存在 `~/.pigo/sessions/<项目slug>/`（参考 pi 的会话格式）。每条记录带 ID 与 parent ID，为将来的树状分支会话打好了基础。

- `/quit`、`Ctrl+C`、`Ctrl+D` 时自动保存会话
- `/save [名称]`、`/load <id>`、`/resume`、`--continue`、`--session <id>` 恢复完整历史
- `/compact` 总结较早的轮次（记录为 `compaction` 条目，恢复时仍保留）；上下文窗口 ~85% 时自动压缩
- 被中断运行留下的孤立 `tool_use` 块会在恢复时自动清理

## 自进化与自动修复

```bash
# 自进化：智能体读取所有 .go 文件，提出修改方案，
# 编辑文件，运行 `go build -o pigo .`，然后继续迭代。
pigo
> /self

# 自动修复 bug
> /repair "中文文本的底部信息栏截断对不齐"

# 或让它在出错时自动修复：
> /autorepair on
```

自进化把 `docs/pi-design.md` 的功能对齐清单和 `docs/pi-readme.md`（完整 pi 参考）作为路线图 —— PiGo 会读取自己的蓝图并填补差距。

---

## 架构

```
pigo/
├── main.go              # CLI、TUI、ESC 打断、命令分发
├── agent/
│   ├── loop.go          # 核心循环、会话、压缩、底部信息栏
│   ├── modes.go         # 模式、模型注册、系统提示词
│   └── tui.go           # CJK 感知的宽度/截断工具
├── config/config.go     # 配置 + .env 加载
├── llm/
│   ├── client.go        # DeepSeek Anthropic 兼容 API（工具调用）
│   ├── deepseek.go      # DeepSeek 原生 API（CoT 流式）
│   └── usage.go         # Token 统计与 USD 费用
├── session/session.go   # JSONL 会话持久化
├── tools/tools.go       # read / write / edit / bash / grep / find / ls
├── termios_*.go         # 平台相关 termios（darwin / linux）
└── docs/                # pi 参考 + PiGo 设计蓝图
```

### 设计亮点

- **双 API 路径**：所有模型走 Anthropic 兼容的 Messages API（`/v1/messages`）做工具调用；`deepseek-reasoner` 的 CoT 走 DeepSeek 原生 Chat Completions API（`/v1/chat/completions`，带 `reasoning_content`）。
- **内联 CoT 解析**：部分 DeepSeek 模型在普通 content 字段中用 ` 回复 … /回复` 或 `<reply>…</reply>` 标签包裹推理内容。`llm/deepseek.go` 会把它们路由到思考展示区，绝不吞掉无标签内容，并能重组跨 SSE 分片的标签（有 `llm/deepseek_test.go` 测试覆盖）。
- **CJK 优先的 TUI**：按 rune 安全截断与显示宽度计算 —— 中文界面、提示状态、底部信息栏、会话列表在 CJK 终端下都能正确对齐。
- **终端安全**：通过 `select()` 做原始模式 ESC 监听、所有退出路径都恢复终端、raw→cooked 转换后 `stdinDrain` —— 绝不留下残废的 raw tty。

---

## 与 pi 的对比

| | pi | PiGo |
|---|---|---|
| 运行时 | Node.js ≥ 20 | **静态 Go 二进制** |
| 安装 | npm / curl | `make install` 或下载 |
| 语言 | TypeScript | Go 1.21+ |
| 提供方 | 多模型提供商 | **DeepSeek**（Anthropic 兼容 + 原生 CoT） |
| 会话 | JSONL + 分支 | JSONL + ID 树结构 |
| 压缩 | ✓ | ✓（`/compact` + 自动） |
| 自进化 | 通过扩展 | 内置 `/self` |
| 扩展 / Skills / MCP | ✓ | 计划中（见路线图） |

## 路线图

- [ ] 预编译发布二进制 + GitHub Actions CI
- [ ] `/login` 式凭证管理
- [ ] 上下文文件 `@` 模糊文件选择器（对齐 pi）
- [ ] JSON / RPC 输出模式，便于脚本化
- [ ] MCP 服务器 / 扩展
- [ ] Windows 支持

## 开发

```bash
make build    # go build -o pigo .
make test     # go test ./...
make clean    # 删除二进制
```

然后运行 `pigo` 试试 `/self` —— 它会开始自我改进。

## License

[MIT](LICENSE)
