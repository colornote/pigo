# PiGo — pi in Go

## pi 参考文档

`docs/pi-readme.md` — pi 完整文档，包含所有核心功能设计。
自迭代时务必参考此文档，逐步对齐 pi 的功能。

## 架构

```
pigo/
├── main.go              # CLI / 命令分发
├── config/config.go     # 配置 + .env 加载
├── llm/client.go        # DeepSeek API (Anthropic 兼容)
├── tools/tools.go       # read / write / edit / bash
├── agent/loop.go        # 核心循环
├── agent/modes.go       # 模式系统 / 模型注册
├── docs/pi-readme.md    # pi 完整文档 (参考)
└── pigo                 # 二进制
```

## 命令

| 命令 | 功能 |
|------|------|
| `/model <name>` | 切换模型 |
| `/models` | 列出可用模型 |
| `/thinking <lvl>` | off/low/medium/high/max |
| `/mode` | 显示当前状态 |
| `/self` | 自迭代改进 |
| `/repair <desc>` | 自动修复 bug |
| `/help` | 帮助 |
| `/quit` | 退出 |

## 三种模式

| 模式 | 触发 | 提示词 |
|------|------|------|
| Normal | 默认 | 编码助手 |
| Self-Iterate | `/self` | 读源码→改进→go build |
| Auto-Repair | `/repair` | 诊断→修复→重建 |

## .env 配置

```bash
DEEPSEEK_API_KEY=sk-xxx
PIGO_MODEL=deepseek-v4-flash
PIGO_THINKING=medium
PIGO_BASE_URL=https://api.deepseek.com/anthropic
PIGO_MAX_TURNS=50
```

加载顺序: shell 环境 → `~/.pigo/.env` → `./.env`（后者覆盖前者）
