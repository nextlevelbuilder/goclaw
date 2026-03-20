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
  <a href="https://docs.goclaw.sh">Dokumentáció</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Gyors kezdés</a> •
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

A **GoClaw** egy többügynökös AI átjáró, amely összeköti az LLM-eket az eszközeiddel, csatornáiddal és adataiddal — egyetlen Go binárisként telepítve, futásidejű függőségek nélkül. Ügynökcsapatokat és ügynökök közötti delegálást vezényel több mint 20 LLM-szolgáltatón keresztül, teljes többbérlős izolációval.

Az [OpenClaw](https://github.com/openclaw/openclaw) Go portja, fokozott biztonsággal, többbérlős PostgreSQL-lel és éles környezetre alkalmas megfigyelhetőséggel.

🌐 **Nyelvek:**
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

## Mi teszi egyedülállóvá

- **Ügynökcsapatok és vezénylés** — Megosztott feladattáblával rendelkező csapatok, ügynökök közötti delegálás (szinkron/aszinkron) és hibrid ügynökfelderítés
- **Többbérlős PostgreSQL** — Felhasználónkénti munkaterületek, felhasználónkénti kontextfájlok, titkosított API-kulcsok (AES-256-GCM), izolált munkamenetek
- **Egyetlen bináris** — ~25 MB statikus Go bináris, nincs Node.js futtatókörnyezet, <1 mp indulási idő, fut egy $5-os VPS-en
- **Éles szintű biztonság** — 5 rétegű jogosultságrendszer (átjáró hitelesítés → globális eszközszabályzat → ügynökönkénti → csatornánkénti → csak tulajdonos) plusz sebességkorlátozás, prompt injection felismerés, SSRF védelem, shell tiltási minták és AES-256-GCM titkosítás
- **20+ LLM-szolgáltató** — Anthropic (natív HTTP+SSE prompt gyorsítótárazással), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP és bármilyen OpenAI-kompatibilis végpont
- **7 üzenetküldő csatorna** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Szolgáltatónkénti gondolkodási mód (Anthropic budget tokenek, OpenAI reasoning effort, DashScope thinking budget) streamelési támogatással
- **Heartbeat** — Rendszeres ügynök-bejelentkezések HEARTBEAT.md ellenőrzőlistákon keresztül, OK esetén elnémítással, aktív órákkal, újrapróbálkozási logikával és csatornán keresztüli kézbesítéssel
- **Ütemezés és Cron** — `at`, `every` és cron kifejezések automatizált ügynökfeladatokhoz, sávon alapuló párhuzamossággal
- **Megfigyelhetőség** — Beépített LLM-hívás nyomkövetés spanekkel és prompt gyorsítótár metrikákkal, opcionális OpenTelemetry OTLP exporttal

## Claw Ökoszisztéma

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Nyelv           | TypeScript      | Rust     | Go       | **Go**                                  |
| Bináris méret   | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (alap) / **~36 MB** (+ OTel) |
| Docker image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (tétlen)    | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Indulás         | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Célhardver      | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| Funkció                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Többbérlős (PostgreSQL)    | —                                    | —                                            | —                                     | ✅                             |
| MCP integráció             | — (ACP-t használ)                   | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Ügynökcsapatok             | —                                    | —                                            | —                                     | ✅ Feladattábla + postaláda    |
| Biztonsági megerősítés     | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Alapszintű (workspace korlát, exec tiltás) | ✅ 5 rétegű védelem        |
| OTel megfigyelhetőség      | ✅ (opt-in bővítmény)               | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| Prompt gyorsítótárazás     | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Tudásgráf                  | —                                    | —                                            | —                                     | ✅ LLM kinyerés + bejárás      |
| Skill rendszer             | ✅ Embeddings/szemantikus            | ✅ SKILL.md + TOML                           | ✅ Alapszintű                         | ✅ BM25 + pgvector hibrid      |
| Sáv alapú ütemező          | ✅                                   | Korlátozott párhuzamosság                    | —                                     | ✅ (main/subagent/team/cron)   |
| Üzenetküldő csatornák      | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Kísérő alkalmazások        | macOS, iOS, Android                  | Python SDK                                   | —                                     | Webes irányítópult              |
| Live Canvas / Hang         | ✅ (A2UI + TTS/STT)                  | —                                            | Hangtranszkripció                     | TTS (4 szolgáltató)            |
| LLM-szolgáltatók           | 10+                                  | 8 natív + 29 kompatibilis                    | 13+                                   | **20+**                        |
| Felhasználónkénti munkaterület | ✅ (fájl alapú)                  | —                                            | —                                     | ✅ (PostgreSQL)                |
| Titkosított titkok         | — (csak env változók)               | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM az adatbázisban |

## Architektúra

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Gyors kezdés

**Előfeltételek:** Go 1.26+, PostgreSQL 18 pgvectorrel, Docker (opcionális)

### Forrásból

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interaktív telepítő varázsló
source .env.local && ./goclaw
```

### Docker segítségével

```bash
# .env generálása automatikusan generált titkokkal
chmod +x prepare-env.sh && ./prepare-env.sh

# Adj meg legalább egy GOCLAW_*_API_KEY értéket a .env fájlban, majd:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Webes irányítópult: http://localhost:3000
# Állapotfelmérés: curl http://localhost:18790/health
```

Ha a `GOCLAW_*_API_KEY` környezeti változók be vannak állítva, az átjáró interaktív prompt nélkül automatikusan elvégzi az onboardingot — felismeri a szolgáltatót, futtatja a migrációkat és feltölti az alapértelmezett adatokat.

> A build-változatokért (OTel, Tailscale, Redis), Docker image-címkékért és compose overlay-ekért lásd a [Telepítési útmutatót](https://docs.goclaw.sh/#deploy-docker-compose).

## Többügynökös vezénylés

A GoClaw támogatja az ügynökcsapatokat és az ügynökök közötti delegálást — minden ügynök saját identitással, eszközökkel, LLM-szolgáltatóval és kontextfájlokkal fut.

### Ügynök delegálás

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Mód | Működése | Legjobb felhasználás |
|------|-------------|----------|
| **Szinkron** | Az A ügynök kérdez a B ügynöktől és **vár** a válaszra | Gyors keresések, tényellenőrzések |
| **Aszinkron** | Az A ügynök kérdez a B ügynöktől és **továbblép**. B később bejelenti az eredményt | Hosszú feladatok, jelentések, mélyanalízis |

Az ügynökök explicit **jogosultsági kapcsolatokon** keresztül kommunikálnak, irányvezérléssel (`outbound`, `inbound`, `bidirectional`) és párhuzamossági korlátokkal mind kapcsolatonként, mind ügynökönként.

### Ügynökcsapatok

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Megosztott feladattábla** — Feladatok létrehozása, igénylése, befejezése, keresése `blocked_by` függőségekkel
- **Csapat postaláda** — Közvetlen társak közötti üzenetküldés és körüzenet
- **Eszközök**: `team_tasks` feladatkezeléshez, `team_message` postaládához

> A delegálás részleteiért, jogosultsági kapcsolatokért és párhuzamossági vezérlésért lásd az [Ügynökcsapatok dokumentációt](https://docs.goclaw.sh/#teams-what-are-teams).

## Beépített eszközök

| Eszköz               | Csoport       | Leírás                                                       |
| -------------------- | ------------- | ------------------------------------------------------------ |
| `read_file`          | fs            | Fájl tartalmának olvasása (virtuális FS útválasztással)      |
| `write_file`         | fs            | Fájlok írása/létrehozása                                     |
| `edit_file`          | fs            | Célzott szerkesztések alkalmazása meglévő fájlokon           |
| `list_files`         | fs            | Könyvtár tartalmának listázása                               |
| `search`             | fs            | Fájltartalom keresése minta alapján                          |
| `glob`               | fs            | Fájlok keresése glob minta alapján                           |
| `exec`               | runtime       | Shell parancsok végrehajtása (jóváhagyási munkafolyamattal)  |
| `web_search`         | web           | Webes keresés (Brave, DuckDuckGo)                            |
| `web_fetch`          | web           | Webes tartalom lekérése és feldolgozása                      |
| `memory_search`      | memory        | Hosszú távú memória keresése (FTS + vector)                  |
| `memory_get`         | memory        | Memóriabejegyzések lekérése                                  |
| `skill_search`       | —             | Skillek keresése (BM25 + embedding hibrid)                   |
| `knowledge_graph_search` | memory    | Entitások keresése és tudásgráf kapcsolatok bejárása         |
| `create_image`       | media         | Képgenerálás (DashScope, MiniMax)                            |
| `create_audio`       | media         | Hanggenerálás (OpenAI, ElevenLabs, MiniMax, Suno)            |
| `create_video`       | media         | Videógenerálás (MiniMax, Veo)                                |
| `read_document`      | media         | Dokumentum olvasása (Gemini File API, szolgáltató lánc)      |
| `read_image`         | media         | Képelemzés                                                   |
| `read_audio`         | media         | Hangátirat és hanganalízis                                   |
| `read_video`         | media         | Videóelemzés                                                 |
| `message`            | messaging     | Üzenetek küldése csatornákra                                 |
| `tts`                | —             | Szövegfelolvasás szintézis                                   |
| `spawn`              | —             | Alügynök indítása                                            |
| `subagents`          | sessions      | Futó alügynökök vezérlése                                    |
| `team_tasks`         | teams         | Megosztott feladattábla (listázás, létrehozás, igénylés, befejezés, keresés) |
| `team_message`       | teams         | Csapat postaláda (küldés, körüzenet, olvasás)                |
| `sessions_list`      | sessions      | Aktív munkamenetek listázása                                 |
| `sessions_history`   | sessions      | Munkamenet előzmények megtekintése                           |
| `sessions_send`      | sessions      | Üzenet küldése egy munkamenetbe                              |
| `sessions_spawn`     | sessions      | Új munkamenet indítása                                       |
| `session_status`     | sessions      | Munkamenet állapotának ellenőrzése                           |
| `cron`               | automation    | Cron feladatok ütemezése és kezelése                         |
| `gateway`            | automation    | Átjáró adminisztráció                                        |
| `browser`            | ui            | Böngésző automatizálás (navigálás, kattintás, gépelés, képernyőkép) |
| `announce_queue`     | automation    | Aszinkron eredmény bejelentés (aszinkron delegáláshoz)       |

## Dokumentáció

A teljes dokumentáció elérhető a **[docs.goclaw.sh](https://docs.goclaw.sh)** oldalon — vagy böngéssz a forrásban itt: [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Szakasz | Témák |
|---------|--------|
| [Kezdő lépések](https://docs.goclaw.sh/#what-is-goclaw) | Telepítés, Gyors kezdés, Konfiguráció, Webes irányítópult bemutató |
| [Alapfogalmak](https://docs.goclaw.sh/#how-goclaw-works) | Ügynök hurok, Munkamenetek, Eszközök, Memória, Többbérlősség |
| [Ügynökök](https://docs.goclaw.sh/#creating-agents) | Ügynökök létrehozása, Kontextfájlok, Személyiség, Megosztás és hozzáférés |
| [Szolgáltatók](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 további |
| [Csatornák](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Ügynökcsapatok](https://docs.goclaw.sh/#teams-what-are-teams) | Csapatok, Feladattábla, Üzenetküldés, Delegálás és átadás |
| [Haladó](https://docs.goclaw.sh/#custom-tools) | Egyéni eszközök, MCP, Skillek, Cron, Sandbox, Horgok, RBAC |
| [Telepítés](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Adatbázis, Biztonság, Megfigyelhetőség, Tailscale |
| [Hivatkozás](https://docs.goclaw.sh/#cli-commands) | CLI parancsok, REST API, WebSocket protokoll, Környezeti változók |

## Tesztelés

```bash
go test ./...                                    # Egységtesztek
go test -v ./tests/integration/ -timeout 120s    # Integrációs tesztek (futó átjárót igényel)
```

## Projekt állapota

Részletes funkció-állapotért, beleértve azt, hogy mi lett tesztelve éles környezetben és mi van még folyamatban, lásd a [CHANGELOG.md](CHANGELOG.md) fájlt.

## Köszönetnyilvánítás

A GoClaw az eredeti [OpenClaw](https://github.com/openclaw/openclaw) projektre épül. Hálásak vagyunk az architektúráért és a vízióért, amely ezt a Go portot ihlette.

## Licenc

MIT
