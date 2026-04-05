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
  <a href="https://docs.goclaw.sh">Dokumentation</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Schnellstart</a> •
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

**GoClaw** ist ein Multi-Agenten-KI-Gateway, das LLMs mit Ihren Tools, Kanälen und Daten verbindet — als einzelne Go-Binary ohne Laufzeitabhängigkeiten bereitgestellt. Es orchestriert Agenten-Teams und agenten-übergreifende Delegation über 20+ LLM-Anbieter mit vollständiger Mandantenisolierung.

Ein Go-Port von [OpenClaw](https://github.com/openclaw/openclaw) mit verbesserter Sicherheit, mandantenfähigem PostgreSQL und produktionsreifer Beobachtbarkeit.

🌐 **Sprachen:**
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
## Was es besonders macht

- **Agenten-Teams & Orchestrierung** — Teams mit gemeinsamen Aufgaben-Boards, agenten-übergreifender Delegation (synchron/asynchron) und hybrider Agenten-Erkennung
- **Mandantenfähiges PostgreSQL** — Arbeitsbereiche pro Benutzer, Kontextdateien pro Benutzer, verschlüsselte API-Schlüssel (AES-256-GCM), isolierte Sitzungen
- **Single Binary** — ~25 MB statische Go-Binary, kein Node.js-Laufzeit, <1s Startzeit, läuft auf einem $5-VPS
- **Produktionssicherheit** — 5-schichtiges Berechtigungssystem (Gateway-Auth → globale Tool-Richtlinie → pro Agent → pro Kanal → nur Eigentümer) plus Rate Limiting, Prompt-Injection-Erkennung, SSRF-Schutz, Shell-Verweigerungsmuster und AES-256-GCM-Verschlüsselung
- **20+ LLM-Anbieter** — Anthropic (natives HTTP+SSE mit Prompt-Caching), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP und jeder OpenAI-kompatible Endpunkt
- **7 Messaging-Kanäle** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Anbieter-spezifischer Denkmodus (Anthropic Budget-Tokens, OpenAI Reasoning Effort, DashScope Thinking Budget) mit Streaming-Unterstützung
- **Heartbeat** — Regelmäßige Agenten-Check-ins über HEARTBEAT.md-Checklisten mit Unterdrückung bei OK, aktiven Stunden, Wiederholungslogik und Kanal-Zustellung
- **Planung & Cron** — `at`-, `every`- und Cron-Ausdrücke für automatisierte Agenten-Aufgaben mit spurbasierter Nebenläufigkeit
- **Beobachtbarkeit** — Integriertes LLM-Aufruf-Tracing mit Spans und Prompt-Cache-Metriken, optionaler OpenTelemetry OTLP-Export

## Das Claw-Ökosystem

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Sprache         | TypeScript      | Rust     | Go       | **Go**                                  |
| Binary-Größe    | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (Basis) / **~36 MB** (+ OTel) |
| Docker-Image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (inaktiv)   | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Startzeit       | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Zielhardware    | $599+ Mac Mini  | $10 Edge | $10 Edge | **$5 VPS+**                             |

| Funktion                   | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Mandantenfähig (PostgreSQL)| —                                    | —                                            | —                                     | ✅                             |
| MCP-Integration            | — (verwendet ACP)                    | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Agenten-Teams              | —                                    | —                                            | —                                     | ✅ Task Board + Postfach       |
| Sicherheitshärtung         | ✅ (SSRF, Pfad-Traversal, Injection) | ✅ (Sandbox, Rate Limit, Injection, Pairing) | Grundlegend (Arbeitsbereich, Exec-Sperre) | ✅ 5-schichtige Verteidigung   |
| OTel-Beobachtbarkeit       | ✅ (optionale Erweiterung)           | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (optionaler Build-Tag) |
| Prompt-Caching             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-kompatibel |
| Wissensgraph               | —                                    | —                                            | —                                     | ✅ LLM-Extraktion + Traversierung |
| Skill-System               | ✅ Embeddings/Semantisch             | ✅ SKILL.md + TOML                           | ✅ Grundlegend                        | ✅ BM25 + pgvector Hybrid      |
| Spurbasierter Scheduler    | ✅                                   | Begrenzte Nebenläufigkeit                    | —                                     | ✅ (main/subagent/team/cron)   |
| Messaging-Kanäle           | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Begleit-Apps               | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web-Dashboard                  |
| Live Canvas / Sprache      | ✅ (A2UI + TTS/STT)                  | —                                            | Sprach-Transkription                  | TTS (4 Anbieter)               |
| LLM-Anbieter               | 10+                                  | 8 nativ + 29 kompatibel                      | 13+                                   | **20+**                        |
| Arbeitsbereiche pro Nutzer | ✅ (dateibasiert)                    | —                                            | —                                     | ✅ (PostgreSQL)                |
| Verschlüsselte Geheimnisse | — (nur Umgebungsvariablen)           | ✅ ChaCha20-Poly1305                         | — (Klartext-JSON)                     | ✅ AES-256-GCM in DB           |

## Architektur

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Schnellstart

**Voraussetzungen:** Go 1.26+, PostgreSQL 18 mit pgvector, Docker (optional)

### Aus dem Quellcode

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interaktiver Einrichtungsassistent
source .env.local && ./goclaw
```

### Mit Docker

```bash
# .env mit automatisch generierten Geheimnissen erstellen
chmod +x prepare-env.sh && ./prepare-env.sh

# Mindestens einen GOCLAW_*_API_KEY in .env hinzufügen, dann:
make up

# Web-Dashboard unter http://localhost:18790
# Health-Check: curl http://localhost:18790/health
```

Wenn `GOCLAW_*_API_KEY`-Umgebungsvariablen gesetzt sind, führt das Gateway das Onboarding automatisch ohne interaktive Eingaben durch — erkennt den Anbieter, führt Migrationen aus und befüllt Standarddaten.

> Für Build-Varianten (OTel, Tailscale, Redis), Docker-Image-Tags und Compose-Overlays, siehe den [Deployment-Leitfaden](https://docs.goclaw.sh/#deploy-docker-compose).

## Multi-Agenten-Orchestrierung

GoClaw unterstützt Agenten-Teams und agenten-übergreifende Delegation — jeder Agent läuft mit seiner eigenen Identität, Tools, LLM-Anbieter und Kontextdateien.

### Agenten-Delegation

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Modus | Funktionsweise | Geeignet für |
|-------|----------------|--------------|
| **Synchron** | Agent A fragt Agent B und **wartet** auf die Antwort | Schnelle Abfragen, Faktenprüfungen |
| **Asynchron** | Agent A fragt Agent B und **macht weiter**. B kündigt das Ergebnis später an | Lange Aufgaben, Berichte, Tiefenanalysen |

Agenten kommunizieren über explizite **Berechtigungslinks** mit Richtungssteuerung (`outbound`, `inbound`, `bidirectional`) und Nebenläufigkeitslimits auf Link- und Agenten-Ebene.

### Agenten-Teams

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Gemeinsames Aufgaben-Board** — Aufgaben erstellen, beanspruchen, abschließen und durchsuchen mit `blocked_by`-Abhängigkeiten
- **Team-Postfach** — Direkte Peer-to-Peer-Nachrichten und Broadcasts
- **Tools**: `team_tasks` für Aufgabenverwaltung, `team_message` für das Postfach

> Für Details zu Delegation, Berechtigungslinks und Nebenläufigkeitskontrolle, siehe die [Agenten-Teams-Dokumentation](https://docs.goclaw.sh/#teams-what-are-teams).

## Integrierte Tools

| Tool               | Gruppe        | Beschreibung                                                  |
| ------------------ | ------------- | ------------------------------------------------------------- |
| `read_file`        | fs            | Dateiinhalt lesen (mit virtuellem FS-Routing)                 |
| `write_file`       | fs            | Dateien schreiben/erstellen                                   |
| `edit_file`        | fs            | Gezielte Bearbeitungen an vorhandenen Dateien vornehmen       |
| `list_files`       | fs            | Verzeichnisinhalt auflisten                                   |
| `search`           | fs            | Dateiinhalt nach Muster durchsuchen                           |
| `glob`             | fs            | Dateien nach Glob-Muster suchen                               |
| `exec`             | runtime       | Shell-Befehle ausführen (mit Genehmigungsworkflow)            |
| `web_search`       | web           | Im Web suchen (Brave, DuckDuckGo)                             |
| `web_fetch`        | web           | Webinhalt abrufen und parsen                                  |
| `memory_search`    | memory        | Langzeitgedächtnis durchsuchen (FTS + Vektor)                 |
| `memory_get`       | memory        | Gedächtniseinträge abrufen                                    |
| `skill_search`     | —             | Skills durchsuchen (BM25 + Embedding-Hybrid)                  |
| `knowledge_graph_search` | memory  | Entitäten suchen und Wissensgraph-Beziehungen traversieren    |
| `create_image`     | media         | Bildgenerierung (DashScope, MiniMax)                          |
| `create_audio`     | media         | Audiogenerierung (OpenAI, ElevenLabs, MiniMax, Suno)          |
| `create_video`     | media         | Videogenerierung (MiniMax, Veo)                               |
| `read_document`    | media         | Dokumente lesen (Gemini File API, Anbieter-Kette)             |
| `read_image`       | media         | Bildanalyse                                                   |
| `read_audio`       | media         | Audio-Transkription und -Analyse                              |
| `read_video`       | media         | Videoanalyse                                                  |
| `message`          | messaging     | Nachrichten an Kanäle senden                                  |
| `tts`              | —             | Text-to-Speech-Synthese                                       |
| `spawn`            | —             | Einen Subagenten starten                                      |
| `subagents`        | sessions      | Laufende Subagenten steuern                                   |
| `team_tasks`       | teams         | Gemeinsames Aufgaben-Board (auflisten, erstellen, beanspruchen, abschließen, suchen) |
| `team_message`     | teams         | Team-Postfach (senden, broadcast, lesen)                      |
| `sessions_list`    | sessions      | Aktive Sitzungen auflisten                                    |
| `sessions_history` | sessions      | Sitzungsverlauf anzeigen                                      |
| `sessions_send`    | sessions      | Nachricht an eine Sitzung senden                              |
| `sessions_spawn`   | sessions      | Eine neue Sitzung starten                                     |
| `session_status`   | sessions      | Sitzungsstatus prüfen                                         |
| `cron`             | automation    | Cron-Jobs planen und verwalten                                |
| `gateway`          | automation    | Gateway-Administration                                        |
| `browser`          | ui            | Browser-Automatisierung (navigieren, klicken, tippen, Screenshot) |
| `announce_queue`   | automation    | Asynchrone Ergebnisankündigung (für asynchrone Delegationen)  |

## Dokumentation

Vollständige Dokumentation unter **[docs.goclaw.sh](https://docs.goclaw.sh)** — oder den Quellcode unter [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs) durchsuchen.

| Abschnitt | Themen |
|-----------|--------|
| [Erste Schritte](https://docs.goclaw.sh/#what-is-goclaw) | Installation, Schnellstart, Konfiguration, Web-Dashboard-Tour |
| [Grundkonzepte](https://docs.goclaw.sh/#how-goclaw-works) | Agenten-Loop, Sitzungen, Tools, Gedächtnis, Mandantenfähigkeit |
| [Agenten](https://docs.goclaw.sh/#creating-agents) | Agenten erstellen, Kontextdateien, Persönlichkeit, Teilen & Zugriff |
| [Anbieter](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 weitere |
| [Kanäle](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Agenten-Teams](https://docs.goclaw.sh/#teams-what-are-teams) | Teams, Aufgaben-Board, Messaging, Delegation & Übergabe |
| [Erweitert](https://docs.goclaw.sh/#custom-tools) | Benutzerdefinierte Tools, MCP, Skills, Cron, Sandbox, Hooks, RBAC |
| [Deployment](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Datenbank, Sicherheit, Beobachtbarkeit, Tailscale |
| [Referenz](https://docs.goclaw.sh/#cli-commands) | CLI-Befehle, REST-API, WebSocket-Protokoll, Umgebungsvariablen |

## Testen

```bash
go test ./...                                    # Unit-Tests
go test -v ./tests/integration/ -timeout 120s    # Integrationstests (erfordert laufendes Gateway)
```

## Projektstatus

Siehe [CHANGELOG.md](CHANGELOG.md) für detaillierten Funktionsstatus, einschließlich was in der Produktion getestet wurde und was noch in Bearbeitung ist.

## Danksagungen

GoClaw basiert auf dem ursprünglichen [OpenClaw](https://github.com/openclaw/openclaw)-Projekt. Wir sind dankbar für die Architektur und Vision, die diesen Go-Port inspiriert hat.

## Lizenz

MIT
