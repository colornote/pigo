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

## 构建 / 测试（本机 受限运行环境 ⇄ build-host 交叉编译）

受限运行环境 上编译极慢且默认参数会被杀，**推荐在 build-host 上交叉编译**（一键脚本
`./scripts/remote-build.sh`）：

```sh
sh ./scripts/remote-build.sh             # 构建 + 拉回 + 冒烟测试
sh ./scripts/remote-build.sh --test      # 追加 go vet ./... + go test ./...
sh ./scripts/remote-build.sh --install   # 追加原子安装（先备份）
```

- 目标 `linux/386`，必须用 **Go 1.22.12**——目标机自带的 1.23.0 编出的二进制在
  受限运行环境 上会 SIGSYS（Go 1.23 启进程调 `pidfd_open`，受限运行环境 未实现）
- `--test` 在 构建机上跑（原生 amd64，几秒出结果）；受限运行环境 上原生编译仅作后备
- 细节、手动步骤与踩坑见全局 `~/.pigo/AGENTS.md`

## Packages / 扩展

pigo 启动和 `/reload` 时自动扫描 `packages/`（项目级）与 `~/.pigo/packages/`
（全局），把可用扩展的名称 + 描述注入系统提示词的 `## Packages` 段。
你不需要手动记忆清单——每次会话的系统提示词里就有。

发现可用的 package 时：

1. 先用 `read packages/<name>/SKILL.md` 读取完整用法（渐进式披露）
2. 按 SKILL.md 的说明调用对应的 CLI / 脚本
3. 扩展包是独立能力（不依赖 pigo 核心），不要修改其源码，除非任务明确要求
4. 截图类输出（如 `browser shot`）→ 保存为图片后用 `read` 查看；若主模型不支持图像，切换多模态模型（`deepseek-v4-flash-vision-exp` / `mimo-v2.5`）再分析

当前已有：`packages/browser/` — CDP Chrome 自动化（open/tabs/eval/click/type/text/shot）。

