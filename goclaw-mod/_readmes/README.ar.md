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
  <a href="https://docs.goclaw.sh">التوثيق</a> •
  <a href="https://docs.goclaw.sh/#quick-start">البدء السريع</a> •
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

**GoClaw** هو بوابة ذكاء اصطناعي متعددة الوكلاء تربط نماذج اللغة الكبيرة بأدواتك وقنواتك وبياناتك — يُنشر كملف Go ثنائي واحد بدون أي تبعيات وقت تشغيل. يُنسّق فرق الوكلاء والتفويض بين الوكلاء عبر أكثر من 20 مزوّد نماذج لغوية مع عزل كامل لمتعددي المستأجرين.

منفذ Go من [OpenClaw](https://github.com/openclaw/openclaw) مع أمان محسّن، وPostgreSQL متعدد المستأجرين، وإمكانية رصد وإنتاجية متميزة.

🌐 **اللغات:**
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

## ما الذي يميّزه

- **فرق الوكلاء والتنسيق** — فرق ذات لوحات مهام مشتركة، وتفويض بين الوكلاء (متزامن/غير متزامن)، واكتشاف هجين للوكلاء
- **PostgreSQL متعدد المستأجرين** — مساحات عمل لكل مستخدم، وملفات سياق لكل مستخدم، ومفاتيح API مشفّرة (AES-256-GCM)، وجلسات معزولة
- **ملف ثنائي واحد** — ملف Go ثابت بحجم ~25 ميغابايت، بدون Node.js، بدء تشغيل أقل من ثانية، يعمل على خادم VPS بـ5 دولارات
- **أمان للإنتاج** — نظام أذونات من 5 طبقات (مصادقة البوابة ← سياسة الأداة العالمية ← لكل وكيل ← لكل قناة ← للمالك فقط) بالإضافة إلى تحديد المعدل، وكشف حقن البرومبت، وحماية SSRF، وأنماط رفض Shell، وتشفير AES-256-GCM
- **أكثر من 20 مزوّد نماذج لغوية** — Anthropic (HTTP+SSE أصلي مع تخزين مؤقت للبرومبت)، OpenAI، OpenRouter، Groq، DeepSeek، Gemini، Mistral، xAI، MiniMax، Cohere، Perplexity، DashScope، Bailian، Zai، Ollama، Ollama Cloud، Claude CLI، Codex، ACP، وأي نقطة نهاية متوافقة مع OpenAI
- **7 قنوات مراسلة** — Telegram، Discord، Slack، Zalo OA، Zalo Personal، Feishu/Lark، WhatsApp
- **Extended Thinking** — وضع تفكير لكل مزوّد (رموز ميزانية Anthropic، جهد استدلال OpenAI، ميزانية تفكير DashScope) مع دعم البث
- **Heartbeat** — فحوصات دورية للوكيل عبر قوائم مراجعة HEARTBEAT.md مع كبت عند النجاح، وساعات نشطة، ومنطق إعادة المحاولة، وتسليم القناة
- **الجدولة والكرون** — تعبيرات `at` و`every` والكرون للمهام الآلية للوكيل مع تزامن قائم على المسارات
- **إمكانية الرصد** — تتبع مدمج لاستدعاءات نموذج اللغة الكبيرة مع spans ومقاييس ذاكرة التخزين المؤقت للبرومبت، وتصدير اختياري عبر OpenTelemetry OTLP

## نظام بيئة Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| اللغة           | TypeScript      | Rust     | Go       | **Go**                                  |
| حجم الملف الثنائي | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (أساسي) / **~36 MB** (+ OTel) |
| صورة Docker     | —               | —        | —        | **~50 MB** (Alpine)                     |
| الذاكرة (خامل)  | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| بدء التشغيل     | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| العتاد المستهدف  | Mac Mini بـ599 دولار+ | حافة 10 دولار | حافة 10 دولار | **VPS بـ5 دولار+**              |

| الميزة                     | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| متعدد المستأجرين (PostgreSQL) | —                                 | —                                            | —                                     | ✅                             |
| تكامل MCP                  | — (يستخدم ACP)                       | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| فرق الوكلاء                | —                                    | —                                            | —                                     | ✅ لوحة مهام + صندوق بريد      |
| تصليب الأمان               | ✅ (SSRF، اجتياز المسار، الحقن)      | ✅ (صندوق حماية، تحديد المعدل، الحقن، الإقران) | أساسي (تقييد مساحة العمل، رفض التنفيذ) | ✅ دفاع من 5 طبقات             |
| إمكانية الرصد OTel         | ✅ (امتداد اختياري)                  | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (علامة بناء اختيارية)  |
| التخزين المؤقت للبرومبت    | —                                    | —                                            | —                                     | ✅ Anthropic + متوافق OpenAI   |
| الرسم البياني للمعرفة      | —                                    | —                                            | —                                     | ✅ استخراج نموذج اللغة + اجتياز |
| نظام المهارات              | ✅ تضمينات/دلالية                    | ✅ SKILL.md + TOML                           | ✅ أساسي                              | ✅ BM25 + pgvector هجين        |
| جدولة قائمة على المسارات  | ✅                                   | تزامن محدود                                  | —                                     | ✅ (رئيسي/وكيل فرعي/فريق/كرون) |
| قنوات المراسلة             | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| التطبيقات المرافقة         | macOS، iOS، Android                  | Python SDK                                   | —                                     | لوحة تحكم ويب                  |
| Canvas الحي / الصوت        | ✅ (A2UI + TTS/STT)                  | —                                            | نسخ صوتي                              | TTS (4 مزودين)                 |
| مزودو نماذج اللغة الكبيرة  | 10+                                  | 8 أصلي + 29 متوافق                          | 13+                                   | **20+**                        |
| مساحات عمل لكل مستخدم      | ✅ (قائمة على الملفات)               | —                                            | —                                     | ✅ (PostgreSQL)                |
| الأسرار المشفّرة           | — (متغيرات البيئة فقط)               | ✅ ChaCha20-Poly1305                         | — (JSON نص عادي)                      | ✅ AES-256-GCM في قاعدة البيانات |

## المعمارية

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## البدء السريع

**المتطلبات الأساسية:** Go 1.26+، PostgreSQL 18 مع pgvector، Docker (اختياري)

### من المصدر

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # معالج إعداد تفاعلي
source .env.local && ./goclaw
```

### مع Docker

```bash
# توليد .env مع أسرار مولّدة تلقائياً
chmod +x prepare-env.sh && ./prepare-env.sh

# أضف على الأقل مفتاح GOCLAW_*_API_KEY إلى .env، ثم:
make up

# لوحة تحكم الويب على http://localhost:18790
# فحص الصحة: curl http://localhost:18790/health
```

عند تعيين متغيرات البيئة `GOCLAW_*_API_KEY`، تعمل البوابة على الإعداد التلقائي بدون مطالبات تفاعلية — تكتشف المزوّد، وتشغّل الترحيلات، وتزرع البيانات الافتراضية.

> لاختلافات البناء (OTel، Tailscale، Redis)، وعلامات صور Docker، والتراكبات المُركّبة، راجع [دليل النشر](https://docs.goclaw.sh/#deploy-docker-compose).

## تنسيق متعدد الوكلاء

يدعم GoClaw فرق الوكلاء والتفويض بين الوكلاء — يعمل كل وكيل بهويته الخاصة وأدواته ومزوّد نماذج اللغة الكبيرة وملفات السياق.

### تفويض الوكيل

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| الوضع | كيف يعمل | الأنسب لـ |
|------|-------------|----------|
| **متزامن** | الوكيل A يسأل الوكيل B و**ينتظر** الإجابة | البحثات السريعة، التحقق من الحقائق |
| **غير متزامن** | الوكيل A يسأل الوكيل B و**يكمل عمله**. يُعلن B لاحقاً | المهام الطويلة، التقارير، التحليل العميق |

يتواصل الوكلاء عبر **روابط الأذونات** الصريحة مع التحكم في الاتجاه (`outbound`، `inbound`، `bidirectional`) وحدود التزامن على مستوى الرابط والوكيل.

### فرق الوكلاء

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **لوحة المهام المشتركة** — إنشاء المهام، والمطالبة بها، وإكمالها، والبحث فيها مع تبعيات `blocked_by`
- **صندوق بريد الفريق** — رسائل مباشرة من نظير إلى نظير وبث جماعي
- **الأدوات**: `team_tasks` لإدارة المهام، و`team_message` لصندوق البريد

> للاطلاع على تفاصيل التفويض وروابط الأذونات والتحكم في التزامن، راجع [توثيق فرق الوكلاء](https://docs.goclaw.sh/#teams-what-are-teams).

## الأدوات المدمجة

| الأداة               | المجموعة      | الوصف                                                        |
| -------------------- | ------------- | ------------------------------------------------------------ |
| `read_file`          | fs            | قراءة محتويات الملفات (مع توجيه نظام الملفات الافتراضي)    |
| `write_file`         | fs            | كتابة/إنشاء الملفات                                         |
| `edit_file`          | fs            | تطبيق تعديلات محددة على الملفات الموجودة                    |
| `list_files`         | fs            | عرض محتويات الدليل                                          |
| `search`             | fs            | البحث في محتويات الملفات بنمط معين                          |
| `glob`               | fs            | البحث عن الملفات بنمط glob                                  |
| `exec`               | runtime       | تنفيذ أوامر Shell (مع سير عمل الموافقة)                     |
| `web_search`         | web           | البحث على الويب (Brave، DuckDuckGo)                         |
| `web_fetch`          | web           | جلب محتوى الويب وتحليله                                     |
| `memory_search`      | memory        | البحث في الذاكرة طويلة المدى (FTS + متجه)                   |
| `memory_get`         | memory        | استرجاع مدخلات الذاكرة                                      |
| `skill_search`       | —             | البحث في المهارات (BM25 + تضمين هجين)                      |
| `knowledge_graph_search` | memory   | البحث في الكيانات واجتياز علاقات الرسم البياني للمعرفة      |
| `create_image`       | media         | توليد الصور (DashScope، MiniMax)                            |
| `create_audio`       | media         | توليد الصوت (OpenAI، ElevenLabs، MiniMax، Suno)             |
| `create_video`       | media         | توليد الفيديو (MiniMax، Veo)                                |
| `read_document`      | media         | قراءة المستندات (Gemini File API، سلسلة المزودين)           |
| `read_image`         | media         | تحليل الصور                                                 |
| `read_audio`         | media         | نسخ الصوت وتحليله                                           |
| `read_video`         | media         | تحليل الفيديو                                               |
| `message`            | messaging     | إرسال رسائل إلى القنوات                                     |
| `tts`                | —             | تحويل النص إلى كلام                                         |
| `spawn`              | —             | إطلاق وكيل فرعي                                             |
| `subagents`          | sessions      | التحكم في الوكلاء الفرعية الجارية                           |
| `team_tasks`         | teams         | لوحة المهام المشتركة (قائمة، إنشاء، مطالبة، إكمال، بحث)    |
| `team_message`       | teams         | صندوق بريد الفريق (إرسال، بث، قراءة)                       |
| `sessions_list`      | sessions      | عرض الجلسات النشطة                                          |
| `sessions_history`   | sessions      | عرض تاريخ الجلسة                                            |
| `sessions_send`      | sessions      | إرسال رسالة إلى جلسة                                        |
| `sessions_spawn`     | sessions      | إطلاق جلسة جديدة                                            |
| `session_status`     | sessions      | فحص حالة الجلسة                                             |
| `cron`               | automation    | جدولة وإدارة وظائف الكرون                                   |
| `gateway`            | automation    | إدارة البوابة                                               |
| `browser`            | ui            | أتمتة المتصفح (تصفح، نقر، كتابة، لقطة شاشة)               |
| `announce_queue`     | automation    | إعلان النتائج غير المتزامنة (للتفويضات غير المتزامنة)       |

## التوثيق

التوثيق الكامل على **[docs.goclaw.sh](https://docs.goclaw.sh)** — أو تصفّح المصدر في [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| القسم | المواضيع |
|---------|--------|
| [البدء](https://docs.goclaw.sh/#what-is-goclaw) | التثبيت، البدء السريع، الإعداد، جولة لوحة تحكم الويب |
| [المفاهيم الأساسية](https://docs.goclaw.sh/#how-goclaw-works) | حلقة الوكيل، الجلسات، الأدوات، الذاكرة، متعددية المستأجرين |
| [الوكلاء](https://docs.goclaw.sh/#creating-agents) | إنشاء الوكلاء، ملفات السياق، الشخصية، المشاركة والوصول |
| [المزودون](https://docs.goclaw.sh/#providers-overview) | Anthropic، OpenAI، OpenRouter، Gemini، DeepSeek، +15 المزيد |
| [القنوات](https://docs.goclaw.sh/#channels-overview) | Telegram، Discord، Slack، Feishu، Zalo، WhatsApp، WebSocket |
| [فرق الوكلاء](https://docs.goclaw.sh/#teams-what-are-teams) | الفرق، لوحة المهام، المراسلة، التفويض والتسليم |
| [متقدم](https://docs.goclaw.sh/#custom-tools) | الأدوات المخصصة، MCP، المهارات، الكرون، صندوق الحماية، الخطافات، RBAC |
| [النشر](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose، قاعدة البيانات، الأمان، إمكانية الرصد، Tailscale |
| [المرجع](https://docs.goclaw.sh/#cli-commands) | أوامر CLI، REST API، بروتوكول WebSocket، متغيرات البيئة |

## الاختبار

```bash
go test ./...                                    # اختبارات الوحدة
go test -v ./tests/integration/ -timeout 120s    # اختبارات التكامل (تتطلب بوابة قيد التشغيل)
```

## حالة المشروع

راجع [CHANGELOG.md](CHANGELOG.md) للاطلاع على حالة الميزات التفصيلية بما في ذلك ما تم اختباره في الإنتاج وما لا يزال قيد التطوير.

## شكر وتقدير

بُني GoClaw على مشروع [OpenClaw](https://github.com/openclaw/openclaw) الأصلي. نحن ممتنون للمعمارية والرؤية التي ألهمت هذا المنفذ بلغة Go.

## الرخصة

MIT
