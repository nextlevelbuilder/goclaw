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
  <a href="https://docs.goclaw.sh">Документация</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Быстрый старт</a> •
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

**GoClaw** — это многоагентный AI-шлюз, который подключает LLM-модели к вашим инструментам, каналам и данным — разворачивается как единый Go-бинарник без каких-либо сторонних зависимостей времени выполнения. Он оркестрирует команды агентов и межагентную делегацию через 20+ провайдеров LLM с полной мультиарендной изоляцией.

Go-порт проекта [OpenClaw](https://github.com/openclaw/openclaw) с расширенной безопасностью, мультиарендным PostgreSQL и наблюдаемостью производственного уровня.

🌐 **Языки:**
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
## В чём отличие

- **Команды агентов и оркестрация** — Команды с общими досками задач, межагентная делегация (синхронная/асинхронная) и гибридное обнаружение агентов
- **Мультиарендный PostgreSQL** — Отдельные рабочие пространства для каждого пользователя, контекстные файлы на пользователя, зашифрованные API-ключи (AES-256-GCM), изолированные сессии
- **Единый бинарник** — ~25 МБ статический Go-бинарник, без Node.js, запуск менее чем за 1 с, работает на VPS за $5
- **Безопасность производственного уровня** — 5-уровневая система прав (аутентификация шлюза → глобальная политика инструментов → на агента → на канал → только для владельца) плюс ограничение запросов, обнаружение prompt-инъекций, защита от SSRF, запрещённые shell-паттерны и шифрование AES-256-GCM
- **20+ провайдеров LLM** — Anthropic (нативный HTTP+SSE с кэшированием промптов), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP и любой OpenAI-совместимый эндпоинт
- **7 каналов обмена сообщениями** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Режим thinking на каждого провайдера (бюджет токенов Anthropic, усилия рассуждения OpenAI, бюджет мышления DashScope) с поддержкой стриминга
- **Heartbeat** — Периодические проверки агентов через чек-листы HEARTBEAT.md с подавлением при успехе, активными часами, логикой повторных попыток и доставкой в канал
- **Планировщик и cron** — Выражения `at`, `every` и cron для автоматизированных задач агентов с параллелизмом на основе очередей
- **Наблюдаемость** — Встроенная трассировка LLM-вызовов со спанами и метриками кэша промптов, опциональный экспорт OpenTelemetry OTLP

## Экосистема Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Язык            | TypeScript      | Rust     | Go       | **Go**                                  |
| Размер бинарника | 28 МБ + Node.js | 3,4 МБ   | ~8 МБ    | **~25 МБ** (базовый) / **~36 МБ** (+ OTel) |
| Docker-образ    | —               | —        | —        | **~50 МБ** (Alpine)                     |
| ОЗУ (простой)   | > 1 ГБ          | < 5 МБ   | < 10 МБ  | **~35 МБ**                              |
| Запуск          | > 5 с           | < 10 мс  | < 1 с    | **< 1 с**                               |
| Целевое железо  | Mac Mini от $599 | Edge за $10 | Edge за $10 | **VPS от $5**                       |

| Функция                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Мультиаренда (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| Интеграция MCP             | — (использует ACP)                   | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Команды агентов            | —                                    | —                                            | —                                     | ✅ Доска задач + почтовый ящик |
| Усиление безопасности      | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Базовая (ограничение workspace, запрет exec) | ✅ 5-уровневая защита    |
| Наблюдаемость OTel         | ✅ (опциональное расширение)         | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (опциональный build tag) |
| Кэширование промптов       | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Граф знаний                | —                                    | —                                            | —                                     | ✅ Извлечение LLM + обход      |
| Система навыков            | ✅ Embeddings/семантическая          | ✅ SKILL.md + TOML                           | ✅ Базовая                            | ✅ BM25 + pgvector гибрид      |
| Планировщик с очередями    | ✅                                   | Ограниченный параллелизм                     | —                                     | ✅ (main/subagent/team/cron)   |
| Каналы сообщений           | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Сопутствующие приложения   | macOS, iOS, Android                  | Python SDK                                   | —                                     | Веб-дэшборд                    |
| Live Canvas / Голос        | ✅ (A2UI + TTS/STT)                  | —                                            | Транскрипция голоса                   | TTS (4 провайдера)             |
| Провайдеры LLM             | 10+                                  | 8 нативных + 29 совместимых                  | 13+                                   | **20+**                        |
| Рабочие пространства на пользователя | ✅ (файловые)             | —                                            | —                                     | ✅ (PostgreSQL)                |
| Зашифрованные секреты      | — (только env-переменные)            | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM в БД            |

## Архитектура

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Быстрый старт

**Требования:** Go 1.26+, PostgreSQL 18 с pgvector, Docker (опционально)

### Из исходного кода

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Интерактивный мастер настройки
source .env.local && ./goclaw
```

### С Docker

```bash
# Генерация .env с автоматически созданными секретами
chmod +x prepare-env.sh && ./prepare-env.sh

# Добавьте как минимум один GOCLAW_*_API_KEY в .env, затем:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Веб-дэшборд: http://localhost:3000
# Проверка работоспособности: curl http://localhost:18790/health
```

Если переменные окружения `GOCLAW_*_API_KEY` заданы, шлюз автоматически настраивается без интерактивных запросов — определяет провайдера, выполняет миграции и инициализирует данные по умолчанию.

> Варианты сборки (OTel, Tailscale, Redis), теги Docker-образов и дополнительные compose-конфигурации см. в [Руководстве по развёртыванию](https://docs.goclaw.sh/#deploy-docker-compose).

## Многоагентная оркестрация

GoClaw поддерживает команды агентов и межагентную делегацию — каждый агент работает со своей идентичностью, инструментами, провайдером LLM и контекстными файлами.

### Делегация агентов

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Режим | Принцип работы | Лучше всего подходит для |
|-------|----------------|--------------------------|
| **Синхронный** | Агент A обращается к агенту B и **ждёт** ответа | Быстрые запросы, проверка фактов |
| **Асинхронный** | Агент A обращается к агенту B и **продолжает работу**. B сообщает позже | Длинные задачи, отчёты, глубокий анализ |

Агенты общаются через явные **ссылки-разрешения** с управлением направлением (`outbound`, `inbound`, `bidirectional`) и ограничениями параллелизма как на уровне ссылки, так и на уровне агента.

### Команды агентов

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Общая доска задач** — Создание, принятие, завершение, поиск задач с зависимостями `blocked_by`
- **Командный почтовый ящик** — Прямой обмен сообщениями между участниками и широковещательная рассылка
- **Инструменты**: `team_tasks` для управления задачами, `team_message` для почтового ящика

> Подробности о делегации, ссылках-разрешениях и управлении параллелизмом см. в [документации по командам агентов](https://docs.goclaw.sh/#teams-what-are-teams).

## Встроенные инструменты

| Инструмент         | Группа        | Описание                                                     |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Чтение содержимого файлов (с маршрутизацией виртуальной ФС)  |
| `write_file`       | fs            | Запись/создание файлов                                       |
| `edit_file`        | fs            | Точечное редактирование существующих файлов                  |
| `list_files`       | fs            | Список содержимого директории                                |
| `search`           | fs            | Поиск содержимого файлов по шаблону                          |
| `glob`             | fs            | Поиск файлов по glob-шаблону                                 |
| `exec`             | runtime       | Выполнение shell-команд (с рабочим процессом подтверждения)  |
| `web_search`       | web           | Поиск в интернете (Brave, DuckDuckGo)                        |
| `web_fetch`        | web           | Получение и парсинг веб-контента                             |
| `memory_search`    | memory        | Поиск в долгосрочной памяти (FTS + vector)                   |
| `memory_get`       | memory        | Извлечение записей из памяти                                 |
| `skill_search`     | —             | Поиск навыков (гибрид BM25 + embedding)                      |
| `knowledge_graph_search` | memory  | Поиск сущностей и обход связей графа знаний                  |
| `create_image`     | media         | Генерация изображений (DashScope, MiniMax)                   |
| `create_audio`     | media         | Генерация аудио (OpenAI, ElevenLabs, MiniMax, Suno)          |
| `create_video`     | media         | Генерация видео (MiniMax, Veo)                               |
| `read_document`    | media         | Чтение документов (Gemini File API, цепочка провайдеров)     |
| `read_image`       | media         | Анализ изображений                                           |
| `read_audio`       | media         | Транскрипция и анализ аудио                                  |
| `read_video`       | media         | Анализ видео                                                 |
| `message`          | messaging     | Отправка сообщений в каналы                                  |
| `tts`              | —             | Синтез речи (Text-to-Speech)                                 |
| `spawn`            | —             | Запуск субагента                                             |
| `subagents`        | sessions      | Управление запущенными субагентами                           |
| `team_tasks`       | teams         | Общая доска задач (список, создание, принятие, завершение, поиск) |
| `team_message`     | teams         | Командный почтовый ящик (отправка, рассылка, чтение)         |
| `sessions_list`    | sessions      | Список активных сессий                                       |
| `sessions_history` | sessions      | Просмотр истории сессий                                      |
| `sessions_send`    | sessions      | Отправка сообщения в сессию                                  |
| `sessions_spawn`   | sessions      | Запуск новой сессии                                          |
| `session_status`   | sessions      | Проверка статуса сессии                                      |
| `cron`             | automation    | Планирование и управление cron-заданиями                     |
| `gateway`          | automation    | Администрирование шлюза                                      |
| `browser`          | ui            | Автоматизация браузера (навигация, клик, ввод, скриншот)     |
| `announce_queue`   | automation    | Объявление асинхронных результатов (для асинхронных делегаций) |

## Документация

Полная документация на **[docs.goclaw.sh](https://docs.goclaw.sh)** — или просматривайте исходники в [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Раздел | Темы |
|--------|------|
| [Начало работы](https://docs.goclaw.sh/#what-is-goclaw) | Установка, Быстрый старт, Конфигурация, Обзор веб-дэшборда |
| [Основные концепции](https://docs.goclaw.sh/#how-goclaw-works) | Цикл агента, Сессии, Инструменты, Память, Мультиаренда |
| [Агенты](https://docs.goclaw.sh/#creating-agents) | Создание агентов, Контекстные файлы, Личность, Общий доступ |
| [Провайдеры](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 других |
| [Каналы](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Команды агентов](https://docs.goclaw.sh/#teams-what-are-teams) | Команды, Доска задач, Обмен сообщениями, Делегация и передача управления |
| [Расширенное использование](https://docs.goclaw.sh/#custom-tools) | Пользовательские инструменты, MCP, Навыки, Cron, Sandbox, Хуки, RBAC |
| [Развёртывание](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, База данных, Безопасность, Наблюдаемость, Tailscale |
| [Справочник](https://docs.goclaw.sh/#cli-commands) | CLI-команды, REST API, WebSocket-протокол, Переменные окружения |

## Тестирование

```bash
go test ./...                                    # Юнит-тесты
go test -v ./tests/integration/ -timeout 120s    # Интеграционные тесты (требуется запущенный шлюз)
```

## Статус проекта

Подробный статус функций, включая то, что протестировано в продакшене и что ещё в разработке, см. в [CHANGELOG.md](CHANGELOG.md).

## Благодарности

GoClaw основан на оригинальном проекте [OpenClaw](https://github.com/openclaw/openclaw). Мы благодарны за архитектуру и видение, которые вдохновили этот Go-порт.

## Лицензия

MIT
