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
  <a href="https://docs.goclaw.sh">เอกสาร</a> •
  <a href="https://docs.goclaw.sh/#quick-start">เริ่มต้นอย่างรวดเร็ว</a> •
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

**GoClaw** คือ AI gateway แบบ multi-agent ที่เชื่อมต่อ LLM เข้ากับเครื่องมือ ช่องทางสื่อสาร และข้อมูลของคุณ — ติดตั้งเป็น Go binary ไฟล์เดียวโดยไม่มี runtime dependency ใดๆ รองรับการประสานงาน agent teams และการส่งต่องานระหว่าง agent ผ่านผู้ให้บริการ LLM มากกว่า 20 รายพร้อมการแยกข้อมูลแบบ multi-tenant อย่างสมบูรณ์

เป็น Go port ของ [OpenClaw](https://github.com/openclaw/openclaw) ที่เสริมด้วยความปลอดภัยขั้นสูง, PostgreSQL แบบ multi-tenant และความสามารถด้าน observability ระดับ production

🌐 **ภาษา:**
[🇺🇸 English](../README.md) ·
[🇨🇳 简体中文](README.zh-CN.md) ·
[🇯🇵 日本語](README.ja.md) ·
[🇰🇷 한국어](README.ko.md) ·
[🇻🇳 Tiếng Việt](README.vi.md) ·
[🇪🇸 Español](README.es.md) ·
[🇧🇷 Português](README.pt.md) ·
[🇩🇪 Deutsch](README.de.md) ·
[🇫🇷 Français](README.fr.md) ·
[🇷🇺 Русский](README.ru.md)
[🇹🇭 ไทย](README.th.md) ·

## จุดเด่นที่แตกต่าง

- **Agent Teams และการประสานงาน** — Teams ที่มี task board ร่วมกัน, การส่งต่องานระหว่าง agent (แบบ sync/async) และการค้นหา agent แบบผสม
- **PostgreSQL แบบ Multi-Tenant** — workspace แยกต่อผู้ใช้, ไฟล์ context แยกต่อผู้ใช้, API key เข้ารหัสด้วย AES-256-GCM และ session ที่แยกจากกัน
- **Single Binary** — Go binary แบบ static ขนาดประมาณ 25 MB ไม่ต้องใช้ Node.js runtime เริ่มทำงานภายใน <1 วินาที รันบน VPS ราคา $5 ได้
- **ความปลอดภัยระดับ Production** — ระบบสิทธิ์ 5 ชั้น (gateway auth → global tool policy → per-agent → per-channel → owner-only) พร้อม rate limiting, การตรวจจับ prompt injection, การป้องกัน SSRF, shell deny patterns และการเข้ารหัส AES-256-GCM
- **ผู้ให้บริการ LLM มากกว่า 20 ราย** — Anthropic (native HTTP+SSE พร้อม prompt caching), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP และ endpoint ที่เข้ากันได้กับ OpenAI ทุกรูปแบบ
- **7 ช่องทางสื่อสาร** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — โหมดการคิดแบบขยายต่อผู้ให้บริการ (Anthropic budget tokens, OpenAI reasoning effort, DashScope thinking budget) พร้อมรองรับ streaming
- **Heartbeat** — การตรวจสอบ agent เป็นระยะด้วย HEARTBEAT.md checklists พร้อม suppress-on-OK, กำหนดชั่วโมงทำงาน, logic การลองใหม่ และการส่งผ่านช่องทาง
- **การตั้งเวลาและ Cron** — คำสั่ง `at`, `every` และ cron expressions สำหรับงาน agent อัตโนมัติพร้อม lane-based concurrency
- **Observability** — การติดตาม LLM call แบบ built-in ด้วย spans และ prompt cache metrics พร้อมรองรับการส่งออก OpenTelemetry OTLP แบบ optional

## Claw Ecosystem

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| ภาษา            | TypeScript      | Rust     | Go       | **Go**                                  |
| ขนาด binary     | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Docker image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (ขณะ idle)  | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| เวลาเริ่มทำงาน  | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| ฮาร์ดแวร์เป้าหมาย | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| ฟีเจอร์                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| การรวม MCP                 | — (ใช้ ACP)                          | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Agent teams                | —                                    | —                                            | —                                     | ✅ Task board + mailbox        |
| การเสริมความปลอดภัย        | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | พื้นฐาน (workspace restrict, exec deny) | ✅ การป้องกัน 5 ชั้น          |
| OTel observability         | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| Prompt caching             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Knowledge graph            | —                                    | —                                            | —                                     | ✅ LLM extraction + traversal  |
| ระบบ skill                 | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ พื้นฐาน                            | ✅ BM25 + pgvector hybrid      |
| Lane-based scheduler       | ✅                                   | Bounded concurrency                          | —                                     | ✅ (main/subagent/team/cron)   |
| ช่องทางสื่อสาร             | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Companion apps             | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web dashboard                  |
| Live Canvas / เสียง        | ✅ (A2UI + TTS/STT)                  | —                                            | Voice transcription                   | TTS (4 ผู้ให้บริการ)           |
| ผู้ให้บริการ LLM           | 10+                                  | 8 native + 29 compat                         | 13+                                   | **20+**                        |
| workspace ต่อผู้ใช้        | ✅ (file-based)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| Encrypted secrets          | — (env vars เท่านั้น)               | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM ใน DB           |

## สถาปัตยกรรม

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## เริ่มต้นอย่างรวดเร็ว

**ข้อกำหนดเบื้องต้น:** Go 1.26+, PostgreSQL 18 พร้อม pgvector, Docker (optional)

### จาก Source Code

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # ตัวช่วยตั้งค่าแบบ interactive
source .env.local && ./goclaw
```

### ด้วย Docker

```bash
# สร้างไฟล์ .env พร้อม secrets ที่สร้างอัตโนมัติ
chmod +x prepare-env.sh && ./prepare-env.sh

# เพิ่ม GOCLAW_*_API_KEY อย่างน้อยหนึ่งรายการใน .env จากนั้น:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Web Dashboard ที่ http://localhost:3000
# ตรวจสอบสถานะ: curl http://localhost:18790/health
```

เมื่อตั้งค่า environment variables `GOCLAW_*_API_KEY` แล้ว gateway จะทำการ onboard อัตโนมัติโดยไม่ต้องป้อนข้อมูลแบบ interactive — ตรวจจับผู้ให้บริการ รัน migrations และสร้างข้อมูลเริ่มต้น

> สำหรับ build variants (OTel, Tailscale, Redis), Docker image tags และ compose overlays ดูที่ [Deployment Guide](https://docs.goclaw.sh/#deploy-docker-compose)

## การประสานงาน Multi-Agent

GoClaw รองรับ agent teams และการส่งต่องานระหว่าง agent — แต่ละ agent ทำงานด้วย identity, เครื่องมือ, ผู้ให้บริการ LLM และไฟล์ context ของตัวเอง

### การส่งต่องานระหว่าง Agent

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| โหมด | วิธีทำงาน | เหมาะสำหรับ |
|------|-------------|----------|
| **Sync** | Agent A ถาม Agent B และ**รอ**คำตอบ | การค้นหาข้อมูลด่วน, การตรวจสอบข้อเท็จจริง |
| **Async** | Agent A ถาม Agent B และ**ทำงานต่อ** ส่วน B แจ้งผลลัพธ์ทีหลัง | งานระยะยาว, รายงาน, การวิเคราะห์เชิงลึก |

Agent สื่อสารผ่าน **permission links** แบบชัดเจนพร้อมการควบคุมทิศทาง (`outbound`, `inbound`, `bidirectional`) และการจำกัด concurrency ทั้งในระดับต่อ link และต่อ agent

### Agent Teams

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Shared task board** — สร้าง, รับ, เสร็จสิ้น, ค้นหา task พร้อมการพึ่งพา `blocked_by`
- **Team mailbox** — การส่งข้อความแบบ peer-to-peer และการ broadcast
- **เครื่องมือ**: `team_tasks` สำหรับจัดการ task, `team_message` สำหรับ mailbox

> สำหรับรายละเอียดการส่งต่องาน, permission links และการควบคุม concurrency ดูที่ [เอกสาร Agent Teams](https://docs.goclaw.sh/#teams-what-are-teams)

## เครื่องมือ Built-in

| เครื่องมือ         | กลุ่ม         | คำอธิบาย                                                     |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | อ่านเนื้อหาไฟล์ (พร้อม virtual FS routing)                   |
| `write_file`       | fs            | เขียน/สร้างไฟล์                                              |
| `edit_file`        | fs            | แก้ไขเฉพาะส่วนของไฟล์ที่มีอยู่                              |
| `list_files`       | fs            | แสดงรายการเนื้อหาในไดเรกทอรี                                 |
| `search`           | fs            | ค้นหาเนื้อหาไฟล์ตามรูปแบบ                                   |
| `glob`             | fs            | ค้นหาไฟล์ด้วย glob pattern                                   |
| `exec`             | runtime       | รันคำสั่ง shell (พร้อม approval workflow)                    |
| `web_search`       | web           | ค้นหาเว็บ (Brave, DuckDuckGo)                                |
| `web_fetch`        | web           | ดึงและแยกวิเคราะห์เนื้อหาเว็บ                               |
| `memory_search`    | memory        | ค้นหา long-term memory (FTS + vector)                        |
| `memory_get`       | memory        | ดึงข้อมูล memory entries                                      |
| `skill_search`     | —             | ค้นหา skills (BM25 + embedding hybrid)                       |
| `knowledge_graph_search` | memory  | ค้นหา entities และ traverse ความสัมพันธ์ใน knowledge graph   |
| `create_image`     | media         | สร้างรูปภาพ (DashScope, MiniMax)                             |
| `create_audio`     | media         | สร้างเสียง (OpenAI, ElevenLabs, MiniMax, Suno)               |
| `create_video`     | media         | สร้างวิดีโอ (MiniMax, Veo)                                   |
| `read_document`    | media         | อ่านเอกสาร (Gemini File API, provider chain)                 |
| `read_image`       | media         | วิเคราะห์รูปภาพ                                              |
| `read_audio`       | media         | ถอดความและวิเคราะห์เสียง                                     |
| `read_video`       | media         | วิเคราะห์วิดีโอ                                              |
| `message`          | messaging     | ส่งข้อความไปยังช่องทาง                                       |
| `tts`              | —             | สังเคราะห์ Text-to-Speech                                    |
| `spawn`            | —             | สร้าง subagent                                               |
| `subagents`        | sessions      | ควบคุม subagent ที่กำลังทำงาน                                |
| `team_tasks`       | teams         | Shared task board (list, create, claim, complete, search)    |
| `team_message`     | teams         | Team mailbox (send, broadcast, read)                         |
| `sessions_list`    | sessions      | แสดงรายการ session ที่ใช้งานอยู่                             |
| `sessions_history` | sessions      | ดูประวัติ session                                            |
| `sessions_send`    | sessions      | ส่งข้อความไปยัง session                                      |
| `sessions_spawn`   | sessions      | สร้าง session ใหม่                                           |
| `session_status`   | sessions      | ตรวจสอบสถานะ session                                         |
| `cron`             | automation    | กำหนดเวลาและจัดการ cron jobs                                 |
| `gateway`          | automation    | การดูแลระบบ gateway                                          |
| `browser`          | ui            | Browser automation (navigate, click, type, screenshot)       |
| `announce_queue`   | automation    | การประกาศผลลัพธ์แบบ async (สำหรับการส่งต่องานแบบ async)      |

## เอกสาร

เอกสารฉบับสมบูรณ์ที่ **[docs.goclaw.sh](https://docs.goclaw.sh)** — หรือดู source ที่ [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| หัวข้อ | เนื้อหา |
|---------|--------|
| [เริ่มต้นใช้งาน](https://docs.goclaw.sh/#what-is-goclaw) | การติดตั้ง, เริ่มต้นอย่างรวดเร็ว, การตั้งค่า, ทัวร์ Web Dashboard |
| [แนวคิดหลัก](https://docs.goclaw.sh/#how-goclaw-works) | Agent Loop, Sessions, เครื่องมือ, Memory, Multi-Tenancy |
| [Agents](https://docs.goclaw.sh/#creating-agents) | การสร้าง Agent, ไฟล์ Context, บุคลิกภาพ, การแชร์และการเข้าถึง |
| [Providers](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 เพิ่มเติม |
| [ช่องทาง](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Agent Teams](https://docs.goclaw.sh/#teams-what-are-teams) | Teams, Task Board, การส่งข้อความ, การส่งต่องานและ Handoff |
| [ขั้นสูง](https://docs.goclaw.sh/#custom-tools) | เครื่องมือกำหนดเอง, MCP, Skills, Cron, Sandbox, Hooks, RBAC |
| [การ Deploy](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, ฐานข้อมูล, ความปลอดภัย, Observability, Tailscale |
| [อ้างอิง](https://docs.goclaw.sh/#cli-commands) | คำสั่ง CLI, REST API, WebSocket Protocol, Environment Variables |

## การทดสอบ

```bash
go test ./...                                    # Unit tests
go test -v ./tests/integration/ -timeout 120s    # Integration tests (ต้องมี gateway ที่กำลังทำงาน)
```

## สถานะโครงการ

ดู [CHANGELOG.md](CHANGELOG.md) สำหรับสถานะฟีเจอร์โดยละเอียด รวมถึงสิ่งที่ได้รับการทดสอบใน production แล้วและสิ่งที่ยังอยู่ระหว่างดำเนินการ

## ขอบคุณ

GoClaw สร้างขึ้นจากโครงการ [OpenClaw](https://github.com/openclaw/openclaw) ต้นฉบับ เราขอขอบคุณสถาปัตยกรรมและวิสัยทัศน์ที่เป็นแรงบันดาลใจในการ port มาเป็น Go

## สัญญาอนุญาต

MIT
