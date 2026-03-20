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
  <a href="https://docs.goclaw.sh">תיעוד</a> •
  <a href="https://docs.goclaw.sh/#quick-start">התחלה מהירה</a> •
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

**GoClaw** הוא שער AI רב-סוכנים המחבר מודלי שפה גדולים לכלים, לערוצים ולנתונים שלך — פרוס כקובץ בינארי יחיד של Go ללא תלויות ריצה. הוא מתזמר צוותי סוכנים ואת הברת המשימות בין סוכנים אצל מעל 20 ספקי LLM עם בידוד מרובה-דיירים מלא.

פורט Go של [OpenClaw](https://github.com/openclaw/openclaw) עם אבטחה משופרת, PostgreSQL מרובה-דיירים ויכולות תצפית ברמת ייצור.

🌐 **שפות:**
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

## מה מבדיל אותו

- **צוותי סוכנים ותזמור** — צוותים עם לוחות משימות משותפים, האברת משימות בין סוכנים (סינכרונית/אסינכרונית) וגילוי סוכנים היברידי
- **PostgreSQL מרובה-דיירים** — סביבות עבודה פר-משתמש, קבצי הקשר פר-משתמש, מפתחות API מוצפנים (AES-256-GCM), סשנים מבודדים
- **קובץ בינארי יחיד** — קובץ בינארי סטטי של Go בגודל ~25 MB, ללא Node.js runtime, הפעלה תוך פחות מ-1 שנייה, רץ על VPS בעלות $5
- **אבטחת ייצור** — מערכת הרשאות 5 שכבות (אימות שער ← מדיניות כלים גלובלית ← פר-סוכן ← פר-ערוץ ← בעלים בלבד) בתוספת הגבלת קצב, זיהוי הזרקת פרומפטים, הגנת SSRF, דפוסי דחיית מעטפת והצפנת AES-256-GCM
- **מעל 20 ספקי LLM** — Anthropic (HTTP+SSE מקורי עם שמירת פרומפטים), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP וכל נקודת קצה תואמת OpenAI
- **7 ערוצי הודעות** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — מצב חשיבה פר-ספק (תקציב טוקנים של Anthropic, מאמץ חשיבה של OpenAI, תקציב חשיבה של DashScope) עם תמיכה בסטרימינג
- **Heartbeat** — בדיקות מצב תקופתיות של סוכנים דרך רשימות HEARTBEAT.md עם הדחקה-בהצלחה, שעות פעילות, לוגיקת ניסיון חוזר ומשלוח לערוצים
- **תזמון ו-Cron** — ביטויי `at`, `every` ו-cron למשימות סוכן אוטומטיות עם מקביליות מבוססת-נתיב
- **תצפית** — מעקב שיחות LLM מובנה עם spans ומדדי מטמון פרומפטים, ייצוא אופציונלי של OpenTelemetry OTLP

## מערכת האקולוגית של Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| שפה             | TypeScript      | Rust     | Go       | **Go**                                  |
| גודל קובץ בינארי | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (בסיס) / **~36 MB** (+ OTel) |
| תמונת Docker    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (במנוחה)    | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| הפעלה           | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| חומרת יעד       | Mac Mini $599+  | קצה $10  | קצה $10  | **VPS $5+**                             |

| תכונה                      | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| מרובה-דיירים (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| שילוב MCP                  | — (משתמש ב-ACP)                      | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| צוותי סוכנים               | —                                    | —                                            | —                                     | ✅ לוח משימות + תיבת דואר      |
| הקשחת אבטחה                | ✅ (SSRF, מעבר נתיב, הזרקה)          | ✅ (ארגז חול, הגבלת קצב, הזרקה, צימוד)       | בסיסי (הגבלת סביבת עבודה, חסימת exec) | ✅ הגנה 5 שכבות               |
| תצפית OTel                 | ✅ (הרחבה אופציונלית)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (תג בנייה אופציונלי)   |
| שמירת פרומפטים             | —                                    | —                                            | —                                     | ✅ Anthropic + תואם-OpenAI     |
| גרף ידע                    | —                                    | —                                            | —                                     | ✅ חילוץ LLM + מעבר גרף        |
| מערכת מיומנויות            | ✅ הטמעות/סמנטי                      | ✅ SKILL.md + TOML                           | ✅ בסיסי                              | ✅ BM25 + pgvector היברידי     |
| מתזמן מבוסס-נתיב           | ✅                                   | מקביליות מוגבלת                              | —                                     | ✅ (main/subagent/team/cron)   |
| ערוצי הודעות               | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| אפליקציות נלוות            | macOS, iOS, Android                  | Python SDK                                   | —                                     | לוח בקרה ווב                   |
| Canvas חי / קול            | ✅ (A2UI + TTS/STT)                  | —                                            | תמלול קולי                            | TTS (4 ספקים)                  |
| ספקי LLM                   | 10+                                  | 8 מקורי + 29 תואם                            | 13+                                   | **20+**                        |
| סביבות עבודה פר-משתמש      | ✅ (מבוסס-קבצים)                     | —                                            | —                                     | ✅ (PostgreSQL)                |
| סודות מוצפנים              | — (משתני סביבה בלבד)                 | ✅ ChaCha20-Poly1305                         | — (JSON טקסט פשוט)                    | ✅ AES-256-GCM במסד הנתונים    |

## ארכיטקטורה

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## התחלה מהירה

**דרישות מוקדמות:** Go 1.26+, PostgreSQL 18 עם pgvector, Docker (אופציונלי)

### מהמקור

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # אשף הגדרה אינטראקטיבי
source .env.local && ./goclaw
```

### עם Docker

```bash
# יצירת .env עם סודות שנוצרו אוטומטית
chmod +x prepare-env.sh && ./prepare-env.sh

# הוסף לפחות GOCLAW_*_API_KEY אחד ל-.env, ואז:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# לוח הבקרה הווב בכתובת http://localhost:3000
# בדיקת תקינות: curl http://localhost:18790/health
```

כאשר משתני הסביבה `GOCLAW_*_API_KEY` מוגדרים, השער מגדיר את עצמו אוטומטית ללא פרומפטים אינטראקטיביים — מזהה ספק, מריץ מיגרציות ומזרע נתוני ברירת מחדל.

> לגרסאות בנייה (OTel, Tailscale, Redis), תגי תמונת Docker ושכבות compose, ראה את [מדריך הפריסה](https://docs.goclaw.sh/#deploy-docker-compose).

## תזמור רב-סוכנים

GoClaw תומך בצוותי סוכנים ובהאברת משימות בין סוכנים — כל סוכן רץ עם הזהות, הכלים, ספק ה-LLM וקבצי ההקשר שלו.

### האברת משימות בין סוכנים

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| מצב | איך זה עובד | הכי מתאים ל |
|------|-------------|----------|
| **סינכרוני** | סוכן א' מבקש מסוכן ב' ו**ממתין** לתשובה | בדיקות מהירות, אימות עובדות |
| **אסינכרוני** | סוכן א' מבקש מסוכן ב' ו**ממשיך**. ב' מכריז מאוחר יותר | משימות ארוכות, דוחות, ניתוח מעמיק |

סוכנים מתקשרים דרך **קישורי הרשאה** מפורשים עם בקרת כיוון (`outbound`, `inbound`, `bidirectional`) ומגבלות מקביליות ברמת הקישור וברמת הסוכן.

### צוותי סוכנים

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **לוח משימות משותף** — יצירה, תפיסה, השלמה וחיפוש משימות עם תלויות `blocked_by`
- **תיבת דואר של הצוות** — הודעות ישירות עמית-לעמית ושידורים
- **כלים**: `team_tasks` לניהול משימות, `team_message` לתיבת הדואר

> לפרטי האברה, קישורי הרשאה ובקרת מקביליות, ראה את [תיעוד צוותי סוכנים](https://docs.goclaw.sh/#teams-what-are-teams).

## כלים מובנים

| כלי                | קבוצה         | תיאור                                                         |
| ------------------ | ------------- | ------------------------------------------------------------- |
| `read_file`        | fs            | קריאת תוכן קבצים (עם ניתוב FS וירטואלי)                      |
| `write_file`       | fs            | כתיבה/יצירת קבצים                                            |
| `edit_file`        | fs            | החלת עריכות ממוקדות על קבצים קיימים                          |
| `list_files`       | fs            | רישום תוכן ספריה                                             |
| `search`           | fs            | חיפוש תוכן קבצים לפי דפוס                                    |
| `glob`             | fs            | מציאת קבצים לפי דפוס glob                                    |
| `exec`             | runtime       | הרצת פקודות מעטפת (עם תהליך אישור)                           |
| `web_search`       | web           | חיפוש ברשת (Brave, DuckDuckGo)                               |
| `web_fetch`        | web           | שליפה וניתוח תוכן ווב                                        |
| `memory_search`    | memory        | חיפוש בזיכרון לטווח ארוך (FTS + וקטור)                       |
| `memory_get`       | memory        | אחזור רשומות זיכרון                                          |
| `skill_search`     | —             | חיפוש מיומנויות (BM25 + הטמעה היברידית)                     |
| `knowledge_graph_search` | memory  | חיפוש ישויות ומעבר קשרי גרף ידע                              |
| `create_image`     | media         | יצירת תמונות (DashScope, MiniMax)                            |
| `create_audio`     | media         | יצירת שמע (OpenAI, ElevenLabs, MiniMax, Suno)                |
| `create_video`     | media         | יצירת וידאו (MiniMax, Veo)                                   |
| `read_document`    | media         | קריאת מסמכים (Gemini File API, שרשרת ספקים)                  |
| `read_image`       | media         | ניתוח תמונות                                                 |
| `read_audio`       | media         | תמלול וניתוח שמע                                             |
| `read_video`       | media         | ניתוח וידאו                                                  |
| `message`          | messaging     | שליחת הודעות לערוצים                                         |
| `tts`              | —             | סינתזת Text-to-Speech                                        |
| `spawn`            | —             | הפעלת תת-סוכן                                               |
| `subagents`        | sessions      | שליטה בתת-סוכנים פעילים                                     |
| `team_tasks`       | teams         | לוח משימות משותף (רשימה, יצירה, תפיסה, השלמה, חיפוש)        |
| `team_message`     | teams         | תיבת דואר צוות (שליחה, שידור, קריאה)                        |
| `sessions_list`    | sessions      | רישום סשנים פעילים                                           |
| `sessions_history` | sessions      | צפייה בהיסטוריית סשנים                                       |
| `sessions_send`    | sessions      | שליחת הודעה לסשן                                             |
| `sessions_spawn`   | sessions      | הפעלת סשן חדש                                               |
| `session_status`   | sessions      | בדיקת מצב סשן                                               |
| `cron`             | automation    | תזמון וניהול משימות cron                                     |
| `gateway`          | automation    | ניהול שער                                                    |
| `browser`          | ui            | אוטומציה בדפדפן (ניווט, לחיצה, הקלדה, צילום מסך)            |
| `announce_queue`   | automation    | הכרזת תוצאות אסינכרוניות (להאברות אסינכרוניות)               |

## תיעוד

תיעוד מלא בכתובת **[docs.goclaw.sh](https://docs.goclaw.sh)** — או עיין במקור ב-[`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| קטע | נושאים |
|---------|--------|
| [תחילת עבודה](https://docs.goclaw.sh/#what-is-goclaw) | התקנה, התחלה מהירה, הגדרה, סיור בלוח הבקרה הווב |
| [מושגי יסוד](https://docs.goclaw.sh/#how-goclaw-works) | לולאת סוכן, סשנים, כלים, זיכרון, מרובה-דיירות |
| [סוכנים](https://docs.goclaw.sh/#creating-agents) | יצירת סוכנים, קבצי הקשר, אישיות, שיתוף וגישה |
| [ספקים](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 נוספים |
| [ערוצים](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [צוותי סוכנים](https://docs.goclaw.sh/#teams-what-are-teams) | צוותים, לוח משימות, הודעות, האברה והעברה |
| [מתקדם](https://docs.goclaw.sh/#custom-tools) | כלים מותאמים, MCP, מיומנויות, Cron, ארגז חול, Hooks, RBAC |
| [פריסה](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, מסד נתונים, אבטחה, תצפית, Tailscale |
| [עיון](https://docs.goclaw.sh/#cli-commands) | פקודות CLI, REST API, פרוטוקול WebSocket, משתני סביבה |

## בדיקות

```bash
go test ./...                                    # בדיקות יחידה
go test -v ./tests/integration/ -timeout 120s    # בדיקות אינטגרציה (דורש שער פעיל)
```

## מצב הפרויקט

ראה את [CHANGELOG.md](CHANGELOG.md) למצב תכונות מפורט כולל מה נבדק בייצור ומה עדיין בתהליך.

## תודות

GoClaw בנוי על בסיס הפרויקט המקורי [OpenClaw](https://github.com/openclaw/openclaw). אנו אסירי תודה על הארכיטקטורה והחזון שהשרה פורט Go זה.

## רישיון

MIT
