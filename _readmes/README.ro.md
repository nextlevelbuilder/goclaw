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
  <a href="https://docs.goclaw.sh">Documentație</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Pornire Rapidă</a> •
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

**GoClaw** este un gateway AI multi-agent care conectează LLM-uri la instrumentele, canalele și datele tale — implementat ca un singur binar Go fără dependențe de rulare. Orchestrează echipe de agenți și delegare inter-agent prin 20+ furnizori LLM cu izolare completă multi-tenant.

Un port Go al [OpenClaw](https://github.com/openclaw/openclaw) cu securitate îmbunătățită, PostgreSQL multi-tenant și observabilitate la nivel de producție.

🌐 **Limbi:**
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

## Ce Îl Diferențiază

- **Echipe de Agenți și Orchestrare** — Echipe cu panouri de sarcini partajate, delegare inter-agent (sincron/asincron) și descoperire hibridă a agenților
- **PostgreSQL Multi-Tenant** — Spații de lucru per utilizator, fișiere de context per utilizator, chei API criptate (AES-256-GCM), sesiuni izolate
- **Binar Unic** — Binar Go static de ~25 MB, fără Node.js runtime, pornire în <1s, rulează pe un VPS de $5
- **Securitate la Nivel de Producție** — Sistem de permisiuni în 5 straturi (autentificare gateway → politică globală instrumente → per-agent → per-canal → doar proprietar), plus limitare rată, detectare injecție prompturi, protecție SSRF, tipare de refuzare comenzi shell și criptare AES-256-GCM
- **20+ Furnizori LLM** — Anthropic (HTTP+SSE nativ cu cache prompturi), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP și orice endpoint compatibil OpenAI
- **7 Canale de Mesagerie** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Mod de gândire per furnizor (tokeni buget Anthropic, efort de raționament OpenAI, buget de gândire DashScope) cu suport streaming
- **Heartbeat** — Verificări periodice ale agenților prin liste de verificare HEARTBEAT.md cu suprimare la OK, ore active, logică de reîncercare și livrare pe canal
- **Programare și Cron** — Expresii `at`, `every` și cron pentru sarcini automate ale agenților cu concurență bazată pe benzi
- **Observabilitate** — Urmărire integrată a apelurilor LLM cu intervale și metrici cache prompturi, export OTLP OpenTelemetry opțional

## Ecosistemul Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Limbaj          | TypeScript      | Rust     | Go       | **Go**                                  |
| Dimensiune binar | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (de bază) / **~36 MB** (+ OTel) |
| Imagine Docker  | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (inactiv)   | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Pornire         | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Hardware țintă  | Mac Mini $599+  | edge $10 | edge $10 | **VPS $5+**                             |

| Funcționalitate               | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| ----------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)     | —                                    | —                                            | —                                     | ✅                             |
| Integrare MCP                 | — (folosește ACP)                    | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Echipe de agenți              | —                                    | —                                            | —                                     | ✅ Panou sarcini + cutie poștală |
| Întărire securitate           | ✅ (SSRF, traversare cale, injecție) | ✅ (sandbox, limitare rată, injecție, asociere) | De bază (restricție spațiu lucru, refuz exec) | ✅ Apărare în 5 straturi |
| Observabilitate OTel          | ✅ (extensie opțională)              | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (etichetă build opțională) |
| Cache prompturi               | —                                    | —                                            | —                                     | ✅ Anthropic + compat OpenAI   |
| Graf de cunoaștere            | —                                    | —                                            | —                                     | ✅ Extragere LLM + traversare  |
| Sistem skill-uri              | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ De bază                            | ✅ BM25 + hibrid pgvector      |
| Programator bazat pe benzi    | ✅                                   | Concurență limitată                          | —                                     | ✅ (main/subagent/team/cron)   |
| Canale de mesagerie           | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Aplicații companion           | macOS, iOS, Android                  | Python SDK                                   | —                                     | Tablou de bord web             |
| Canvas Live / Voce            | ✅ (A2UI + TTS/STT)                  | —                                            | Transcriere voce                      | TTS (4 furnizori)              |
| Furnizori LLM                 | 10+                                  | 8 nativi + 29 compat                         | 13+                                   | **20+**                        |
| Spații de lucru per utilizator | ✅ (bazat pe fișiere)               | —                                            | —                                     | ✅ (PostgreSQL)                |
| Secrete criptate              | — (doar variabile env)               | ✅ ChaCha20-Poly1305                         | — (JSON text simplu)                  | ✅ AES-256-GCM în BD           |

## Arhitectură

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Pornire Rapidă

**Condiții prealabile:** Go 1.26+, PostgreSQL 18 cu pgvector, Docker (opțional)

### Din Sursă

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Asistent interactiv de configurare
source .env.local && ./goclaw
```

### Cu Docker

```bash
# Generează .env cu secrete auto-generate
chmod +x prepare-env.sh && ./prepare-env.sh

# Adaugă cel puțin un GOCLAW_*_API_KEY în .env, apoi:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Tablou de bord web la http://localhost:3000
# Verificare stare: curl http://localhost:18790/health
```

Când variabilele de mediu `GOCLAW_*_API_KEY` sunt setate, gateway-ul se configurează automat fără prompturi interactive — detectează furnizorul, rulează migrările și inițializează datele implicite.

> Pentru variante de build (OTel, Tailscale, Redis), etichete imagini Docker și suprapuneri compose, consultați [Ghidul de Implementare](https://docs.goclaw.sh/#deploy-docker-compose).

## Orchestrare Multi-Agent

GoClaw suportă echipe de agenți și delegare inter-agent — fiecare agent rulează cu propria identitate, instrumente, furnizor LLM și fișiere de context.

### Delegare Agent

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Mod | Cum funcționează | Ideal pentru |
|------|-------------|----------|
| **Sincron** | Agentul A întreabă Agentul B și **așteaptă** răspunsul | Căutări rapide, verificări fapte |
| **Asincron** | Agentul A întreabă Agentul B și **continuă**. B anunță mai târziu | Sarcini lungi, rapoarte, analize aprofundate |

Agenții comunică prin **linkuri de permisiune** explicite cu control direcțional (`outbound`, `inbound`, `bidirectional`) și limite de concurență atât la nivel de link cât și la nivel de agent.

### Echipe de Agenți

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Panou de sarcini partajat** — Creează, revendică, finalizează, caută sarcini cu dependențe `blocked_by`
- **Cutie poștală echipă** — Mesagerie directă peer-to-peer și transmisii
- **Instrumente**: `team_tasks` pentru gestionarea sarcinilor, `team_message` pentru cutia poștală

> Pentru detalii despre delegare, linkuri de permisiune și control concurență, consultați [documentația Echipe de Agenți](https://docs.goclaw.sh/#teams-what-are-teams).

## Instrumente Integrate

| Instrument         | Grup          | Descriere                                                    |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Citește conținutul fișierelor (cu rutare FS virtuală)        |
| `write_file`       | fs            | Scrie/creează fișiere                                        |
| `edit_file`        | fs            | Aplică modificări țintite fișierelor existente               |
| `list_files`       | fs            | Listează conținutul directorului                             |
| `search`           | fs            | Caută conținut fișiere după tipar                            |
| `glob`             | fs            | Găsește fișiere după tipar glob                              |
| `exec`             | runtime       | Execută comenzi shell (cu flux de aprobare)                  |
| `web_search`       | web           | Caută pe web (Brave, DuckDuckGo)                             |
| `web_fetch`        | web           | Preia și parsează conținut web                               |
| `memory_search`    | memory        | Caută în memoria pe termen lung (FTS + vector)               |
| `memory_get`       | memory        | Recuperează intrări din memorie                              |
| `skill_search`     | —             | Caută skill-uri (hibrid BM25 + embedding)                    |
| `knowledge_graph_search` | memory  | Caută entități și traversează relații în graful de cunoaștere |
| `create_image`     | media         | Generare imagini (DashScope, MiniMax)                        |
| `create_audio`     | media         | Generare audio (OpenAI, ElevenLabs, MiniMax, Suno)           |
| `create_video`     | media         | Generare video (MiniMax, Veo)                                |
| `read_document`    | media         | Citire documente (Gemini File API, lanț furnizori)            |
| `read_image`       | media         | Analiză imagini                                              |
| `read_audio`       | media         | Transcriere și analiză audio                                 |
| `read_video`       | media         | Analiză video                                                |
| `message`          | messaging     | Trimite mesaje pe canale                                     |
| `tts`              | —             | Sinteză Text-to-Speech                                       |
| `spawn`            | —             | Lansează un subagent                                         |
| `subagents`        | sessions      | Controlează subagent-urile active                            |
| `team_tasks`       | teams         | Panou sarcini partajat (listare, creare, revendicare, finalizare, căutare) |
| `team_message`     | teams         | Cutie poștală echipă (trimitere, transmisie, citire)         |
| `sessions_list`    | sessions      | Listează sesiunile active                                    |
| `sessions_history` | sessions      | Vizualizează istoricul sesiunilor                            |
| `sessions_send`    | sessions      | Trimite mesaj unei sesiuni                                   |
| `sessions_spawn`   | sessions      | Lansează o sesiune nouă                                      |
| `session_status`   | sessions      | Verifică starea sesiunii                                     |
| `cron`             | automation    | Programează și gestionează joburi cron                       |
| `gateway`          | automation    | Administrare gateway                                         |
| `browser`          | ui            | Automatizare browser (navigare, click, tastare, captură ecran) |
| `announce_queue`   | automation    | Anunț rezultat asincron (pentru delegări asincrone)          |

## Documentație

Documentație completă la **[docs.goclaw.sh](https://docs.goclaw.sh)** — sau parcurge sursa în [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Secțiune | Subiecte |
|---------|--------|
| [Începere](https://docs.goclaw.sh/#what-is-goclaw) | Instalare, Pornire Rapidă, Configurare, Tur Tablou de Bord Web |
| [Concepte de Bază](https://docs.goclaw.sh/#how-goclaw-works) | Bucla Agent, Sesiuni, Instrumente, Memorie, Multi-Tenant |
| [Agenți](https://docs.goclaw.sh/#creating-agents) | Creare Agenți, Fișiere Context, Personalitate, Partajare și Acces |
| [Furnizori](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 alții |
| [Canale](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Echipe de Agenți](https://docs.goclaw.sh/#teams-what-are-teams) | Echipe, Panou Sarcini, Mesagerie, Delegare și Handoff |
| [Avansat](https://docs.goclaw.sh/#custom-tools) | Instrumente Personalizate, MCP, Skill-uri, Cron, Sandbox, Hook-uri, RBAC |
| [Implementare](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Bază de Date, Securitate, Observabilitate, Tailscale |
| [Referință](https://docs.goclaw.sh/#cli-commands) | Comenzi CLI, REST API, Protocol WebSocket, Variabile de Mediu |

## Testare

```bash
go test ./...                                    # Teste unitare
go test -v ./tests/integration/ -timeout 120s    # Teste de integrare (necesită gateway activ)
```

## Starea Proiectului

Consultați [CHANGELOG.md](CHANGELOG.md) pentru starea detaliată a funcționalităților, inclusiv ce a fost testat în producție și ce este încă în desfășurare.

## Mulțumiri

GoClaw este construit pe baza proiectului original [OpenClaw](https://github.com/openclaw/openclaw). Suntem recunoscători pentru arhitectura și viziunea care au inspirat acest port Go.

## Licență

MIT
