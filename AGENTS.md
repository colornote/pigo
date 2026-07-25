# PiGo — pi in Go

## pi 参考文档（必读）

自迭代时务必先查阅 `docs/` 中的参考文档：

| 文档 | 内容 |
|------|------|
| `docs/pi-readme.md` | pi 完整功能文档 (707 行)，包含所有 CLI 选项、模型选择、工具系统、权限系统、插件、MCP 等 |
| `docs/pi-design.md` | PiGo 设计蓝图，功能对齐清单，架构概览 |

遇到 pi 功能相关问题时，先 `read docs/pi-readme.md` 查找对应章节。

## 架构

```
pigo/
├── main.go              # CLI / 命令分发
├── config/config.go     # 配置 + .env 加载
├── llm/client.go        # DeepSeek API (Anthropic 兼容)
├── llm/deepseek.go      # DeepSeek 原生 API (CoT/推理)
├── llm/usage.go         # Token 用量 & 定价
├── tools/tools.go       # read / write / edit / bash
├── agent/loop.go        # 核心循环 + Footer
├── agent/modes.go       # 模式系统 / 模型注册 / 提示词
├── docs/pi-readme.md    # pi 完整文档 (参考)
├── docs/pi-design.md    # PiGo 设计蓝图 (参考)
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
| `/repair <desc>` | 自动修复 |
| `/help` | 帮助 |
| `/quit` | 退出 |
