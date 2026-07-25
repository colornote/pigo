# PiGo Design Plan

> 基于 pi (`@earendil-works/pi-coding-agent`) 核心架构分析。
> pigo 用 Go 实现 pi 的核心功能，具备自我迭代能力。

## 架构

```
pigo/
├── main.go              # CLI 入口，命令分发
├── config/config.go     # 配置 + provider 注册
├── llm/client.go        # Anthropic 兼容 API 客户端 (流式)
├── llm/deepseek.go      # DeepSeek 原生 API 客户端 (CoT/推理)
├── llm/usage.go         # Token 用量追踪 & 模型定价
├── tools/tools.go       # read/write/edit/bash 工具
├── agent/loop.go        # 核心 Agent 循环
├── agent/modes.go       # 模式系统 / 模型注册 / 提示词
├── docs/                # pi 参考文档
└── pigo                 # 编译产物
```

## Pi 特性 → PiGo 实现

### 1. 模型切换 (`/model`)
- pi: `/model` 打开选择器，Ctrl+P 切换 scoped models
- PiGo: `/model [name]` 切换模型。预定义模型注册表：
  - `deepseek-v4-flash` — 快速，默认
  - `deepseek-v4-pro[1m]` — 强大，1M 上下文
  - `deepseek-chat` — 通用
  - `deepseek-reasoner` — 深度推理，支持线上 CoT
- 运行时切换，无需重启

### 2. Thinking Level (`/thinking`)
- pi: `/settings` → thinking level (off/minimal/low/medium/high/xhigh/max)
- PiGo: `/thinking [level]` — 映射到模型特定的 thinking tokens
  - `off` → 2048 tokens
  - `low` → 4096 tokens
  - `medium` → 8192 tokens
  - `high` → 16384 tokens
  - `max` → 32768 tokens

### 3. 模式系统
- **Normal Mode** (默认) — 编码助手，有工具，加载 AGENTS.md
- **Self-Iterate Mode** (`/self`) — 读取自身代码，改进，重新编译
- **Auto-Repair Mode** (`/repair`) — 使用中遇到错误自动修复

### 4. 自修复 (`/repair`)
- 用户遇到 bug → 输入 `/repair "描述"`
- Agent 读取相关文件，诊断，编辑，重新编译
- 也可在 agent 错误时自动触发

### 5. 线上 CoT 链
- `deepseek-reasoner` + thinking≠off 时自动走原生 API
- 实时流式显示 `reasoning_content`（思考链）
- 然后显示 `content`（最终回答）

### 6. Footer 状态栏
- 每次响应后显示：模型 | thinking | 目录 | ◫ tokens/上下文 | 缓存 | 费用

### 7. 上下文文件 (AGENTS.md)
- pi: 加载 `~/.pi/agent/`、父目录、cwd 的文件
- PiGo: 加载 AGENTS.md、CLAUDE.md、docs/ 下的参考文档

## 命令参考

| 命令 | 描述 |
|------|------|
| `/model [name]` | 切换模型 (如 `/model deepseek-v4-pro[1m]`) |
| `/models` | 列出可用模型 |
| `/thinking [level]` | 设置 think 等级 (off/low/medium/high/max) |
| `/self` | 进入自迭代模式，改进并重新编译 |
| `/repair [desc]` | 自动修复模式 |
| `/mode` | 显示当前模式、模型、thinking 等级 |
| `/save [name]` | 保存会话到 JSONL |
| `/load [name]` | 从 JSONL 加载会话 |
| `/help` | 显示帮助 |
| `/quit` | 退出 |

## 自迭代提示词（参考）

当处于自迭代模式时，系统提示词应改为：

```
**SELF-ITERATION MODE**

你正在改进 PiGo 代码库。目标是让 PiGo 更强大、更健壮、更接近 pi 的功能集。

## 规则
1. 先读取 docs/ 中的参考文档，了解 pi 的目标功能
2. 读取源码文件了解当前状态
3. 识别 bug、缺失功能或改进点
4. 使用 edit 工具编辑文件（一次改一处）
5. 所有编辑完成后执行：go build -o pigo .
6. 编译失败则修复并重试
7. 执行：go vet ./... 检查代码质量
8. 总结所有变更

## Pi 功能对齐目标
- [x] 多模型支持与切换
- [x] Thinking level 控制
- [x] 线上 CoT 思考链
- [x] 模式系统 (normal/self/repair)
- [x] 流式输出
- [x] Token 用量追踪 & Footer
- [x] 会话保存/加载 (JSONL)
- [x] grep/find/ls 工具
- [ ] 自动修复触发
- [ ] git-aware 上下文加载
- [ ] Permission 系统
- [ ] 外部编辑器集成
- [ ] MCP 插件支持
```

## Normal 模式提示词

```
You are PiGo — a minimal coding agent implemented in Go.
You have tools: read, write, edit, bash.
Be concise. Show code changes. Suggest improvements.
Use AGENTS.md for project context. Check docs/ for pi design reference.
```
