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
  <a href="https://docs.goclaw.sh">دستاویزات</a> •
  <a href="https://docs.goclaw.sh/#quick-start">فوری آغاز</a> •
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

**GoClaw** ایک ملٹی-ایجنٹ AI گیٹ وے ہے جو LLMs کو آپ کے ٹولز، چینلز، اور ڈیٹا سے جوڑتا ہے — ایک واحد Go بائنری کے طور پر بغیر کسی رن ٹائم انحصار کے تعینات کیا جاتا ہے۔ یہ 20+ LLM فراہم کنندگان پر ایجنٹ ٹیموں اور انٹر-ایجنٹ ڈیلیگیشن کو مکمل ملٹی-ٹیننٹ آئسولیشن کے ساتھ ترتیب دیتا ہے۔

[OpenClaw](https://github.com/openclaw/openclaw) کا Go پورٹ جس میں بہتر سیکیورٹی، ملٹی-ٹیننٹ PostgreSQL، اور پروڈکشن گریڈ آبزرویبلٹی شامل ہے۔

🌐 **زبانیں:**
[🇺🇸 English](../README.md) ·
[🇨🇳 简体中文](README.zh-CN.md) ·
[🇯🇵 日本語](README.ja.md) ·
[🇰🇷 한국어](README.ko.md) ·
[🇻🇳 Tiếng Việt](README.vi.md) ·
[🇵🇭 Tagalog](README.tl.md) ·
[🇪🇸 Español](README.es.md) ·
[🇧🇷 Português](README.pt.md) ·
[🇮🇹 Italiano](README.it.md) ·
[🇩🇪 Deutsch](README.de.md) ·
[🇫🇷 Français](README.fr.md) ·
[🇸🇦 العربية](README.ar.md) ·
[🇮🇳 हिन्दी](README.hi.md) ·
[🇷🇺 Русский](README.ru.md) ·
[🇧🇩 বাংলা](README.bn.md) ·
[🇮🇱 עברית](README.he.md) ·
[🇵🇱 Polski](README.pl.md) ·
[🇨🇿 Čeština](README.cs.md) ·
[🇳🇱 Nederlands](README.nl.md) ·
[🇹🇷 Türkçe](README.tr.md) ·
[🇺🇦 Українська](README.uk.md) ·
[🇮🇩 Bahasa Indonesia](README.id.md) ·
[🇹🇭 ไทย](README.th.md) ·
[🇵🇰 اردو](README.ur.md) ·
[🇷🇴 Română](README.ro.md) ·
[🇸🇪 Svenska](README.sv.md) ·
[🇬🇷 Ελληνικά](README.el.md) ·
[🇭🇺 Magyar](README.hu.md) ·
[🇫🇮 Suomi](README.fi.md) ·
[🇩🇰 Dansk](README.da.md) ·
[🇳🇴 Norsk](README.nb.md)

## یہ مختلف کیوں ہے

- **ایجنٹ ٹیمیں اور آرکیسٹریشن** — مشترکہ ٹاسک بورڈز، انٹر-ایجنٹ ڈیلیگیشن (sync/async)، اور ہائبرڈ ایجنٹ ڈسکوری کے ساتھ ٹیمیں
- **ملٹی-ٹیننٹ PostgreSQL** — فی-صارف ورک اسپیسز، فی-صارف کانٹیکسٹ فائلیں، انکرپٹڈ API کیز (AES-256-GCM)، آئسولیٹڈ سیشنز
- **واحد بائنری** — ~25 MB اسٹیٹک Go بائنری، کوئی Node.js رن ٹائم نہیں، <1s اسٹارٹ اپ، $5 VPS پر چلتی ہے
- **پروڈکشن سیکیورٹی** — 5-پرت پرمیشن سسٹم (gateway auth → گلوبل ٹول پالیسی → فی-ایجنٹ → فی-چینل → صرف مالک) نیز ریٹ لمٹنگ، پرامپٹ انجیکشن ڈیٹیکشن، SSRF پروٹیکشن، شیل ڈینائی پیٹرنز، اور AES-256-GCM انکرپشن
- **20+ LLM فراہم کنندگان** — Anthropic (native HTTP+SSE پرامپٹ کیشنگ کے ساتھ)، OpenAI، OpenRouter، Groq، DeepSeek، Gemini، Mistral، xAI، MiniMax، Cohere، Perplexity، DashScope، Bailian، Zai، Ollama، Ollama Cloud، Claude CLI، Codex، ACP، اور کوئی بھی OpenAI-compatible اینڈپوائنٹ
- **7 میسجنگ چینلز** — Telegram، Discord، Slack، Zalo OA، Zalo Personal، Feishu/Lark، WhatsApp
- **Extended Thinking** — فی-فراہم کنندہ تھنکنگ موڈ (Anthropic بجٹ ٹوکنز، OpenAI reasoning effort، DashScope تھنکنگ بجٹ) اسٹریمنگ سپورٹ کے ساتھ
- **Heartbeat** — HEARTBEAT.md چیک لسٹس کے ذریعے وقتاً فوقتاً ایجنٹ چیک-انز، suppress-on-OK، فعال اوقات، ری-ٹرائی لاجک، اور چینل ڈیلیوری کے ساتھ
- **شیڈیولنگ اور Cron** — لین-بیسڈ کنکرنسی کے ساتھ خودکار ایجنٹ ٹاسکس کے لیے `at`، `every`، اور cron ایکسپریشنز
- **آبزرویبلٹی** — spans اور پرامپٹ کیش میٹرکس کے ساتھ بلٹ-ان LLM کال ٹریسنگ، اختیاری OpenTelemetry OTLP برآمد

## Claw ایکو سسٹم

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| زبان            | TypeScript      | Rust     | Go       | **Go**                                  |
| بائنری سائز     | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (بنیادی) / **~36 MB** (+ OTel) |
| Docker امیج     | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (خاموش)     | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| اسٹارٹ اپ       | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| ہدف ہارڈویئر    | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| فیچر                       | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| ملٹی-ٹیننٹ (PostgreSQL)    | —                                    | —                                            | —                                     | ✅                             |
| MCP انٹیگریشن              | — (ACP استعمال کرتا ہے)              | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| ایجنٹ ٹیمیں               | —                                    | —                                            | —                                     | ✅ ٹاسک بورڈ + میل باکس       |
| سیکیورٹی ہارڈننگ           | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | بنیادی (workspace restrict, exec deny) | ✅ 5-پرت دفاع                 |
| OTel آبزرویبلٹی            | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| پرامپٹ کیشنگ               | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| نالج گراف                  | —                                    | —                                            | —                                     | ✅ LLM ایکسٹریکشن + ٹراورسل  |
| اسکل سسٹم                  | ✅ ایمبیڈنگز/سیمینٹک               | ✅ SKILL.md + TOML                           | ✅ بنیادی                             | ✅ BM25 + pgvector ہائبرڈ     |
| لین-بیسڈ شیڈیولر           | ✅                                   | محدود کنکرنسی                               | —                                     | ✅ (main/subagent/team/cron)   |
| میسجنگ چینلز               | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| کمپینین ایپس               | macOS, iOS, Android                  | Python SDK                                   | —                                     | ویب ڈیش بورڈ                  |
| لائیو کینوس / وائس         | ✅ (A2UI + TTS/STT)                  | —                                            | وائس ٹرانسکرپشن                      | TTS (4 فراہم کنندگان)          |
| LLM فراہم کنندگان           | 10+                                  | 8 native + 29 compat                         | 13+                                   | **20+**                        |
| فی-صارف ورک اسپیسز         | ✅ (فائل بیسڈ)                       | —                                            | —                                     | ✅ (PostgreSQL)                |
| انکرپٹڈ سیکریٹس            | — (صرف env vars)                     | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM in DB           |

## آرکیٹیکچر

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## فوری آغاز

**پیش شرائط:** Go 1.26+، PostgreSQL 18 بمعہ pgvector، Docker (اختیاری)

### سورس سے

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # انٹرایکٹو سیٹ اپ وزرڈ
source .env.local && ./goclaw
```

### Docker کے ساتھ

```bash
# خودکار تیار کردہ سیکریٹس کے ساتھ .env بنائیں
chmod +x prepare-env.sh && ./prepare-env.sh

# .env میں کم از کم ایک GOCLAW_*_API_KEY شامل کریں، پھر:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# ویب ڈیش بورڈ http://localhost:3000 پر
# ہیلتھ چیک: curl http://localhost:18790/health
```

جب `GOCLAW_*_API_KEY` انوائرنمنٹ ویریبلز سیٹ ہوں، گیٹ وے انٹرایکٹو پرامپٹس کے بغیر خودکار آن بورڈ ہو جاتا ہے — فراہم کنندہ کا پتہ لگاتا ہے، مائیگریشنز چلاتا ہے، اور ڈیفالٹ ڈیٹا سیڈ کرتا ہے۔

> بلڈ ویریئنٹس (OTel، Tailscale، Redis)، Docker امیج ٹیگز، اور compose اوورلیز کے لیے، [ڈیپلائمنٹ گائیڈ](https://docs.goclaw.sh/#deploy-docker-compose) دیکھیں۔

## ملٹی-ایجنٹ آرکیسٹریشن

GoClaw ایجنٹ ٹیموں اور انٹر-ایجنٹ ڈیلیگیشن کو سپورٹ کرتا ہے — ہر ایجنٹ اپنی شناخت، ٹولز، LLM فراہم کنندہ، اور کانٹیکسٹ فائلوں کے ساتھ چلتا ہے۔

### ایجنٹ ڈیلیگیشن

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| موڈ | یہ کیسے کام کرتا ہے | بہترین استعمال |
|------|-------------|----------|
| **Sync** | ایجنٹ A ایجنٹ B سے پوچھتا ہے اور جواب کا **انتظار کرتا ہے** | فوری تلاشیں، حقائق کی تصدیق |
| **Async** | ایجنٹ A ایجنٹ B سے پوچھتا ہے اور **آگے بڑھتا ہے**۔ B بعد میں اعلان کرتا ہے | طویل ٹاسکس، رپورٹس، گہرا تجزیہ |

ایجنٹ واضح **پرمیشن لنکس** کے ذریعے ڈائریکشن کنٹرول (`outbound`، `inbound`، `bidirectional`) اور فی-لنک اور فی-ایجنٹ سطحوں پر کنکرنسی لمٹس کے ساتھ بات چیت کرتے ہیں۔

### ایجنٹ ٹیمیں

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **مشترکہ ٹاسک بورڈ** — `blocked_by` انحصارات کے ساتھ ٹاسکس بنائیں، دعویٰ کریں، مکمل کریں، تلاش کریں
- **ٹیم میل باکس** — براہ راست پیر-ٹو-پیر میسجنگ اور براڈکاسٹس
- **ٹولز**: ٹاسک مینجمنٹ کے لیے `team_tasks`، میل باکس کے لیے `team_message`

> ڈیلیگیشن کی تفصیلات، پرمیشن لنکس، اور کنکرنسی کنٹرول کے لیے، [ایجنٹ ٹیمز دستاویزات](https://docs.goclaw.sh/#teams-what-are-teams) دیکھیں۔

## بلٹ-ان ٹولز

| ٹول                | گروپ          | تفصیل                                                        |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | فائل مواد پڑھیں (virtual FS routing کے ساتھ)                |
| `write_file`       | fs            | فائلیں لکھیں/بنائیں                                         |
| `edit_file`        | fs            | موجودہ فائلوں میں ہدفی ترامیم لاگو کریں                    |
| `list_files`       | fs            | ڈائریکٹری مواد فہرست کریں                                   |
| `search`           | fs            | پیٹرن کے ذریعے فائل مواد تلاش کریں                         |
| `glob`             | fs            | glob پیٹرن کے ذریعے فائلیں تلاش کریں                       |
| `exec`             | runtime       | شیل کمانڈز چلائیں (اپروول ورک فلو کے ساتھ)                 |
| `web_search`       | web           | ویب تلاش کریں (Brave، DuckDuckGo)                           |
| `web_fetch`        | web           | ویب مواد حاصل کریں اور پارس کریں                            |
| `memory_search`    | memory        | طویل مدتی میموری تلاش کریں (FTS + vector)                   |
| `memory_get`       | memory        | میموری اندراجات حاصل کریں                                   |
| `skill_search`     | —             | اسکلز تلاش کریں (BM25 + embedding ہائبرڈ)                  |
| `knowledge_graph_search` | memory  | اداروں کو تلاش کریں اور نالج گراف تعلقات عبور کریں        |
| `create_image`     | media         | تصویر بنانا (DashScope، MiniMax)                             |
| `create_audio`     | media         | آڈیو بنانا (OpenAI، ElevenLabs، MiniMax، Suno)              |
| `create_video`     | media         | ویڈیو بنانا (MiniMax، Veo)                                   |
| `read_document`    | media         | دستاویز پڑھنا (Gemini File API، provider chain)              |
| `read_image`       | media         | تصویر تجزیہ                                                  |
| `read_audio`       | media         | آڈیو ٹرانسکرپشن اور تجزیہ                                  |
| `read_video`       | media         | ویڈیو تجزیہ                                                  |
| `message`          | messaging     | چینلز کو پیغامات بھیجیں                                     |
| `tts`              | —             | ٹیکسٹ-ٹو-اسپیچ سنتھیسس                                    |
| `spawn`            | —             | ایک سب ایجنٹ پیدا کریں                                      |
| `subagents`        | sessions      | چلتے سب ایجنٹس کو کنٹرول کریں                              |
| `team_tasks`       | teams         | مشترکہ ٹاسک بورڈ (فہرست، بنائیں، دعویٰ کریں، مکمل کریں، تلاش کریں) |
| `team_message`     | teams         | ٹیم میل باکس (بھیجیں، براڈکاسٹ کریں، پڑھیں)               |
| `sessions_list`    | sessions      | فعال سیشنز فہرست کریں                                       |
| `sessions_history` | sessions      | سیشن تاریخ دیکھیں                                           |
| `sessions_send`    | sessions      | ایک سیشن کو پیغام بھیجیں                                    |
| `sessions_spawn`   | sessions      | ایک نیا سیشن پیدا کریں                                      |
| `session_status`   | sessions      | سیشن اسٹیٹس چیک کریں                                       |
| `cron`             | automation    | cron جابز شیڈیول اور منظم کریں                             |
| `gateway`          | automation    | گیٹ وے انتظامیہ                                             |
| `browser`          | ui            | براؤزر آٹومیشن (navigate، click، type، screenshot)           |
| `announce_queue`   | automation    | Async نتیجہ اعلان (async delegations کے لیے)                |

## دستاویزات

مکمل دستاویزات **[docs.goclaw.sh](https://docs.goclaw.sh)** پر — یا [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs) میں سورس براؤز کریں۔

| سیکشن | موضوعات |
|---------|--------|
| [شروعات](https://docs.goclaw.sh/#what-is-goclaw) | انسٹالیشن، فوری آغاز، کنفیگریشن، ویب ڈیش بورڈ ٹور |
| [بنیادی تصورات](https://docs.goclaw.sh/#how-goclaw-works) | ایجنٹ لوپ، سیشنز، ٹولز، میموری، ملٹی-ٹیننسی |
| [ایجنٹس](https://docs.goclaw.sh/#creating-agents) | ایجنٹس بنانا، کانٹیکسٹ فائلیں، شخصیت، شیئرنگ اور رسائی |
| [فراہم کنندگان](https://docs.goclaw.sh/#providers-overview) | Anthropic، OpenAI، OpenRouter، Gemini، DeepSeek، +15 مزید |
| [چینلز](https://docs.goclaw.sh/#channels-overview) | Telegram، Discord، Slack، Feishu، Zalo، WhatsApp، WebSocket |
| [ایجنٹ ٹیمیں](https://docs.goclaw.sh/#teams-what-are-teams) | ٹیمیں، ٹاسک بورڈ، میسجنگ، ڈیلیگیشن اور ہینڈ آف |
| [ایڈوانسڈ](https://docs.goclaw.sh/#custom-tools) | کسٹم ٹولز، MCP، اسکلز، Cron، سینڈ باکس، ہکس، RBAC |
| [ڈیپلائمنٹ](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose، ڈیٹا بیس، سیکیورٹی، آبزرویبلٹی، Tailscale |
| [حوالہ](https://docs.goclaw.sh/#cli-commands) | CLI کمانڈز، REST API، WebSocket پروٹوکول، انوائرنمنٹ ویریبلز |

## ٹیسٹنگ

```bash
go test ./...                                    # یونٹ ٹیسٹس
go test -v ./tests/integration/ -timeout 120s    # انٹیگریشن ٹیسٹس (چلتے گیٹ وے کی ضرورت ہے)
```

## پروجیکٹ اسٹیٹس

تفصیلی فیچر اسٹیٹس کے لیے [CHANGELOG.md](CHANGELOG.md) دیکھیں، جس میں شامل ہے کہ پروڈکشن میں کیا ٹیسٹ ہو چکا ہے اور کیا ابھی زیر عمل ہے۔

## اعترافات

GoClaw اصل [OpenClaw](https://github.com/openclaw/openclaw) پروجیکٹ پر بنا ہے۔ ہم اس آرکیٹیکچر اور وژن کے شکر گزار ہیں جس نے اس Go پورٹ کو متاثر کیا۔

## لائسنس

MIT
