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
  <a href="https://docs.goclaw.sh">Documentatie</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Snel starten</a> •
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

**GoClaw** is een multi-agent AI gateway die LLM's verbindt met uw tools, kanalen en gegevens — uitgerold als één enkel Go-binary zonder runtime-afhankelijkheden. Het orkestreert agentteams en inter-agent-delegatie via 20+ LLM-providers met volledige multi-tenant isolatie.

Een Go-port van [OpenClaw](https://github.com/openclaw/openclaw) met verbeterde beveiliging, multi-tenant PostgreSQL en productieklare observeerbaarheid.

🌐 **Talen:**
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

## Wat maakt het anders

- **Agentteams & Orkestratie** — Teams met gedeelde taakborden, inter-agent-delegatie (synchroon/asynchroon) en hybride agentdetectie
- **Multi-Tenant PostgreSQL** — Werkruimtes per gebruiker, contextbestanden per gebruiker, versleutelde API-sleutels (AES-256-GCM), geïsoleerde sessies
- **Enkel Binary** — ~25 MB statisch Go-binary, geen Node.js-runtime, <1 s opstartijd, draait op een VPS van $5
- **Productiebeveiliging** — 5-laags rechtensysteem (gateway-authenticatie → globaal toolbeleid → per agent → per kanaal → alleen eigenaar) plus snelheidsbeperking, detectie van prompt-injectie, SSRF-bescherming, shell-weigerings­patronen en AES-256-GCM-versleuteling
- **20+ LLM-providers** — Anthropic (native HTTP+SSE met prompt-caching), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP en elk OpenAI-compatibel eindpunt
- **7 Berichtenkanalen** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Denkmodus per provider (Anthropic-budgettokens, OpenAI-redeneerinspanning, DashScope-denkbudget) met streamingondersteuning
- **Heartbeat** — Periodieke agent-check-ins via HEARTBEAT.md-checklists met onderdrukken-bij-OK, actieve uren, herproberinglogica en kanaallevering
- **Plannen & Cron** — `at`-, `every`- en cron-expressies voor geautomatiseerde agenttaken met op rijstroken gebaseerde gelijktijdigheid
- **Observeerbaarheid** — Ingebouwde LLM-aanroeptracering met spans en prompt-cachemetrics, optionele OpenTelemetry OTLP-export

## Claw-ecosysteem

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Taal            | TypeScript      | Rust     | Go       | **Go**                                  |
| Binary-grootte  | 28 MB + Node.js | 3,4 MB   | ~8 MB    | **~25 MB** (basis) / **~36 MB** (+ OTel) |
| Docker-image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (inactief)  | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Opstartijd      | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Doelhardware    | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| Functie                         | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| ------------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)       | —                                    | —                                            | —                                     | ✅                             |
| MCP-integratie                  | — (gebruikt ACP)                     | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Agentteams                      | —                                    | —                                            | —                                     | ✅ Taakbord + postvak          |
| Beveiligingsverharding          | ✅ (SSRF, padtraversal, injectie)    | ✅ (sandbox, snelheidsbeperking, injectie, koppeling) | Basis (werkruimtebeperking, exec-weigering) | ✅ 5-laagse verdediging        |
| OTel-observeerbaarheid          | ✅ (opt-in extensie)                 | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build-tag)     |
| Prompt-caching                  | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Kennisgraaf                     | —                                    | —                                            | —                                     | ✅ LLM-extractie + doorloop    |
| Vaardigheidssysteem             | ✅ Embeddings/semantisch             | ✅ SKILL.md + TOML                           | ✅ Basis                              | ✅ BM25 + pgvector hybride     |
| Op rijstroken gebaseerde planner | ✅                                  | Begrensde gelijktijdigheid                   | —                                     | ✅ (main/subagent/team/cron)   |
| Berichtenkanalen                | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Companion-apps                  | macOS, iOS, Android                  | Python SDK                                   | —                                     | Webdashboard                   |
| Live Canvas / Spraak            | ✅ (A2UI + TTS/STT)                  | —                                            | Spraaktranscriptie                    | TTS (4 providers)              |
| LLM-providers                   | 10+                                  | 8 native + 29 compat                         | 13+                                   | **20+**                        |
| Werkruimtes per gebruiker       | ✅ (bestandsgebaseerd)               | —                                            | —                                     | ✅ (PostgreSQL)                |
| Versleutelde geheimen           | — (alleen omgevingsvariabelen)       | ✅ ChaCha20-Poly1305                         | — (gewone tekst JSON)                 | ✅ AES-256-GCM in DB           |

## Architectuur

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Snel starten

**Vereisten:** Go 1.26+, PostgreSQL 18 met pgvector, Docker (optioneel)

### Vanuit broncode

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interactieve installatiewizard
source .env.local && ./goclaw
```

### Met Docker

```bash
# Genereer .env met automatisch gegenereerde geheimen
chmod +x prepare-env.sh && ./prepare-env.sh

# Voeg minimaal één GOCLAW_*_API_KEY toe aan .env, dan:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Webdashboard op http://localhost:3000
# Statuscontrole: curl http://localhost:18790/health
```

Wanneer `GOCLAW_*_API_KEY`-omgevingsvariabelen zijn ingesteld, voert de gateway automatisch de installatie uit zonder interactieve aanwijzingen — detecteert de provider, voert migraties uit en seeded standaardgegevens.

> Voor buildvarianten (OTel, Tailscale, Redis), Docker-imagetags en compose-overlays, zie de [Implementatiegids](https://docs.goclaw.sh/#deploy-docker-compose).

## Multi-agent orkestratie

GoClaw ondersteunt agentteams en inter-agent-delegatie — elke agent draait met een eigen identiteit, tools, LLM-provider en contextbestanden.

### Agent-delegatie

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Modus | Hoe het werkt | Het beste voor |
|-------|---------------|----------------|
| **Synchroon** | Agent A vraagt Agent B en **wacht** op het antwoord | Snelle opzoekingen, feitencontroles |
| **Asynchroon** | Agent A vraagt Agent B en **gaat door**. B meldt later | Lange taken, rapporten, diepgaande analyses |

Agents communiceren via expliciete **rechtenkoppelingen** met richtingscontrole (`outbound`, `inbound`, `bidirectional`) en gelijktijdigheidsbeperkingen op zowel koppeling- als agentniveau.

### Agentteams

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Gedeeld taakbord** — Taken aanmaken, claimen, voltooien en zoeken met `blocked_by`-afhankelijkheden
- **Team-postvak** — Directe peer-to-peer berichten en uitzendboodschappen
- **Tools**: `team_tasks` voor taakbeheer, `team_message` voor het postvak

> Voor delegatiedetails, rechtenkoppelingen en gelijktijdigheidscontrole, zie de [Agentteams-documentatie](https://docs.goclaw.sh/#teams-what-are-teams).

## Ingebouwde tools

| Tool               | Groep         | Beschrijving                                                  |
| ------------------ | ------------- | ------------------------------------------------------------- |
| `read_file`        | fs            | Bestandsinhoud lezen (met virtuele FS-routing)                |
| `write_file`       | fs            | Bestanden schrijven/aanmaken                                  |
| `edit_file`        | fs            | Gerichte bewerkingen op bestaande bestanden toepassen         |
| `list_files`       | fs            | Mapinhoud weergeven                                           |
| `search`           | fs            | Bestandsinhoud doorzoeken op patroon                          |
| `glob`             | fs            | Bestanden zoeken op glob-patroon                              |
| `exec`             | runtime       | Shell-opdrachten uitvoeren (met goedkeuringsworkflow)         |
| `web_search`       | web           | Op het web zoeken (Brave, DuckDuckGo)                         |
| `web_fetch`        | web           | Webinhoud ophalen en verwerken                                |
| `memory_search`    | memory        | Langetermijngeheugen doorzoeken (FTS + vector)                |
| `memory_get`       | memory        | Geheugenitems ophalen                                         |
| `skill_search`     | —             | Vaardigheden zoeken (BM25 + embedding hybride)                |
| `knowledge_graph_search` | memory  | Entiteiten zoeken en kennisgraafrelaties doorlopen            |
| `create_image`     | media         | Afbeeldingen genereren (DashScope, MiniMax)                   |
| `create_audio`     | media         | Audio genereren (OpenAI, ElevenLabs, MiniMax, Suno)           |
| `create_video`     | media         | Video genereren (MiniMax, Veo)                                |
| `read_document`    | media         | Documenten lezen (Gemini File API, providerketen)             |
| `read_image`       | media         | Afbeeldingsanalyse                                            |
| `read_audio`       | media         | Audiotranscriptie en -analyse                                 |
| `read_video`       | media         | Videoanalyse                                                  |
| `message`          | messaging     | Berichten naar kanalen sturen                                 |
| `tts`              | —             | Tekst-naar-spraak-synthese                                    |
| `spawn`            | —             | Een subagent spawnen                                          |
| `subagents`        | sessions      | Actieve subagents beheren                                     |
| `team_tasks`       | teams         | Gedeeld taakbord (weergeven, aanmaken, claimen, voltooien, zoeken) |
| `team_message`     | teams         | Team-postvak (verzenden, uitzenden, lezen)                    |
| `sessions_list`    | sessions      | Actieve sessies weergeven                                     |
| `sessions_history` | sessions      | Sessiegeschiedenis bekijken                                   |
| `sessions_send`    | sessions      | Bericht naar een sessie sturen                                |
| `sessions_spawn`   | sessions      | Een nieuwe sessie spawnen                                     |
| `session_status`   | sessions      | Sessiestatus controleren                                      |
| `cron`             | automation    | Cron-taken plannen en beheren                                 |
| `gateway`          | automation    | Gateway-beheer                                                |
| `browser`          | ui            | Browserautomatisering (navigeren, klikken, typen, schermafbeelding) |
| `announce_queue`   | automation    | Asynchroon resultaat aankondigen (voor asynchrone delegaties) |

## Documentatie

Volledige documentatie op **[docs.goclaw.sh](https://docs.goclaw.sh)** — of blader door de broncode in [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Sectie | Onderwerpen |
|--------|-------------|
| [Aan de slag](https://docs.goclaw.sh/#what-is-goclaw) | Installatie, Snel starten, Configuratie, Rondleiding webdashboard |
| [Kernconcepten](https://docs.goclaw.sh/#how-goclaw-works) | Agent-loop, Sessies, Tools, Geheugen, Multi-tenancy |
| [Agents](https://docs.goclaw.sh/#creating-agents) | Agents aanmaken, Contextbestanden, Persoonlijkheid, Delen & Toegang |
| [Providers](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 meer |
| [Kanalen](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Agentteams](https://docs.goclaw.sh/#teams-what-are-teams) | Teams, Taakbord, Berichten, Delegatie & Overdracht |
| [Geavanceerd](https://docs.goclaw.sh/#custom-tools) | Aangepaste tools, MCP, Vaardigheden, Cron, Sandbox, Hooks, RBAC |
| [Implementatie](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Database, Beveiliging, Observeerbaarheid, Tailscale |
| [Referentie](https://docs.goclaw.sh/#cli-commands) | CLI-opdrachten, REST API, WebSocket Protocol, Omgevingsvariabelen |

## Testen

```bash
go test ./...                                    # Unittests
go test -v ./tests/integration/ -timeout 120s    # Integratietests (vereist actieve gateway)
```

## Projectstatus

Zie [CHANGELOG.md](CHANGELOG.md) voor gedetailleerde functiestatus, inclusief wat in productie is getest en wat nog in ontwikkeling is.

## Dankbetuigingen

GoClaw is gebouwd op het originele [OpenClaw](https://github.com/openclaw/openclaw)-project. We zijn dankbaar voor de architectuur en visie die deze Go-port hebben geïnspireerd.

## Licentie

MIT
