<p align="center">
  <img src="../_statics/goclaw.png" alt="GoClaw" />
</p>

<h1 align="center">GoClaw</h1>

<p align="center"><strong>Enterprise AI Agent Platform</strong></p>

<p align="center">
Multi-agent AI gateway built in Go. 20+ LLM providers. 7 channels. Multi-tenant PostgreSQL.<br/>
Single binary. Production-tested. Agents that orchestrate for you.
</p>

<p align="center">
  <a href="https://docs.goclaw.sh">文档</a> •
  <a href="https://docs.goclaw.sh/#quick-start">快速开始</a> •
  <a href="https://x.com/nlb_io">Twitter / X</a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go_1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go" /></a>
  <a href="https://www.postgresql.org/"><img src="https://img.shields.io/badge/PostgreSQL_18-316192?style=flat-square&logo=postgresql&logoColor=white" alt="PostgreSQL" /></a>
  <a href="https://www.docker.com/"><img src="https://img.shields.io/badge/Docker-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker" /></a>
  <a href="https://developer.mozilla.org/en-US/docs/Web/API/WebSocket"><img src="https://img.shields.io/badge/WebSocket-010101?style=flat-square&logo=socket.io&logoColor=white" alt="WebSocket" /></a>
  <a href="https://opentelemetry.io/"><img src="https://img.shields.io/badge/OpenTelemetry-000000?style=flat-square&logo=opentelemetry&logoColor=white" alt="OpenTelemetry" /></a>
  <a href="https://www.anthropic.com/"><img src="https://img.shields.io/badge/Anthropic-191919?style=flat-square&logo=anthropic&logoColor=white" alt="Anthropic" /></a>
  <a href="https://openai.com/"><img src="https://img.shields.io/badge/OpenAI_Compatible-412991?style=flat-square&logo=openai&logoColor=white" alt="OpenAI" /></a>
  <img src="https://img.shields.io/badge/License-MIT-yellow?style=flat-square" alt="License: MIT" />
</p>

**GoClaw** 是一个多智能体 AI 网关，将大语言模型连接到你的工具、渠道和数据 —— 以单个 Go 二进制文件部署，零运行时依赖。它跨 20 多个大语言模型提供商编排智能体团队和跨智能体委托，并提供完整的多租户隔离。

这是 [OpenClaw](https://github.com/openclaw/openclaw) 的 Go 移植版本，具备增强的安全性、多租户 PostgreSQL 支持以及生产级可观测性。

🌐 **语言：**
[🇺🇸 English](../README.md) ·
[🇨🇳 简体中文](README.zh-CN.md) ·
[🇯🇵 日本語](README.ja.md) ·
[🇰🇷 한국어](README.ko.md) ·
[🇻🇳 Tiếng Việt](README.vi.md) ·
[🇪🇸 Español](README.es.md) ·
[🇧🇷 Português](README.pt.md) ·
[🇫🇷 Français](README.fr.md) ·
[🇩🇪 Deutsch](README.de.md) ·
[🇷🇺 Русский](README.ru.md)
## 与众不同之处

- **智能体团队与编排** — 具有共享任务板的团队、跨智能体委托（同步/异步）以及混合智能体发现
- **多租户 PostgreSQL** — 每用户工作空间、每用户上下文文件、加密 API 密钥（AES-256-GCM）、隔离的会话
- **单一二进制文件** — 约 25 MB 静态 Go 二进制文件，无 Node.js 运行时，启动时间 <1 秒，可在 5 美元 VPS 上运行
- **生产级安全** — 5 层权限体系（网关认证 → 全局工具策略 → 每智能体 → 每渠道 → 仅限所有者），外加速率限制、提示词注入检测、SSRF 防护、Shell 拒绝模式和 AES-256-GCM 加密
- **20 多个大语言模型提供商** — Anthropic（原生 HTTP+SSE，支持提示词缓存）、OpenAI、OpenRouter、Groq、DeepSeek、Gemini、Mistral、xAI、MiniMax、Cohere、Perplexity、DashScope、百炼、Zai、Ollama、Ollama Cloud、Claude CLI、Codex、ACP，以及任何 OpenAI 兼容端点
- **7 个消息渠道** — Telegram、Discord、Slack、Zalo OA、Zalo Personal、飞书/Lark、WhatsApp
- **Extended Thinking** — 每提供商思考模式（Anthropic 预算 token、OpenAI 推理效果、DashScope 思考预算），支持流式传输
- **Heartbeat** — 通过 HEARTBEAT.md 检查清单进行定期智能体签到，支持正常时静默、活跃时段、重试逻辑和渠道投递
- **调度与定时任务** — `at`、`every` 和 cron 表达式用于自动化智能体任务，支持基于通道的并发
- **可观测性** — 内置大语言模型调用追踪（含 span 和提示词缓存指标），可选 OpenTelemetry OTLP 导出

## Claw 生态系统

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| 语言            | TypeScript      | Rust     | Go       | **Go**                                  |
| 二进制大小      | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB**（基础）/ **~36 MB**（含 OTel） |
| Docker 镜像     | —               | —        | —        | **~50 MB**（Alpine）                    |
| 内存占用（空闲）| > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| 启动时间        | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| 目标硬件        | $599+ Mac Mini  | $10 边缘设备 | $10 边缘设备 | **$5 VPS+**                         |

| 功能特性                   | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| 多租户（PostgreSQL）       | —                                    | —                                            | —                                     | ✅                             |
| MCP 集成                   | —（使用 ACP）                        | —                                            | —                                     | ✅（stdio/SSE/streamable-http）|
| 智能体团队                 | —                                    | —                                            | —                                     | ✅ 任务板 + 邮箱               |
| 安全加固                   | ✅（SSRF、路径遍历、注入）           | ✅（沙箱、速率限制、注入、配对）             | 基础（工作区限制、exec 拒绝）         | ✅ 5 层防御                    |
| OTel 可观测性              | ✅（可选扩展）                       | ✅（Prometheus + OTLP）                      | —                                     | ✅ OTLP（可选构建标签）        |
| 提示词缓存                 | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI 兼容     |
| 知识图谱                   | —                                    | —                                            | —                                     | ✅ 大语言模型提取 + 遍历       |
| 技能系统                   | ✅ 嵌入/语义                         | ✅ SKILL.md + TOML                           | ✅ 基础                               | ✅ BM25 + pgvector 混合        |
| 基于通道的调度器           | ✅                                   | 有界并发                                     | —                                     | ✅（main/subagent/team/cron）  |
| 消息渠道                   | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| 伴侣应用                   | macOS、iOS、Android                  | Python SDK                                   | —                                     | Web 控制台                     |
| 实时画布 / 语音            | ✅（A2UI + TTS/STT）                 | —                                            | 语音转录                              | TTS（4 个提供商）              |
| 大语言模型提供商           | 10+                                  | 8 原生 + 29 兼容                             | 13+                                   | **20+**                        |
| 每用户工作空间             | ✅（基于文件）                       | —                                            | —                                     | ✅（PostgreSQL）               |
| 加密密钥                   | —（仅环境变量）                      | ✅ ChaCha20-Poly1305                         | —（明文 JSON）                        | ✅ 数据库中 AES-256-GCM        |

## 架构

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## 快速开始

**前置条件：** Go 1.26+、PostgreSQL 18（含 pgvector）、Docker（可选）

### 从源码构建

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # 交互式设置向导
source .env.local && ./goclaw
```

### 使用 Docker

```bash
# 生成包含自动生成密钥的 .env 文件
chmod +x prepare-env.sh && ./prepare-env.sh

# 在 .env 中至少添加一个 GOCLAW_*_API_KEY，然后：
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Web 控制台地址：http://localhost:3000
# 健康检查：curl http://localhost:18790/health
```

当设置了 `GOCLAW_*_API_KEY` 环境变量时，网关会自动完成初始化，无需交互提示 —— 自动检测提供商、运行数据库迁移并填充默认数据。

> 有关构建变体（OTel、Tailscale、Redis）、Docker 镜像标签和 compose 覆盖文件，请参阅[部署指南](https://docs.goclaw.sh/#deploy-docker-compose)。

## 多智能体编排

GoClaw 支持智能体团队和跨智能体委托 —— 每个智能体以其自身的身份、工具、大语言模型提供商和上下文文件运行。

### 智能体委托

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| 模式 | 工作方式 | 适用场景 |
|------|----------|----------|
| **同步** | 智能体 A 向智能体 B 发起请求并**等待**回答 | 快速查询、事实核查 |
| **异步** | 智能体 A 向智能体 B 发起请求后**继续执行**，B 完成后再通知 | 长时间任务、报告生成、深度分析 |

智能体通过明确的**权限链接**进行通信，支持方向控制（`outbound`、`inbound`、`bidirectional`）以及链接级和智能体级的并发限制。

### 智能体团队

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **共享任务板** — 创建、认领、完成、搜索任务，支持 `blocked_by` 依赖关系
- **团队邮箱** — 点对点直接消息和广播
- **工具**：`team_tasks` 用于任务管理，`team_message` 用于邮箱

> 有关委托详情、权限链接和并发控制，请参阅[智能体团队文档](https://docs.goclaw.sh/#teams-what-are-teams)。

## 内置工具

| 工具               | 分组          | 描述                                                         |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | 读取文件内容（支持虚拟文件系统路由）                         |
| `write_file`       | fs            | 写入/创建文件                                                |
| `edit_file`        | fs            | 对现有文件进行精确编辑                                       |
| `list_files`       | fs            | 列出目录内容                                                 |
| `search`           | fs            | 按模式搜索文件内容                                           |
| `glob`             | fs            | 按 glob 模式查找文件                                         |
| `exec`             | runtime       | 执行 Shell 命令（含审批流程）                                |
| `web_search`       | web           | 网络搜索（Brave、DuckDuckGo）                                |
| `web_fetch`        | web           | 抓取并解析网页内容                                           |
| `memory_search`    | memory        | 搜索长期记忆（全文检索 + 向量）                              |
| `memory_get`       | memory        | 获取记忆条目                                                 |
| `skill_search`     | —             | 搜索技能（BM25 + 嵌入混合）                                  |
| `knowledge_graph_search` | memory  | 搜索实体并遍历知识图谱关系                                   |
| `create_image`     | media         | 图像生成（DashScope、MiniMax）                               |
| `create_audio`     | media         | 音频生成（OpenAI、ElevenLabs、MiniMax、Suno）                |
| `create_video`     | media         | 视频生成（MiniMax、Veo）                                     |
| `read_document`    | media         | 文档读取（Gemini File API、提供商链）                        |
| `read_image`       | media         | 图像分析                                                     |
| `read_audio`       | media         | 音频转录与分析                                               |
| `read_video`       | media         | 视频分析                                                     |
| `message`          | messaging     | 向渠道发送消息                                               |
| `tts`              | —             | 文字转语音合成                                               |
| `spawn`            | —             | 生成子智能体                                                 |
| `subagents`        | sessions      | 控制运行中的子智能体                                         |
| `team_tasks`       | teams         | 共享任务板（列出、创建、认领、完成、搜索）                   |
| `team_message`     | teams         | 团队邮箱（发送、广播、读取）                                 |
| `sessions_list`    | sessions      | 列出活跃会话                                                 |
| `sessions_history` | sessions      | 查看会话历史                                                 |
| `sessions_send`    | sessions      | 向会话发送消息                                               |
| `sessions_spawn`   | sessions      | 生成新会话                                                   |
| `session_status`   | sessions      | 检查会话状态                                                 |
| `cron`             | automation    | 调度和管理定时任务                                           |
| `gateway`          | automation    | 网关管理                                                     |
| `browser`          | ui            | 浏览器自动化（导航、点击、输入、截图）                       |
| `announce_queue`   | automation    | 异步结果通知（用于异步委托）                                 |

## 文档

完整文档请访问 **[docs.goclaw.sh](https://docs.goclaw.sh)** —— 或在 [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs) 中浏览源码。

| 章节 | 主题 |
|------|------|
| [快速开始](https://docs.goclaw.sh/#what-is-goclaw) | 安装、快速启动、配置、Web 控制台导览 |
| [核心概念](https://docs.goclaw.sh/#how-goclaw-works) | 智能体循环、会话、工具、记忆、多租户 |
| [智能体](https://docs.goclaw.sh/#creating-agents) | 创建智能体、上下文文件、人格设定、共享与访问 |
| [提供商](https://docs.goclaw.sh/#providers-overview) | Anthropic、OpenAI、OpenRouter、Gemini、DeepSeek，以及 15+ 个其他提供商 |
| [渠道](https://docs.goclaw.sh/#channels-overview) | Telegram、Discord、Slack、飞书、Zalo、WhatsApp、WebSocket |
| [智能体团队](https://docs.goclaw.sh/#teams-what-are-teams) | 团队、任务板、消息传递、委托与交接 |
| [高级功能](https://docs.goclaw.sh/#custom-tools) | 自定义工具、MCP、技能、定时任务、沙箱、钩子、RBAC |
| [部署](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose、数据库、安全、可观测性、Tailscale |
| [参考](https://docs.goclaw.sh/#cli-commands) | CLI 命令、REST API、WebSocket 协议、环境变量 |

## 测试

```bash
go test ./...                                    # 单元测试
go test -v ./tests/integration/ -timeout 120s    # 集成测试（需要正在运行的网关）
```

## 项目状态

有关详细功能状态（包括已在生产环境测试的内容和仍在进行中的内容），请参阅 [CHANGELOG.md](CHANGELOG.md)。

## 致谢

GoClaw 基于原始的 [OpenClaw](https://github.com/openclaw/openclaw) 项目构建。我们对启发这个 Go 移植版本的架构设计和愿景深表感谢。

## 许可证

MIT
