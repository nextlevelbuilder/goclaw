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
  <a href="https://docs.goclaw.sh">ডকুমেন্টেশন</a> •
  <a href="https://docs.goclaw.sh/#quick-start">দ্রুত শুরু</a> •
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

**GoClaw** হলো একটি মাল্টি-এজেন্ট AI গেটওয়ে যা LLM-কে আপনার টুলস, চ্যানেল এবং ডেটার সাথে সংযুক্ত করে — একটি একক Go বাইনারি হিসেবে স্থাপন করা হয়, কোনো রানটাইম নির্ভরতা ছাড়াই। এটি ২০+ LLM প্রদানকারীর মাধ্যমে সম্পূর্ণ মাল্টি-টেন্যান্ট আইসোলেশন সহ এজেন্ট টিম এবং ইন্টার-এজেন্ট ডেলিগেশন পরিচালনা করে।

[OpenClaw](https://github.com/openclaw/openclaw)-এর একটি Go পোর্ট, উন্নত নিরাপত্তা, মাল্টি-টেন্যান্ট PostgreSQL এবং প্রোডাকশন-গ্রেড পর্যবেক্ষণযোগ্যতা সহ।

🌐 **Languages:**
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

## যা এটিকে আলাদা করে

- **এজেন্ট টিম ও অর্কেস্ট্রেশন** — শেয়ারড টাস্ক বোর্ড, ইন্টার-এজেন্ট ডেলিগেশন (সিঙ্ক/অ্যাসিঙ্ক), এবং হাইব্রিড এজেন্ট ডিসকভারি সহ টিম
- **মাল্টি-টেন্যান্ট PostgreSQL** — প্রতি-ব্যবহারকারী ওয়ার্কস্পেস, প্রতি-ব্যবহারকারী কনটেক্সট ফাইল, এনক্রিপ্টেড API কী (AES-256-GCM), বিচ্ছিন্ন সেশন
- **একক বাইনারি** — ~২৫ MB স্ট্যাটিক Go বাইনারি, কোনো Node.js রানটাইম নেই, <১ সেকেন্ড স্টার্টআপ, $৫-এর VPS-এও চলে
- **প্রোডাকশন নিরাপত্তা** — ৫-স্তরের অনুমতি ব্যবস্থা (gateway auth → গ্লোবাল টুল পলিসি → প্রতি-এজেন্ট → প্রতি-চ্যানেল → মালিক-কেবল) এবং রেট লিমিটিং, প্রম্পট ইনজেকশন ডিটেকশন, SSRF সুরক্ষা, শেল ডিনাই প্যাটার্ন, এবং AES-256-GCM এনক্রিপশন
- **২০+ LLM প্রদানকারী** — Anthropic (নেটিভ HTTP+SSE প্রম্পট ক্যাশিং সহ), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP, এবং যেকোনো OpenAI-কম্প্যাটিবল এন্ডপয়েন্ট
- **৭টি মেসেজিং চ্যানেল** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — প্রতি-প্রদানকারী থিংকিং মোড (Anthropic বাজেট টোকেন, OpenAI রিজনিং এফোর্ট, DashScope থিংকিং বাজেট) স্ট্রিমিং সাপোর্ট সহ
- **Heartbeat** — HEARTBEAT.md চেকলিস্টের মাধ্যমে পর্যায়ক্রমিক এজেন্ট চেক-ইন, suppress-on-OK, সক্রিয় ঘণ্টা, রিট্রি লজিক এবং চ্যানেল ডেলিভারি সহ
- **শিডিউলিং ও ক্রন** — স্বয়ংক্রিয় এজেন্ট টাস্কের জন্য `at`, `every`, এবং ক্রন এক্সপ্রেশন, লেন-ভিত্তিক কনকারেন্সি সহ
- **পর্যবেক্ষণযোগ্যতা** — স্প্যান এবং প্রম্পট ক্যাশ মেট্রিক্স সহ বিল্ট-ইন LLM কল ট্রেসিং, ঐচ্ছিক OpenTelemetry OTLP এক্সপোর্ট

## Claw ইকোসিস্টেম

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| ভাষা            | TypeScript      | Rust     | Go       | **Go**                                  |
| বাইনারি সাইজ   | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (বেস) / **~36 MB** (+ OTel) |
| Docker ইমেজ    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (আইডল)     | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| স্টার্টআপ      | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| লক্ষ্য হার্ডওয়্যার | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| ফিচার                      | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| মাল্টি-টেন্যান্ট (PostgreSQL) | —                                    | —                                            | —                                     | ✅                             |
| MCP ইন্টিগ্রেশন            | — (ACP ব্যবহার করে)                 | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| এজেন্ট টিম                 | —                                    | —                                            | —                                     | ✅ টাস্ক বোর্ড + মেলবক্স      |
| নিরাপত্তা হার্ডেনিং        | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | বেসিক (workspace restrict, exec deny) | ✅ ৫-স্তরের প্রতিরক্ষা         |
| OTel পর্যবেক্ষণযোগ্যতা    | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| প্রম্পট ক্যাশিং            | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| নলেজ গ্রাফ                 | —                                    | —                                            | —                                     | ✅ LLM এক্সট্রাকশন + ট্র্যাভার্সাল |
| স্কিল সিস্টেম              | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ বেসিক                              | ✅ BM25 + pgvector হাইব্রিড    |
| লেন-ভিত্তিক শিডিউলার      | ✅                                   | বাউন্ডেড কনকারেন্সি                          | —                                     | ✅ (main/subagent/team/cron)   |
| মেসেজিং চ্যানেল            | ৩৭+                                  | ১৫+                                          | ১০+                                   | ৭+                             |
| কম্প্যানিয়ন অ্যাপ         | macOS, iOS, Android                  | Python SDK                                   | —                                     | ওয়েব ড্যাশবোর্ড               |
| লাইভ ক্যানভাস / ভয়েস      | ✅ (A2UI + TTS/STT)                  | —                                            | ভয়েস ট্রান্সক্রিপশন                   | TTS (৪ প্রদানকারী)             |
| LLM প্রদানকারী             | ১০+                                  | ৮ নেটিভ + ২৯ কম্প্যাট                        | ১৩+                                   | **২০+**                        |
| প্রতি-ব্যবহারকারী ওয়ার্কস্পেস | ✅ (ফাইল-ভিত্তিক)                  | —                                            | —                                     | ✅ (PostgreSQL)                |
| এনক্রিপ্টেড সিক্রেট        | — (শুধুমাত্র env vars)               | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM in DB           |

## আর্কিটেকচার

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## দ্রুত শুরু

**পূর্বশর্ত:** Go 1.26+, pgvector সহ PostgreSQL 18, Docker (ঐচ্ছিক)

### সোর্স থেকে

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interactive setup wizard
source .env.local && ./goclaw
```

### Docker-এর সাথে

```bash
# Generate .env with auto-generated secrets
chmod +x prepare-env.sh && ./prepare-env.sh

# Add at least one GOCLAW_*_API_KEY to .env, then:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Web Dashboard at http://localhost:3000
# Health check: curl http://localhost:18790/health
```

`GOCLAW_*_API_KEY` এনভায়রনমেন্ট ভেরিয়েবল সেট করা থাকলে, গেটওয়ে ইন্টারেক্টিভ প্রম্পট ছাড়াই স্বয়ংক্রিয়ভাবে অনবোর্ড হয় — প্রদানকারী সনাক্ত করে, মাইগ্রেশন চালায়, এবং ডিফল্ট ডেটা সিড করে।

> বিল্ড ভেরিয়েন্ট (OTel, Tailscale, Redis), Docker ইমেজ ট্যাগ এবং কম্পোজ ওভারলের জন্য, [ডেপ্লয়মেন্ট গাইড](https://docs.goclaw.sh/#deploy-docker-compose) দেখুন।

## মাল্টি-এজেন্ট অর্কেস্ট্রেশন

GoClaw এজেন্ট টিম এবং ইন্টার-এজেন্ট ডেলিগেশন সাপোর্ট করে — প্রতিটি এজেন্ট তার নিজস্ব পরিচয়, টুলস, LLM প্রদানকারী এবং কনটেক্সট ফাইল নিয়ে চলে।

### এজেন্ট ডেলিগেশন

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| মোড | কীভাবে কাজ করে | কোন ক্ষেত্রে সেরা |
|------|-------------|----------|
| **Sync** | এজেন্ট A, এজেন্ট B-কে জিজ্ঞেস করে এবং উত্তরের জন্য **অপেক্ষা করে** | দ্রুত লুকআপ, তথ্য যাচাই |
| **Async** | এজেন্ট A, এজেন্ট B-কে জিজ্ঞেস করে এবং **এগিয়ে যায়**। B পরে ঘোষণা করে | দীর্ঘ কাজ, রিপোর্ট, গভীর বিশ্লেষণ |

এজেন্টরা দিকনির্দেশ নিয়ন্ত্রণ (`outbound`, `inbound`, `bidirectional`) এবং প্রতি-লিংক ও প্রতি-এজেন্ট স্তরে কনকারেন্সি সীমা সহ স্পষ্ট **অনুমতি লিংক**-এর মাধ্যমে যোগাযোগ করে।

### এজেন্ট টিম

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **শেয়ারড টাস্ক বোর্ড** — `blocked_by` ডিপেন্ডেন্সি সহ টাস্ক তৈরি, দাবি, সম্পন্ন ও অনুসন্ধান করুন
- **টিম মেলবক্স** — ডাইরেক্ট পিয়ার-টু-পিয়ার মেসেজিং এবং ব্রডকাস্ট
- **টুলস**: টাস্ক ম্যানেজমেন্টের জন্য `team_tasks`, মেলবক্সের জন্য `team_message`

> ডেলিগেশনের বিস্তারিত, অনুমতি লিংক এবং কনকারেন্সি নিয়ন্ত্রণের জন্য, [এজেন্ট টিম ডকস](https://docs.goclaw.sh/#teams-what-are-teams) দেখুন।

## বিল্ট-ইন টুলস

| টুল                | গ্রুপ         | বিবরণ                                                        |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | ফাইলের বিষয়বস্তু পড়ুন (ভার্চুয়াল FS রাউটিং সহ)           |
| `write_file`       | fs            | ফাইল লিখুন/তৈরি করুন                                         |
| `edit_file`        | fs            | বিদ্যমান ফাইলে লক্ষ্যভিত্তিক সম্পাদনা প্রয়োগ করুন          |
| `list_files`       | fs            | ডিরেক্টরির বিষয়বস্তু তালিকাভুক্ত করুন                      |
| `search`           | fs            | প্যাটার্ন অনুযায়ী ফাইলের বিষয়বস্তু অনুসন্ধান করুন         |
| `glob`             | fs            | glob প্যাটার্ন দ্বারা ফাইল খুঁজুন                            |
| `exec`             | runtime       | শেল কমান্ড চালান (অনুমোদন ওয়ার্কফ্লো সহ)                    |
| `web_search`       | web           | ওয়েব অনুসন্ধান করুন (Brave, DuckDuckGo)                     |
| `web_fetch`        | web           | ওয়েব কনটেন্ট ফেচ ও পার্স করুন                               |
| `memory_search`    | memory        | দীর্ঘমেয়াদী মেমরি অনুসন্ধান করুন (FTS + vector)             |
| `memory_get`       | memory        | মেমরি এন্ট্রি পুনরুদ্ধার করুন                                |
| `skill_search`     | —             | স্কিল অনুসন্ধান করুন (BM25 + embedding হাইব্রিড)             |
| `knowledge_graph_search` | memory  | এন্টিটি অনুসন্ধান করুন এবং নলেজ গ্রাফ সম্পর্ক ট্র্যাভার্স করুন |
| `create_image`     | media         | ছবি তৈরি (DashScope, MiniMax)                                |
| `create_audio`     | media         | অডিও তৈরি (OpenAI, ElevenLabs, MiniMax, Suno)               |
| `create_video`     | media         | ভিডিও তৈরি (MiniMax, Veo)                                    |
| `read_document`    | media         | ডকুমেন্ট পড়া (Gemini File API, প্রদানকারী চেইন)             |
| `read_image`       | media         | ছবি বিশ্লেষণ                                                 |
| `read_audio`       | media         | অডিও ট্রান্সক্রিপশন ও বিশ্লেষণ                              |
| `read_video`       | media         | ভিডিও বিশ্লেষণ                                               |
| `message`          | messaging     | চ্যানেলে বার্তা পাঠান                                        |
| `tts`              | —             | Text-to-Speech সংশ্লেষণ                                      |
| `spawn`            | —             | একটি সাবএজেন্ট স্পন করুন                                     |
| `subagents`        | sessions      | চলমান সাবএজেন্ট নিয়ন্ত্রণ করুন                              |
| `team_tasks`       | teams         | শেয়ারড টাস্ক বোর্ড (তালিকা, তৈরি, দাবি, সম্পন্ন, অনুসন্ধান) |
| `team_message`     | teams         | টিম মেলবক্স (পাঠান, ব্রডকাস্ট, পড়ুন)                        |
| `sessions_list`    | sessions      | সক্রিয় সেশনের তালিকা                                        |
| `sessions_history` | sessions      | সেশন ইতিহাস দেখুন                                            |
| `sessions_send`    | sessions      | একটি সেশনে বার্তা পাঠান                                      |
| `sessions_spawn`   | sessions      | একটি নতুন সেশন স্পন করুন                                     |
| `session_status`   | sessions      | সেশনের অবস্থা পরীক্ষা করুন                                   |
| `cron`             | automation    | ক্রন জব শিডিউল ও পরিচালনা করুন                               |
| `gateway`          | automation    | গেটওয়ে প্রশাসন                                               |
| `browser`          | ui            | ব্রাউজার অটোমেশন (navigate, click, type, screenshot)         |
| `announce_queue`   | automation    | অ্যাসিঙ্ক ফলাফল ঘোষণা (অ্যাসিঙ্ক ডেলিগেশনের জন্য)           |

## ডকুমেন্টেশন

সম্পূর্ণ ডকুমেন্টেশন **[docs.goclaw.sh](https://docs.goclaw.sh)**-এ পাওয়া যাবে — অথবা [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)-এ সোর্স ব্রাউজ করুন।

| বিভাগ | বিষয়বস্তু |
|---------|--------|
| [শুরু করা](https://docs.goclaw.sh/#what-is-goclaw) | ইনস্টলেশন, দ্রুত শুরু, কনফিগারেশন, ওয়েব ড্যাশবোর্ড ট্যুর |
| [মূল ধারণাসমূহ](https://docs.goclaw.sh/#how-goclaw-works) | এজেন্ট লুপ, সেশন, টুলস, মেমরি, মাল্টি-টেন্যান্সি |
| [এজেন্ট](https://docs.goclaw.sh/#creating-agents) | এজেন্ট তৈরি, কনটেক্সট ফাইল, ব্যক্তিত্ব, শেয়ারিং ও অ্যাক্সেস |
| [প্রদানকারী](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +১৫ আরও |
| [চ্যানেল](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [এজেন্ট টিম](https://docs.goclaw.sh/#teams-what-are-teams) | টিম, টাস্ক বোর্ড, মেসেজিং, ডেলিগেশন ও হ্যান্ডঅফ |
| [উন্নত](https://docs.goclaw.sh/#custom-tools) | কাস্টম টুলস, MCP, স্কিল, ক্রন, স্যান্ডবক্স, হুকস, RBAC |
| [ডেপ্লয়মেন্ট](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, ডেটাবেস, নিরাপত্তা, পর্যবেক্ষণযোগ্যতা, Tailscale |
| [রেফারেন্স](https://docs.goclaw.sh/#cli-commands) | CLI কমান্ড, REST API, WebSocket প্রোটোকল, এনভায়রনমেন্ট ভেরিয়েবল |

## পরীক্ষা

```bash
go test ./...                                    # Unit tests
go test -v ./tests/integration/ -timeout 120s    # Integration tests (requires running gateway)
```

## প্রকল্পের অবস্থা

বিস্তারিত ফিচার স্ট্যাটাসের জন্য [CHANGELOG.md](CHANGELOG.md) দেখুন — প্রোডাকশনে কী পরীক্ষিত হয়েছে এবং কী এখনও চলমান তা সহ।

## কৃতজ্ঞতা

GoClaw মূল [OpenClaw](https://github.com/openclaw/openclaw) প্রকল্পের উপর ভিত্তি করে তৈরি। এই Go পোর্টকে অনুপ্রাণিত করা আর্কিটেকচার ও দৃষ্টিভঙ্গির জন্য আমরা কৃতজ্ঞ।

## লাইসেন্স

MIT
