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
  <a href="https://docs.goclaw.sh">दस्तावेज़ीकरण</a> •
  <a href="https://docs.goclaw.sh/#quick-start">त्वरित प्रारंभ</a> •
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

**GoClaw** एक मल्टी-एजेंट AI गेटवे है जो LLMs को आपके टूल्स, चैनलों और डेटा से जोड़ता है — एक सिंगल Go बाइनरी के रूप में तैनात, बिना किसी रनटाइम निर्भरता के। यह 20+ LLM प्रदाताओं के साथ पूर्ण मल्टी-टेनेंट आइसोलेशन के साथ एजेंट टीमों और इंटर-एजेंट डेलीगेशन को ऑर्केस्ट्रेट करता है।

[OpenClaw](https://github.com/openclaw/openclaw) का एक Go पोर्ट, जिसमें उन्नत सुरक्षा, मल्टी-टेनेंट PostgreSQL और प्रोडक्शन-ग्रेड ऑब्ज़र्वेबिलिटी है।

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

## यह क्या अलग बनाता है

- **एजेंट टीमें और ऑर्केस्ट्रेशन** — साझा टास्क बोर्ड, इंटर-एजेंट डेलीगेशन (sync/async), और हाइब्रिड एजेंट डिस्कवरी के साथ टीमें
- **मल्टी-टेनेंट PostgreSQL** — प्रति-उपयोगकर्ता वर्कस्पेस, प्रति-उपयोगकर्ता कॉन्टेक्स्ट फ़ाइलें, एन्क्रिप्टेड API कुंजियाँ (AES-256-GCM), आइसोलेटेड सेशन
- **सिंगल बाइनरी** — ~25 MB स्टेटिक Go बाइनरी, कोई Node.js रनटाइम नहीं, <1s स्टार्टअप, $5 VPS पर चलती है
- **प्रोडक्शन सुरक्षा** — 5-लेयर परमिशन सिस्टम (gateway auth → global tool policy → per-agent → per-channel → owner-only) के साथ रेट लिमिटिंग, प्रॉम्प्ट इंजेक्शन डिटेक्शन, SSRF प्रोटेक्शन, शेल डेनाय पैटर्न, और AES-256-GCM एन्क्रिप्शन
- **20+ LLM प्रदाता** — Anthropic (नेटिव HTTP+SSE विथ प्रॉम्प्ट कैशिंग), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP, और कोई भी OpenAI-compatible एंडपॉइंट
- **7 मैसेजिंग चैनल** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — प्रति-प्रदाता थिंकिंग मोड (Anthropic बजट टोकन, OpenAI रीज़निंग एफर्ट, DashScope थिंकिंग बजट) स्ट्रीमिंग सपोर्ट के साथ
- **Heartbeat** — HEARTBEAT.md चेकलिस्ट के माध्यम से आवधिक एजेंट चेक-इन, suppress-on-OK, सक्रिय घंटे, रिट्री लॉजिक और चैनल डिलीवरी के साथ
- **शेड्यूलिंग और Cron** — स्वचालित एजेंट कार्यों के लिए `at`, `every`, और cron एक्सप्रेशन, लेन-आधारित कंकरेंसी के साथ
- **ऑब्ज़र्वेबिलिटी** — स्पैन और प्रॉम्प्ट कैश मेट्रिक्स के साथ बिल्ट-इन LLM कॉल ट्रेसिंग, वैकल्पिक OpenTelemetry OTLP एक्सपोर्ट

## Claw इकोसिस्टम

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| भाषा            | TypeScript      | Rust     | Go       | **Go**                                  |
| बाइनरी आकार     | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Docker इमेज     | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (निष्क्रिय) | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| स्टार्टअप       | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| लक्ष्य हार्डवेयर | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| फीचर                       | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| मल्टी-टेनेंट (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| MCP इंटीग्रेशन             | — (uses ACP)                         | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| एजेंट टीमें                | —                                    | —                                            | —                                     | ✅ Task board + mailbox        |
| सुरक्षा हार्डनिंग           | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Basic (workspace restrict, exec deny) | ✅ 5-layer defense             |
| OTel ऑब्ज़र्वेबिलिटी       | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| प्रॉम्प्ट कैशिंग           | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| नॉलेज ग्राफ                | —                                    | —                                            | —                                     | ✅ LLM extraction + traversal  |
| स्किल सिस्टम               | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ Basic                              | ✅ BM25 + pgvector hybrid      |
| लेन-आधारित शेड्यूलर        | ✅                                   | Bounded concurrency                          | —                                     | ✅ (main/subagent/team/cron)   |
| मैसेजिंग चैनल              | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| कम्पेनियन ऐप्स             | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web dashboard                  |
| लाइव कैनवास / वॉइस         | ✅ (A2UI + TTS/STT)                  | —                                            | Voice transcription                   | TTS (4 providers)              |
| LLM प्रदाता                | 10+                                  | 8 native + 29 compat                         | 13+                                   | **20+**                        |
| प्रति-उपयोगकर्ता वर्कस्पेस | ✅ (file-based)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| एन्क्रिप्टेड सीक्रेट्स     | — (env vars only)                    | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM in DB           |

## आर्किटेक्चर

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## त्वरित प्रारंभ

**पूर्व-आवश्यकताएँ:** Go 1.26+, PostgreSQL 18 with pgvector, Docker (वैकल्पिक)

### सोर्स से

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interactive setup wizard
source .env.local && ./goclaw
```

### Docker के साथ

```bash
# Generate .env with auto-generated secrets
chmod +x prepare-env.sh && ./prepare-env.sh

# Add at least one GOCLAW_*_API_KEY to .env, then:
make up

# Web Dashboard at http://localhost:18790
# Health check: curl http://localhost:18790/health
```

जब `GOCLAW_*_API_KEY` एनवायरनमेंट वेरिएबल सेट हों, तो गेटवे इंटरएक्टिव प्रॉम्प्ट के बिना स्वत: ऑनबोर्ड हो जाता है — प्रदाता का पता लगाता है, माइग्रेशन चलाता है, और डिफ़ॉल्ट डेटा सीड करता है।

> बिल्ड वेरिएंट (OTel, Tailscale, Redis), Docker इमेज टैग, और compose ओवरले के लिए, [Deployment Guide](https://docs.goclaw.sh/#deploy-docker-compose) देखें।

## मल्टी-एजेंट ऑर्केस्ट्रेशन

GoClaw एजेंट टीमों और इंटर-एजेंट डेलीगेशन का समर्थन करता है — प्रत्येक एजेंट अपनी पहचान, टूल्स, LLM प्रदाता, और कॉन्टेक्स्ट फ़ाइलों के साथ चलता है।

### एजेंट डेलीगेशन

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| मोड | यह कैसे काम करता है | किसके लिए सबसे अच्छा |
|------|-------------|----------|
| **Sync** | एजेंट A, एजेंट B से पूछता है और उत्तर की **प्रतीक्षा करता है** | त्वरित खोज, तथ्य जाँच |
| **Async** | एजेंट A, एजेंट B से पूछता है और **आगे बढ़ जाता है**। B बाद में घोषणा करता है | लंबे कार्य, रिपोर्ट, गहन विश्लेषण |

एजेंट स्पष्ट **परमिशन लिंक** के माध्यम से संवाद करते हैं, जिसमें दिशा नियंत्रण (`outbound`, `inbound`, `bidirectional`) और प्रति-लिंक तथा प्रति-एजेंट दोनों स्तरों पर कंकरेंसी सीमाएँ होती हैं।

### एजेंट टीमें

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **साझा टास्क बोर्ड** — `blocked_by` निर्भरताओं के साथ टास्क बनाएं, क्लेम करें, पूरा करें, खोजें
- **टीम मेलबॉक्स** — सीधे पीयर-टू-पीयर मैसेजिंग और ब्रॉडकास्ट
- **टूल्स**: टास्क मैनेजमेंट के लिए `team_tasks`, मेलबॉक्स के लिए `team_message`

> डेलीगेशन विवरण, परमिशन लिंक, और कंकरेंसी कंट्रोल के लिए, [Agent Teams docs](https://docs.goclaw.sh/#teams-what-are-teams) देखें।

## बिल्ट-इन टूल्स

| टूल                | समूह         | विवरण                                                        |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | फ़ाइल सामग्री पढ़ें (virtual FS routing के साथ)              |
| `write_file`       | fs            | फ़ाइलें लिखें/बनाएं                                          |
| `edit_file`        | fs            | मौजूदा फ़ाइलों में लक्षित संपादन लागू करें                    |
| `list_files`       | fs            | डायरेक्टरी सामग्री सूचीबद्ध करें                             |
| `search`           | fs            | पैटर्न द्वारा फ़ाइल सामग्री खोजें                            |
| `glob`             | fs            | glob पैटर्न द्वारा फ़ाइलें खोजें                             |
| `exec`             | runtime       | शेल कमांड चलाएं (approval workflow के साथ)                   |
| `web_search`       | web           | वेब खोजें (Brave, DuckDuckGo)                                |
| `web_fetch`        | web           | वेब सामग्री फेच और पार्स करें                                |
| `memory_search`    | memory        | दीर्घकालिक मेमोरी खोजें (FTS + vector)                      |
| `memory_get`       | memory        | मेमोरी एंट्री प्राप्त करें                                   |
| `skill_search`     | —             | स्किल खोजें (BM25 + embedding hybrid)                        |
| `knowledge_graph_search` | memory  | एंटिटी खोजें और नॉलेज ग्राफ संबंध ट्रैवर्स करें             |
| `create_image`     | media         | इमेज जेनरेशन (DashScope, MiniMax)                            |
| `create_audio`     | media         | ऑडियो जेनरेशन (OpenAI, ElevenLabs, MiniMax, Suno)           |
| `create_video`     | media         | वीडियो जेनरेशन (MiniMax, Veo)                                |
| `read_document`    | media         | दस्तावेज़ पढ़ना (Gemini File API, provider chain)             |
| `read_image`       | media         | इमेज विश्लेषण                                                |
| `read_audio`       | media         | ऑडियो ट्रांसक्रिप्शन और विश्लेषण                            |
| `read_video`       | media         | वीडियो विश्लेषण                                              |
| `message`          | messaging     | चैनलों पर संदेश भेजें                                        |
| `tts`              | —             | Text-to-Speech सिंथेसिस                                      |
| `spawn`            | —             | एक सबएजेंट स्पॉन करें                                        |
| `subagents`        | sessions      | चल रहे सबएजेंट नियंत्रित करें                                |
| `team_tasks`       | teams         | साझा टास्क बोर्ड (list, create, claim, complete, search)     |
| `team_message`     | teams         | टीम मेलबॉक्स (send, broadcast, read)                         |
| `sessions_list`    | sessions      | सक्रिय सेशन सूचीबद्ध करें                                    |
| `sessions_history` | sessions      | सेशन इतिहास देखें                                            |
| `sessions_send`    | sessions      | किसी सेशन पर संदेश भेजें                                     |
| `sessions_spawn`   | sessions      | एक नया सेशन स्पॉन करें                                       |
| `session_status`   | sessions      | सेशन स्थिति जाँचें                                           |
| `cron`             | automation    | cron जॉब शेड्यूल और प्रबंधित करें                            |
| `gateway`          | automation    | गेटवे प्रशासन                                                |
| `browser`          | ui            | ब्राउज़र ऑटोमेशन (navigate, click, type, screenshot)         |
| `announce_queue`   | automation    | असिंक्रोनस परिणाम घोषणा (async delegations के लिए)           |

## दस्तावेज़ीकरण

पूर्ण दस्तावेज़ीकरण **[docs.goclaw.sh](https://docs.goclaw.sh)** पर — या [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs) में सोर्स ब्राउज़ करें।

| अनुभाग | विषय |
|---------|--------|
| [Getting Started](https://docs.goclaw.sh/#what-is-goclaw) | इंस्टॉलेशन, त्वरित प्रारंभ, कॉन्फ़िगरेशन, Web Dashboard टूर |
| [Core Concepts](https://docs.goclaw.sh/#how-goclaw-works) | एजेंट लूप, सेशन, टूल्स, मेमोरी, मल्टी-टेनेंसी |
| [Agents](https://docs.goclaw.sh/#creating-agents) | एजेंट बनाना, कॉन्टेक्स्ट फ़ाइलें, व्यक्तित्व, शेयरिंग और एक्सेस |
| [Providers](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 और |
| [Channels](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Agent Teams](https://docs.goclaw.sh/#teams-what-are-teams) | टीमें, टास्क बोर्ड, मैसेजिंग, डेलीगेशन और हैंडऑफ |
| [Advanced](https://docs.goclaw.sh/#custom-tools) | कस्टम टूल्स, MCP, स्किल्स, Cron, सैंडबॉक्स, हुक, RBAC |
| [Deployment](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, डेटाबेस, सुरक्षा, ऑब्ज़र्वेबिलिटी, Tailscale |
| [Reference](https://docs.goclaw.sh/#cli-commands) | CLI कमांड, REST API, WebSocket प्रोटोकॉल, एनवायरनमेंट वेरिएबल |

## परीक्षण

```bash
go test ./...                                    # Unit tests
go test -v ./tests/integration/ -timeout 120s    # Integration tests (requires running gateway)
```

## प्रोजेक्ट स्थिति

विस्तृत फीचर स्थिति के लिए [CHANGELOG.md](CHANGELOG.md) देखें, जिसमें शामिल है कि प्रोडक्शन में क्या परीक्षण किया गया है और क्या अभी भी प्रगति में है।

## आभार

GoClaw मूल [OpenClaw](https://github.com/openclaw/openclaw) प्रोजेक्ट पर निर्मित है। हम उस आर्किटेक्चर और दृष्टिकोण के आभारी हैं जिसने इस Go पोर्ट को प्रेरित किया।

## लाइसेंस

MIT
