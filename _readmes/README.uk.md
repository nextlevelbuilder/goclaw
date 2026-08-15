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
  <a href="https://docs.goclaw.sh">Документація</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Швидкий старт</a> •
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

**GoClaw** — це мультиагентний AI-шлюз, що з'єднує LLM з вашими інструментами, каналами та даними — розгортається як єдиний Go-бінарник без зовнішніх залежностей часу виконання. Він оркеструє команди агентів і міжагентну делегацію через 20+ постачальників LLM з повною ізоляцією мультиорендарності.

Go-порт [OpenClaw](https://github.com/openclaw/openclaw) з посиленою безпекою, мультиорендарним PostgreSQL та спостережуваністю виробничого рівня.

🌐 **Мови:**
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

## Чим це відрізняється

- **Команди агентів та оркестрація** — Команди зі спільними дошками завдань, міжагентна делегація (синхронна/асинхронна) та гібридне виявлення агентів
- **Мультиорендарний PostgreSQL** — Окремі робочі простори для кожного користувача, файли контексту для кожного користувача, зашифровані API-ключі (AES-256-GCM), ізольовані сесії
- **Єдиний бінарник** — ~25 МБ статичний Go-бінарник, без Node.js-середовища виконання, запуск менш ніж за 1 секунду, працює на VPS за $5
- **Безпека виробничого рівня** — 5-рівнева система дозволів (автентифікація шлюзу → глобальна політика інструментів → на рівні агента → на рівні каналу → лише для власника), а також обмеження частоти запитів, виявлення ін'єкцій у промпт, захист від SSRF, шаблони заборони команд оболонки та шифрування AES-256-GCM
- **20+ постачальників LLM** — Anthropic (нативний HTTP+SSE з кешуванням промптів), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP та будь-який OpenAI-сумісний endpoint
- **7 каналів обміну повідомленнями** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Режим мислення для кожного постачальника (бюджет токенів Anthropic, зусилля міркування OpenAI, бюджет мислення DashScope) з підтримкою потокової передачі
- **Heartbeat** — Periodичні перевірки агентів через контрольні списки HEARTBEAT.md з придушенням при OK, активними годинами, логікою повторних спроб та доставкою через канали
- **Планування та Cron** — `at`, `every` та cron-вирази для автоматизованих завдань агентів з паралелізмом на основі черг
- **Спостережуваність** — Вбудоване трасування LLM-викликів зі спанами та метриками кешу промптів, необов'язковий експорт OpenTelemetry OTLP

## Екосистема Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Мова            | TypeScript      | Rust     | Go       | **Go**                                  |
| Розмір бінарника | 28 МБ + Node.js | 3,4 МБ   | ~8 МБ    | **~25 МБ** (базовий) / **~36 МБ** (+ OTel) |
| Docker-образ    | —               | —        | —        | **~50 МБ** (Alpine)                     |
| ОЗП (у спокої)  | > 1 ГБ          | < 5 МБ   | < 10 МБ  | **~35 МБ**                              |
| Запуск          | > 5 с           | < 10 мс  | < 1 с    | **< 1 с**                               |
| Цільове залізо  | Mac Mini від $599+ | Edge за $10 | Edge за $10 | **VPS від $5+**                   |

| Функція                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Мультиорендарність (PostgreSQL) | —                               | —                                            | —                                     | ✅                             |
| Інтеграція MCP             | — (використовує ACP)                 | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Команди агентів            | —                                    | —                                            | —                                     | ✅ Дошка завдань + поштова скринька |
| Посилення безпеки          | ✅ (SSRF, обхід шляху, ін'єкції)    | ✅ (пісочниця, обмеження частоти, ін'єкції, спарювання) | Базова (обмеження робочого простору, заборона exec) | ✅ 5-рівневий захист |
| Спостережуваність OTel     | ✅ (необов'язкове розширення)        | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (необов'язковий тег збірки) |
| Кешування промптів         | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Граф знань                 | —                                    | —                                            | —                                     | ✅ Витягування LLM + обхід     |
| Система навичок            | ✅ Вбудовування/семантика            | ✅ SKILL.md + TOML                           | ✅ Базова                             | ✅ BM25 + гібрид pgvector      |
| Планувальник на основі черг | ✅                                  | Обмежений паралелізм                         | —                                     | ✅ (main/subagent/team/cron)   |
| Канали обміну повідомленнями | 37+                                 | 15+                                          | 10+                                   | 7+                             |
| Супутні застосунки         | macOS, iOS, Android                  | Python SDK                                   | —                                     | Веб-панель                     |
| Live Canvas / Голос        | ✅ (A2UI + TTS/STT)                  | —                                            | Транскрипція голосу                   | TTS (4 постачальники)          |
| Постачальники LLM          | 10+                                  | 8 нативних + 29 сумісних                     | 13+                                   | **20+**                        |
| Робочі простори для кожного користувача | ✅ (на основі файлів)  | —                                            | —                                     | ✅ (PostgreSQL)                |
| Зашифровані секрети        | — (лише змінні середовища)           | ✅ ChaCha20-Poly1305                         | — (відкритий JSON)                    | ✅ AES-256-GCM у БД            |

## Архітектура

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Швидкий старт

**Передумови:** Go 1.26+, PostgreSQL 18 з pgvector, Docker (необов'язково)

### З вихідного коду

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Інтерактивний майстер налаштування
source .env.local && ./goclaw
```

### З Docker

```bash
# Генерація .env з автоматично згенерованими секретами
chmod +x prepare-env.sh && ./prepare-env.sh

# Додайте хоча б один GOCLAW_*_API_KEY до .env, потім:
make up

# Веб-панель за адресою http://localhost:18790
# Перевірка стану: curl http://localhost:18790/health
```

Якщо встановлені змінні середовища `GOCLAW_*_API_KEY`, шлюз виконує автоматичне налаштування без інтерактивних підказок — визначає постачальника, запускає міграції та заповнює початкові дані.

> Щодо варіантів збірки (OTel, Tailscale, Redis), тегів Docker-образів та compose-оверлеїв, дивіться [Посібник із розгортання](https://docs.goclaw.sh/#deploy-docker-compose).

## Мультиагентна оркестрація

GoClaw підтримує команди агентів та міжагентну делегацію — кожен агент працює зі своєю власною ідентичністю, інструментами, постачальником LLM та файлами контексту.

### Делегація агентів

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Режим | Як працює | Найкраще для |
|-------|-----------|--------------|
| **Синхронний** | Агент A запитує агента B і **чекає** на відповідь | Швидкі пошукові запити, перевірка фактів |
| **Асинхронний** | Агент A запитує агента B і **продовжує роботу**. B сповіщає пізніше | Тривалі завдання, звіти, глибокий аналіз |

Агенти спілкуються через явні **посилання дозволів** з керуванням напрямком (`outbound`, `inbound`, `bidirectional`) та обмеженнями паралелізму на рівні окремих посилань і агентів.

### Команди агентів

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Спільна дошка завдань** — Створення, захоплення, завершення та пошук завдань із залежностями `blocked_by`
- **Командна поштова скринька** — Пряме повідомлення між учасниками та широкомовні розсилки
- **Інструменти**: `team_tasks` для керування завданнями, `team_message` для поштової скриньки

> Щодо деталей делегації, посилань дозволів та керування паралелізмом, дивіться [документацію Команд агентів](https://docs.goclaw.sh/#teams-what-are-teams).

## Вбудовані інструменти

| Інструмент         | Група         | Опис                                                         |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Читання вмісту файлів (з маршрутизацією віртуальної ФС)     |
| `write_file`       | fs            | Запис/створення файлів                                       |
| `edit_file`        | fs            | Застосування цільових правок до існуючих файлів              |
| `list_files`       | fs            | Перегляд вмісту директорії                                   |
| `search`           | fs            | Пошук вмісту файлів за шаблоном                             |
| `glob`             | fs            | Пошук файлів за glob-шаблоном                               |
| `exec`             | runtime       | Виконання команд оболонки (з робочим процесом підтвердження) |
| `web_search`       | web           | Пошук в інтернеті (Brave, DuckDuckGo)                       |
| `web_fetch`        | web           | Отримання та розбір веб-вмісту                              |
| `memory_search`    | memory        | Пошук у довготривалій пам'яті (FTS + вектор)                |
| `memory_get`       | memory        | Отримання записів пам'яті                                   |
| `skill_search`     | —             | Пошук навичок (гібрид BM25 + вбудовування)                  |
| `knowledge_graph_search` | memory  | Пошук сутностей та обхід зв'язків графу знань               |
| `create_image`     | media         | Генерація зображень (DashScope, MiniMax)                    |
| `create_audio`     | media         | Генерація аудіо (OpenAI, ElevenLabs, MiniMax, Suno)         |
| `create_video`     | media         | Генерація відео (MiniMax, Veo)                              |
| `read_document`    | media         | Читання документів (Gemini File API, ланцюг постачальників)  |
| `read_image`       | media         | Аналіз зображень                                            |
| `read_audio`       | media         | Транскрипція та аналіз аудіо                                |
| `read_video`       | media         | Аналіз відео                                                |
| `message`          | messaging     | Відправка повідомлень у канали                               |
| `tts`              | —             | Синтез мовлення з тексту                                    |
| `spawn`            | —             | Запуск субагента                                            |
| `subagents`        | sessions      | Керування запущеними субагентами                            |
| `team_tasks`       | teams         | Спільна дошка завдань (перегляд, створення, захоплення, завершення, пошук) |
| `team_message`     | teams         | Командна поштова скринька (відправка, широкомовлення, читання) |
| `sessions_list`    | sessions      | Список активних сесій                                       |
| `sessions_history` | sessions      | Перегляд історії сесій                                      |
| `sessions_send`    | sessions      | Відправка повідомлення в сесію                              |
| `sessions_spawn`   | sessions      | Запуск нової сесії                                          |
| `session_status`   | sessions      | Перевірка стану сесії                                       |
| `cron`             | automation    | Планування та керування cron-завданнями                     |
| `gateway`          | automation    | Адміністрування шлюзу                                       |
| `browser`          | ui            | Автоматизація браузера (навігація, клік, введення, знімок екрана) |
| `announce_queue`   | automation    | Оголошення асинхронних результатів (для асинхронних делегацій) |

## Документація

Повна документація на **[docs.goclaw.sh](https://docs.goclaw.sh)** — або перегляньте вихідний код у [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Розділ | Теми |
|--------|------|
| [Початок роботи](https://docs.goclaw.sh/#what-is-goclaw) | Встановлення, Швидкий старт, Конфігурація, Огляд веб-панелі |
| [Основні концепції](https://docs.goclaw.sh/#how-goclaw-works) | Цикл агента, Сесії, Інструменти, Пам'ять, Мультиорендарність |
| [Агенти](https://docs.goclaw.sh/#creating-agents) | Створення агентів, Файли контексту, Особистість, Спільний доступ |
| [Постачальники](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 інших |
| [Канали](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Команди агентів](https://docs.goclaw.sh/#teams-what-are-teams) | Команди, Дошка завдань, Обмін повідомленнями, Делегація та передача |
| [Розширено](https://docs.goclaw.sh/#custom-tools) | Власні інструменти, MCP, Навички, Cron, Пісочниця, Хуки, RBAC |
| [Розгортання](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, База даних, Безпека, Спостережуваність, Tailscale |
| [Довідник](https://docs.goclaw.sh/#cli-commands) | Команди CLI, REST API, Протокол WebSocket, Змінні середовища |

## Тестування

```bash
go test ./...                                    # Модульні тести
go test -v ./tests/integration/ -timeout 120s    # Інтеграційні тести (потребує запущеного шлюзу)
```

## Статус проекту

Дивіться [CHANGELOG.md](CHANGELOG.md) щодо детального стану функцій, включаючи те, що вже протестовано у продакшні та що ще в процесі.

## Подяки

GoClaw побудований на основі оригінального проекту [OpenClaw](https://github.com/openclaw/openclaw). Ми вдячні за архітектуру та бачення, що надихнули цей Go-порт.

## Ліцензія

MIT
