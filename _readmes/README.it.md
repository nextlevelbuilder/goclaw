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
  <a href="https://docs.goclaw.sh">Documentazione</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Avvio Rapido</a> •
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

**GoClaw** è un gateway AI multi-agente che connette gli LLM ai tuoi strumenti, canali e dati — distribuito come singolo binario Go senza dipendenze runtime. Orchestra team di agenti e deleghe inter-agente su 20+ provider LLM con completo isolamento multi-tenant.

Un port Go di [OpenClaw](https://github.com/openclaw/openclaw) con sicurezza migliorata, PostgreSQL multi-tenant e osservabilità di livello produzione.

🌐 **Lingue:**
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

## Cosa lo Rende Diverso

- **Team di Agenti e Orchestrazione** — Team con bacheche delle attività condivise, delega inter-agente (sincrona/asincrona) e scoperta ibrida degli agenti
- **PostgreSQL Multi-Tenant** — Workspace per utente, file di contesto per utente, chiavi API cifrate (AES-256-GCM), sessioni isolate
- **Binario Singolo** — ~25 MB binario Go statico, nessun runtime Node.js, avvio in <1s, funziona su un VPS da $5
- **Sicurezza di Produzione** — Sistema di permessi a 5 livelli (autenticazione gateway → policy globale degli strumenti → per-agente → per-canale → solo proprietario) più limitazione della frequenza, rilevamento di prompt injection, protezione SSRF, pattern di blocco shell e cifratura AES-256-GCM
- **20+ Provider LLM** — Anthropic (HTTP+SSE nativo con caching dei prompt), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP e qualsiasi endpoint compatibile con OpenAI
- **7 Canali di Messaggistica** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Modalità di pensiero per provider (token di budget Anthropic, sforzo di ragionamento OpenAI, budget di pensiero DashScope) con supporto allo streaming
- **Heartbeat** — Check-in periodici degli agenti tramite checklist HEARTBEAT.md con soppressione in caso di OK, ore attive, logica di retry e consegna sul canale
- **Pianificazione e Cron** — Espressioni `at`, `every` e cron per attività automatizzate degli agenti con concorrenza basata su corsie
- **Osservabilità** — Tracciamento integrato delle chiamate LLM con span e metriche di cache dei prompt, esportazione OTLP OpenTelemetry opzionale

## Ecosistema Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Linguaggio      | TypeScript      | Rust     | Go       | **Go**                                  |
| Dimensione binario | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Immagine Docker | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (inattivo)  | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Avvio           | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Hardware target | Mac Mini $599+  | edge $10 | edge $10 | **VPS $5+**                             |

| Funzionalità                  | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| ----------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)     | —                                    | —                                            | —                                     | ✅                             |
| Integrazione MCP              | — (usa ACP)                          | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Team di agenti                | —                                    | —                                            | —                                     | ✅ Bacheca attività + mailbox  |
| Sicurezza rafforzata          | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Base (restrizione workspace, exec deny) | ✅ Difesa a 5 livelli         |
| Osservabilità OTel            | ✅ (estensione opzionale)            | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (tag build opzionale)  |
| Caching dei prompt            | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Grafo della conoscenza        | —                                    | —                                            | —                                     | ✅ Estrazione LLM + traversal  |
| Sistema di skill              | ✅ Embedding/semantico               | ✅ SKILL.md + TOML                           | ✅ Base                               | ✅ BM25 + pgvector ibrido      |
| Scheduler basato su corsie    | ✅                                   | Concorrenza limitata                         | —                                     | ✅ (main/subagent/team/cron)   |
| Canali di messaggistica       | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| App companion                 | macOS, iOS, Android                  | Python SDK                                   | —                                     | Dashboard web                  |
| Live Canvas / Voce            | ✅ (A2UI + TTS/STT)                  | —                                            | Trascrizione vocale                   | TTS (4 provider)               |
| Provider LLM                  | 10+                                  | 8 nativi + 29 compat                         | 13+                                   | **20+**                        |
| Workspace per utente          | ✅ (basato su file)                  | —                                            | —                                     | ✅ (PostgreSQL)                |
| Segreti cifrati               | — (solo variabili env)               | ✅ ChaCha20-Poly1305                         | — (JSON in chiaro)                    | ✅ AES-256-GCM nel DB          |

## Architettura

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Avvio Rapido

**Prerequisiti:** Go 1.26+, PostgreSQL 18 con pgvector, Docker (opzionale)

### Dal Sorgente

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Procedura guidata di configurazione interattiva
source .env.local && ./goclaw
```

### Con Docker

```bash
# Genera .env con segreti auto-generati
chmod +x prepare-env.sh && ./prepare-env.sh

# Aggiungi almeno un GOCLAW_*_API_KEY a .env, poi:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Web Dashboard su http://localhost:3000
# Health check: curl http://localhost:18790/health
```

Quando le variabili d'ambiente `GOCLAW_*_API_KEY` sono impostate, il gateway esegue l'onboarding automaticamente senza prompt interattivi — rileva il provider, esegue le migrazioni e inizializza i dati predefiniti.

> Per varianti di build (OTel, Tailscale, Redis), tag immagini Docker e overlay compose, consulta la [Guida al Deployment](https://docs.goclaw.sh/#deploy-docker-compose).

## Orchestrazione Multi-Agente

GoClaw supporta team di agenti e delega inter-agente — ogni agente opera con la propria identità, strumenti, provider LLM e file di contesto.

### Delega degli Agenti

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Modalità | Come funziona | Ideale per |
|----------|---------------|------------|
| **Sincrona** | L'agente A chiede all'agente B e **attende** la risposta | Ricerche rapide, verifiche di fatti |
| **Asincrona** | L'agente A chiede all'agente B e **prosegue**. B annuncia il risultato in seguito | Compiti lunghi, report, analisi approfondite |

Gli agenti comunicano tramite **link di permesso** espliciti con controllo della direzione (`outbound`, `inbound`, `bidirectional`) e limiti di concorrenza sia a livello di link che di agente.

### Team di Agenti

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Bacheca delle attività condivisa** — Crea, prendi in carico, completa e cerca attività con dipendenze `blocked_by`
- **Mailbox del team** — Messaggistica diretta tra pari e broadcast
- **Strumenti**: `team_tasks` per la gestione delle attività, `team_message` per la mailbox

> Per i dettagli sulla delega, i link di permesso e il controllo della concorrenza, consulta la [documentazione sui Team di Agenti](https://docs.goclaw.sh/#teams-what-are-teams).

## Strumenti Integrati

| Strumento          | Gruppo        | Descrizione                                                  |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Legge il contenuto dei file (con routing FS virtuale)        |
| `write_file`       | fs            | Scrive/crea file                                             |
| `edit_file`        | fs            | Applica modifiche mirate a file esistenti                    |
| `list_files`       | fs            | Elenca il contenuto delle directory                          |
| `search`           | fs            | Cerca nel contenuto dei file per pattern                     |
| `glob`             | fs            | Trova file tramite pattern glob                              |
| `exec`             | runtime       | Esegue comandi shell (con flusso di approvazione)            |
| `web_search`       | web           | Cerca sul web (Brave, DuckDuckGo)                            |
| `web_fetch`        | web           | Recupera e analizza contenuto web                            |
| `memory_search`    | memory        | Cerca nella memoria a lungo termine (FTS + vettoriale)       |
| `memory_get`       | memory        | Recupera voci dalla memoria                                  |
| `skill_search`     | —             | Cerca skill (ibrido BM25 + embedding)                        |
| `knowledge_graph_search` | memory  | Cerca entità e attraversa relazioni nel grafo della conoscenza |
| `create_image`     | media         | Generazione di immagini (DashScope, MiniMax)                 |
| `create_audio`     | media         | Generazione audio (OpenAI, ElevenLabs, MiniMax, Suno)        |
| `create_video`     | media         | Generazione video (MiniMax, Veo)                             |
| `read_document`    | media         | Lettura documenti (Gemini File API, catena provider)         |
| `read_image`       | media         | Analisi di immagini                                          |
| `read_audio`       | media         | Trascrizione e analisi audio                                 |
| `read_video`       | media         | Analisi video                                                |
| `message`          | messaging     | Invia messaggi ai canali                                     |
| `tts`              | —             | Sintesi Text-to-Speech                                       |
| `spawn`            | —             | Avvia un subagente                                           |
| `subagents`        | sessions      | Controlla i subagenti in esecuzione                          |
| `team_tasks`       | teams         | Bacheca attività condivisa (elenca, crea, prendi in carico, completa, cerca) |
| `team_message`     | teams         | Mailbox del team (invia, broadcast, leggi)                   |
| `sessions_list`    | sessions      | Elenca le sessioni attive                                    |
| `sessions_history` | sessions      | Visualizza la cronologia delle sessioni                      |
| `sessions_send`    | sessions      | Invia un messaggio a una sessione                            |
| `sessions_spawn`   | sessions      | Avvia una nuova sessione                                     |
| `session_status`   | sessions      | Controlla lo stato della sessione                            |
| `cron`             | automation    | Pianifica e gestisce job cron                                |
| `gateway`          | automation    | Amministrazione del gateway                                  |
| `browser`          | ui            | Automazione del browser (naviga, clicca, digita, screenshot) |
| `announce_queue`   | automation    | Annuncio asincrono dei risultati (per deleghe asincrone)     |

## Documentazione

Documentazione completa su **[docs.goclaw.sh](https://docs.goclaw.sh)** — oppure sfoglia il sorgente in [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Sezione | Argomenti |
|---------|-----------|
| [Per Iniziare](https://docs.goclaw.sh/#what-is-goclaw) | Installazione, Avvio Rapido, Configurazione, Tour della Dashboard Web |
| [Concetti di Base](https://docs.goclaw.sh/#how-goclaw-works) | Loop degli Agenti, Sessioni, Strumenti, Memoria, Multi-Tenancy |
| [Agenti](https://docs.goclaw.sh/#creating-agents) | Creazione di Agenti, File di Contesto, Personalità, Condivisione e Accesso |
| [Provider](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 altri |
| [Canali](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Team di Agenti](https://docs.goclaw.sh/#teams-what-are-teams) | Team, Bacheca Attività, Messaggistica, Delega e Handoff |
| [Avanzato](https://docs.goclaw.sh/#custom-tools) | Strumenti Personalizzati, MCP, Skill, Cron, Sandbox, Hook, RBAC |
| [Deployment](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Database, Sicurezza, Osservabilità, Tailscale |
| [Riferimento](https://docs.goclaw.sh/#cli-commands) | Comandi CLI, REST API, Protocollo WebSocket, Variabili d'Ambiente |

## Testing

```bash
go test ./...                                    # Unit test
go test -v ./tests/integration/ -timeout 120s    # Integration test (richiede gateway in esecuzione)
```

## Stato del Progetto

Consulta [CHANGELOG.md](CHANGELOG.md) per lo stato dettagliato delle funzionalità, incluso ciò che è stato testato in produzione e ciò che è ancora in corso.

## Ringraziamenti

GoClaw è costruito sul progetto originale [OpenClaw](https://github.com/openclaw/openclaw). Siamo grati per l'architettura e la visione che hanno ispirato questo port Go.

## Licenza

MIT
