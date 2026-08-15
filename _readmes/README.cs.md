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
  <a href="https://docs.goclaw.sh">Dokumentace</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Rychlý start</a> •
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

**GoClaw** je multi-agentní AI gateway, která propojuje LLM s vašimi nástroji, kanály a daty — nasazena jako jediný Go binární soubor bez runtime závislostí. Orchestruje týmy agentů a delegování mezi agenty napříč 20+ poskytovateli LLM s plnou multi-tenant izolací.

Go port projektu [OpenClaw](https://github.com/openclaw/openclaw) s vylepšeným zabezpečením, multi-tenant PostgreSQL a produkční pozorovatelností.

🌐 **Jazyky:**
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

## Co ho odlišuje

- **Týmy agentů a orchestrace** — Týmy se sdílenými nástěnkami úkolů, delegováním mezi agenty (synchronní/asynchronní) a hybridním vyhledáváním agentů
- **Multi-tenant PostgreSQL** — Pracovní prostory pro každého uživatele, kontextové soubory pro každého uživatele, šifrované API klíče (AES-256-GCM), izolované relace
- **Jediný binární soubor** — ~25 MB statický Go binární soubor, bez Node.js runtime, <1 s spuštění, běží na VPS za $5
- **Produkční bezpečnost** — 5vrstvý systém oprávnění (autentizace gateway → globální politika nástrojů → na agenta → na kanál → pouze vlastník) plus omezení rychlosti, detekce injekce promptů, ochrana SSRF, vzory zamítnutí shellu a šifrování AES-256-GCM
- **20+ poskytovatelů LLM** — Anthropic (nativní HTTP+SSE s cachováním promptů), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP a libovolný OpenAI-kompatibilní endpoint
- **7 komunikačních kanálů** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Režim myšlení pro každého poskytovatele (Anthropic tokeny rozpočtu, OpenAI effort uvažování, DashScope rozpočet myšlení) s podporou streamování
- **Heartbeat** — Pravidelné kontroly agentů prostřednictvím kontrolních seznamů HEARTBEAT.md s potlačením při OK, aktivními hodinami, logikou opakování a doručením do kanálu
- **Plánování a Cron** — Výrazy `at`, `every` a cron pro automatizované úlohy agentů s souběžností na základě pruhů
- **Pozorovatelnost** — Vestavěné trasování volání LLM se spany a metrikami cache promptů, volitelný export OpenTelemetry OTLP

## Ekosystém Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Jazyk           | TypeScript      | Rust     | Go       | **Go**                                  |
| Velikost binárního souboru | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (základ) / **~36 MB** (+ OTel) |
| Docker image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (nečinnost) | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Spuštění        | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Cílový hardware | Mac Mini od $599+ | edge za $10 | edge za $10 | **VPS od $5+**                   |

| Funkce                     | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| Integrace MCP              | — (používá ACP)                      | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Týmy agentů                | —                                    | —                                            | —                                     | ✅ Nástěnka úkolů + schránka   |
| Posílení bezpečnosti       | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, omezení rychlosti, injection, párování) | Základní (omezení workspace, zamítnutí exec) | ✅ 5vrstvá obrana    |
| Pozorovatelnost OTel       | ✅ (volitelné rozšíření)             | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (volitelný build tag)  |
| Cachování promptů          | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Znalostní graf             | —                                    | —                                            | —                                     | ✅ Extrakce LLM + procházení   |
| Systém dovedností          | ✅ Embeddings/sémantické             | ✅ SKILL.md + TOML                           | ✅ Základní                           | ✅ BM25 + pgvector hybrid      |
| Plánovač na základě pruhů  | ✅                                   | Omezená souběžnost                           | —                                     | ✅ (main/subagent/team/cron)   |
| Komunikační kanály         | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Doprovodné aplikace        | macOS, iOS, Android                  | Python SDK                                   | —                                     | Webový dashboard               |
| Live Canvas / Hlas         | ✅ (A2UI + TTS/STT)                  | —                                            | Přepis hlasu                          | TTS (4 poskytovatelé)          |
| Poskytovatelé LLM          | 10+                                  | 8 nativních + 29 compat                      | 13+                                   | **20+**                        |
| Pracovní prostory na uživatele | ✅ (souborové)                   | —                                            | —                                     | ✅ (PostgreSQL)                |
| Šifrovaná tajemství        | — (pouze env proměnné)               | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM v DB            |

## Architektura

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Rychlý start

**Předpoklady:** Go 1.26+, PostgreSQL 18 s pgvector, Docker (volitelně)

### Ze zdrojového kódu

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interaktivní průvodce nastavením
source .env.local && ./goclaw
```

### S Docker

```bash
# Vygenerovat .env s automaticky generovanými tajemstvími
chmod +x prepare-env.sh && ./prepare-env.sh

# Přidat alespoň jeden GOCLAW_*_API_KEY do .env, poté:
make up

# Webový dashboard na http://localhost:18790
# Kontrola stavu: curl http://localhost:18790/health
```

Pokud jsou nastaveny proměnné prostředí `GOCLAW_*_API_KEY`, gateway se automaticky inicializuje bez interaktivních výzev — detekuje poskytovatele, spustí migrace a naplní výchozí data.

> Pro varianty sestavení (OTel, Tailscale, Redis), tagy Docker image a překrytí compose viz [Průvodce nasazením](https://docs.goclaw.sh/#deploy-docker-compose).

## Multi-agentní orchestrace

GoClaw podporuje týmy agentů a delegování mezi agenty — každý agent běží s vlastní identitou, nástroji, poskytovatelem LLM a kontextovými soubory.

### Delegování agentů

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Režim | Jak funguje | Nejvhodnejší pro |
|-------|-------------|------------------|
| **Synchronní** | Agent A se zeptá Agenta B a **čeká** na odpověď | Rychlé vyhledávání, ověření faktů |
| **Asynchronní** | Agent A se zeptá Agenta B a **pokračuje dál**. B oznámí výsledek později | Dlouhé úkoly, zprávy, hloubková analýza |

Agenti komunikují prostřednictvím explicitních **odkazů oprávnění** s řízením směru (`outbound`, `inbound`, `bidirectional`) a limity souběžnosti na úrovni odkazu i agenta.

### Týmy agentů

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Sdílená nástěnka úkolů** — Vytváření, přiřazování, dokončování a vyhledávání úkolů se závislostmi `blocked_by`
- **Týmová schránka** — Přímé zprávy mezi peer agenty a broadcasty
- **Nástroje**: `team_tasks` pro správu úkolů, `team_message` pro schránku

> Podrobnosti o delegování, odkazech oprávnění a řízení souběžnosti viz [dokumentace Týmů agentů](https://docs.goclaw.sh/#teams-what-are-teams).

## Vestavěné nástroje

| Nástroj            | Skupina       | Popis                                                        |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Čtení obsahu souborů (s virtuálním FS routingem)             |
| `write_file`       | fs            | Zápis/vytváření souborů                                      |
| `edit_file`        | fs            | Cílené úpravy existujících souborů                           |
| `list_files`       | fs            | Výpis obsahu adresáře                                        |
| `search`           | fs            | Vyhledávání obsahu souborů podle vzoru                       |
| `glob`             | fs            | Vyhledávání souborů podle glob vzoru                         |
| `exec`             | runtime       | Spouštění příkazů shellu (s workflow schválení)              |
| `web_search`       | web           | Vyhledávání na webu (Brave, DuckDuckGo)                      |
| `web_fetch`        | web           | Stahování a parsování webového obsahu                        |
| `memory_search`    | memory        | Vyhledávání v dlouhodobé paměti (FTS + vector)               |
| `memory_get`       | memory        | Načítání záznamů paměti                                      |
| `skill_search`     | —             | Vyhledávání dovedností (BM25 + embedding hybrid)             |
| `knowledge_graph_search` | memory  | Vyhledávání entit a procházení vztahů ve znalostním grafu   |
| `create_image`     | media         | Generování obrázků (DashScope, MiniMax)                      |
| `create_audio`     | media         | Generování zvuku (OpenAI, ElevenLabs, MiniMax, Suno)         |
| `create_video`     | media         | Generování videa (MiniMax, Veo)                              |
| `read_document`    | media         | Čtení dokumentů (Gemini File API, řetěz poskytovatelů)       |
| `read_image`       | media         | Analýza obrázků                                              |
| `read_audio`       | media         | Přepis a analýza zvuku                                       |
| `read_video`       | media         | Analýza videa                                                |
| `message`          | messaging     | Odesílání zpráv do kanálů                                    |
| `tts`              | —             | Syntéza textu na řeč                                         |
| `spawn`            | —             | Spuštění subagenta                                           |
| `subagents`        | sessions      | Řízení běžících subagentů                                    |
| `team_tasks`       | teams         | Sdílená nástěnka úkolů (seznam, vytvoření, přiřazení, dokončení, vyhledávání) |
| `team_message`     | teams         | Týmová schránka (odeslání, broadcast, čtení)                 |
| `sessions_list`    | sessions      | Výpis aktivních relací                                       |
| `sessions_history` | sessions      | Zobrazení historie relací                                     |
| `sessions_send`    | sessions      | Odeslání zprávy do relace                                    |
| `sessions_spawn`   | sessions      | Spuštění nové relace                                         |
| `session_status`   | sessions      | Kontrola stavu relace                                        |
| `cron`             | automation    | Plánování a správa cron úloh                                 |
| `gateway`          | automation    | Správa gateway                                               |
| `browser`          | ui            | Automatizace prohlížeče (navigace, klikání, psaní, screenshot) |
| `announce_queue`   | automation    | Oznámení asynchronních výsledků (pro asynchronní delegování) |

## Dokumentace

Úplná dokumentace na **[docs.goclaw.sh](https://docs.goclaw.sh)** — nebo procházejte zdroj v [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Sekce | Témata |
|-------|--------|
| [Začínáme](https://docs.goclaw.sh/#what-is-goclaw) | Instalace, Rychlý start, Konfigurace, Prohlídka webového dashboardu |
| [Základní koncepty](https://docs.goclaw.sh/#how-goclaw-works) | Smyčka agenta, Relace, Nástroje, Paměť, Multi-tenancy |
| [Agenti](https://docs.goclaw.sh/#creating-agents) | Vytváření agentů, Kontextové soubory, Osobnost, Sdílení a přístup |
| [Poskytovatelé](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 dalších |
| [Kanály](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Týmy agentů](https://docs.goclaw.sh/#teams-what-are-teams) | Týmy, Nástěnka úkolů, Zasílání zpráv, Delegování a předání |
| [Pokročilé](https://docs.goclaw.sh/#custom-tools) | Vlastní nástroje, MCP, Dovednosti, Cron, Sandbox, Háky, RBAC |
| [Nasazení](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Databáze, Bezpečnost, Pozorovatelnost, Tailscale |
| [Reference](https://docs.goclaw.sh/#cli-commands) | Příkazy CLI, REST API, WebSocket protokol, Proměnné prostředí |

## Testování

```bash
go test ./...                                    # Jednotkové testy
go test -v ./tests/integration/ -timeout 120s    # Integrační testy (vyžaduje běžící gateway)
```

## Stav projektu

Podrobný stav funkcí včetně toho, co bylo otestováno v produkci a co je stále ve vývoji, viz [CHANGELOG.md](CHANGELOG.md).

## Poděkování

GoClaw je postaven na původním projektu [OpenClaw](https://github.com/openclaw/openclaw). Jsme vděčni za architekturu a vizi, která inspirovala tento Go port.

## Licence

MIT
