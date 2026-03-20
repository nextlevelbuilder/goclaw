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
  <a href="https://docs.goclaw.sh">Dokumentaatio</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Pikaopas</a> •
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

**GoClaw** on moniagentin AI-yhdyskäytävä, joka yhdistää LLM:t työkaluihisi, kanaviin ja tietoihin — käytetään yksittäisenä Go-binäärinä ilman ajonaikaisriippuvuuksia. Se orkestroi agenttiryhmiä ja agenttien välistä delegointia yli 20 LLM-tarjoajan kautta täydellä monivuokraajan eristyksellä.

Go-portti [OpenClaw](https://github.com/openclaw/openclaw)-projektista, jossa on parannettu turvallisuus, monivuokraaja-PostgreSQL ja tuotantotason observoitavuus.

🌐 **Kielet:**
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

## Mikä tekee siitä erilaisen

- **Agenttiryhmät ja orkestrointi** — Tiimit jaetuilla tehtävälaudoilla, agenttien välinen delegointi (synkroninen/asynkroninen) ja hybridin agenttien löytäminen
- **Monivuokraaja-PostgreSQL** — Käyttäjäkohtaiset työtilat, käyttäjäkohtaiset kontekstitiedostot, salatut API-avaimet (AES-256-GCM), eristetyt sessiot
- **Yksittäinen binääri** — ~25 MB staattinen Go-binääri, ei Node.js-ajonaikaa, alle 1 s käynnistys, toimii 5 dollarin VPS:llä
- **Tuotantoturvallisuus** — 5-kerroksinen käyttöoikeusjärjestelmä (yhdyskäytävätodennus → globaali työkalukäytäntö → agenttikohtainen → kanavakohtainen → vain omistajalle) sekä nopeusrajoitus, kehotteen injektioiden havaitseminen, SSRF-suojaus, komennon estokuviot ja AES-256-GCM-salaus
- **20+ LLM-tarjoajaa** — Anthropic (natiivi HTTP+SSE kehotteen välimuistilla), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP ja mikä tahansa OpenAI-yhteensopiva päätepiste
- **7 viestintäkanavaa** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Tarjoajakohtainen ajattelutila (Anthropic budjettitokenit, OpenAI päättelyponnistus, DashScope ajattelubudjetti) suoratoistotuella
- **Heartbeat** — Säännölliset agenttien tarkistukset HEARTBEAT.md-tarkistuslistojen kautta, joissa on OK:n yhteydessä estäminen, aktiiviset tunnit, uudelleenyrityslogiikka ja kanavajakelu
- **Ajastus ja Cron** — `at`-, `every`- ja cron-lausekkeet automaattisille agenttitehtäville kaistapohjaisen samanaikaisuuden kanssa
- **Observoitavuus** — Sisäänrakennettu LLM-kutsujen jäljitys väleillä ja kehotteen välimuistimittareilla, valinnainen OpenTelemetry OTLP-vienti

## Claw-ekosysteemi

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Kieli           | TypeScript      | Rust     | Go       | **Go**                                  |
| Binäärikoko     | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (perus) / **~36 MB** (+ OTel) |
| Docker-kuva     | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (jouten)    | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Käynnistys      | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Kohdealusta     | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| Ominaisuus                 | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Monivuokraaja (PostgreSQL) | —                                    | —                                            | —                                     | ✅                             |
| MCP-integraatio            | — (käyttää ACP:ta)                   | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Agenttiryhmät              | —                                    | —                                            | —                                     | ✅ Tehtävätaulu + postilaatikko |
| Turvallisuuden kovennos    | ✅ (SSRF, polun läpikulku, injektio) | ✅ (hiekkalaatikko, nopeusrajoitus, injektio, parittaminen) | Perus (työtilan rajoitus, suorituksen esto) | ✅ 5-kerroksinen puolustus |
| OTel-observoitavuus        | ✅ (valinnainen laajennus)           | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (valinnainen rakennustagi) |
| Kehotteen välimuisti       | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Tietoverkko                | —                                    | —                                            | —                                     | ✅ LLM-poiminta + läpikulku    |
| Taitojärjestelmä           | ✅ Upotukset/semanttinen             | ✅ SKILL.md + TOML                           | ✅ Perus                              | ✅ BM25 + pgvector hybriidi    |
| Kaistapohjainen ajastin    | ✅                                   | Rajoitettu samanaikaisuus                    | —                                     | ✅ (main/subagent/team/cron)   |
| Viestintäkanavat           | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Kumppanisovellukset        | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web-koontinäyttö               |
| Live Canvas / Ääni         | ✅ (A2UI + TTS/STT)                  | —                                            | Äänen transkriptio                    | TTS (4 tarjoajaa)              |
| LLM-tarjoajat              | 10+                                  | 8 natiivi + 29 yhteensopiva                  | 13+                                   | **20+**                        |
| Käyttäjäkohtaiset työtilat | ✅ (tiedostopohjainen)               | —                                            | —                                     | ✅ (PostgreSQL)                |
| Salatut salaisuudet        | — (vain ympäristömuuttujat)          | ✅ ChaCha20-Poly1305                         | — (pelkkä teksti JSON)                | ✅ AES-256-GCM tietokannassa   |

## Arkkitehtuuri

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Pikaopas

**Vaatimukset:** Go 1.26+, PostgreSQL 18 pgvectorilla, Docker (valinnainen)

### Lähdekoodista

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interaktiivinen asennusohjaaja
source .env.local && ./goclaw
```

### Dockerilla

```bash
# Luo .env automaattisesti generoiduilla salaisuuksilla
chmod +x prepare-env.sh && ./prepare-env.sh

# Lisää vähintään yksi GOCLAW_*_API_KEY tiedostoon .env, sitten:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Web-koontinäyttö osoitteessa http://localhost:3000
# Terveystarkistus: curl http://localhost:18790/health
```

Kun `GOCLAW_*_API_KEY`-ympäristömuuttujat on asetettu, yhdyskäytävä käynnistyy automaattisesti ilman interaktiivisia kehotteita — tunnistaa tarjoajan, ajaa migraatiot ja alustaa oletusdata.

> Rakennusvarianteista (OTel, Tailscale, Redis), Docker-kuvatageista ja compose-päällekkäisyyksistä, katso [Käyttöönotto-opas](https://docs.goclaw.sh/#deploy-docker-compose).

## Moniagentin orkestrointi

GoClaw tukee agenttiryhmiä ja agenttien välistä delegointia — jokainen agentti toimii omalla identiteetillään, työkaluilla, LLM-tarjoajalla ja kontekstitiedostoilla.

### Agentin delegointi

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Tila | Toimintaperiaate | Parhaiten sopii |
|------|-----------------|-----------------|
| **Synkroninen** | Agentti A pyytää agenttia B ja **odottaa** vastausta | Nopeat tiedonhaut, faktojen tarkistus |
| **Asynkroninen** | Agentti A pyytää agenttia B ja **jatkaa eteenpäin**. B ilmoittaa myöhemmin | Pitkät tehtävät, raportit, syvä analyysi |

Agentit kommunikoivat eksplisiittisten **käyttöoikeuslinkkien** kautta suuntakontrollilla (`outbound`, `inbound`, `bidirectional`) ja samanaikaisuusrajoituksilla sekä linkkikohtaisella että agenttikohtaisella tasolla.

### Agenttiryhmät

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Jaettu tehtävätaulu** — Tehtävien luominen, varaaminen, viimeistely ja haku `blocked_by`-riippuvuuksilla
- **Tiimin postilaatikko** — Suora vertaisviestintä ja lähetykset
- **Työkalut**: `team_tasks` tehtävähallintaan, `team_message` postilaatikolle

> Delegoinnin yksityiskohdista, käyttöoikeuslinkeistä ja samanaikaisuuden hallinnasta, katso [Agenttiryhmien dokumentaatio](https://docs.goclaw.sh/#teams-what-are-teams).

## Sisäänrakennetut työkalut

| Työkalu            | Ryhmä         | Kuvaus                                                       |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Lue tiedoston sisältö (virtuaalisen tiedostojärjestelmän reitityksellä) |
| `write_file`       | fs            | Kirjoita/luo tiedostoja                                      |
| `edit_file`        | fs            | Tee kohdennettuja muokkauksia olemassa oleviin tiedostoihin  |
| `list_files`       | fs            | Listaa hakemiston sisältö                                    |
| `search`           | fs            | Etsi tiedostojen sisältöä kuvion mukaan                      |
| `glob`             | fs            | Etsi tiedostoja glob-kuvion mukaan                           |
| `exec`             | runtime       | Suorita komentotulkin komentoja (hyväksymistyönkululla)      |
| `web_search`       | web           | Etsi verkosta (Brave, DuckDuckGo)                            |
| `web_fetch`        | web           | Nouda ja jäsennä verkkosisältöä                              |
| `memory_search`    | memory        | Etsi pitkäaikaisesta muistista (FTS + vektori)               |
| `memory_get`       | memory        | Nouda muistikirjauksia                                       |
| `skill_search`     | —             | Etsi taitoja (BM25 + upotushybriidi)                         |
| `knowledge_graph_search` | memory  | Etsi entiteettejä ja liiku tietoverkon suhteiden läpi        |
| `create_image`     | media         | Kuvan luominen (DashScope, MiniMax)                          |
| `create_audio`     | media         | Äänen luominen (OpenAI, ElevenLabs, MiniMax, Suno)           |
| `create_video`     | media         | Videon luominen (MiniMax, Veo)                               |
| `read_document`    | media         | Dokumentin lukeminen (Gemini File API, tarjoajaketju)        |
| `read_image`       | media         | Kuvan analysointi                                            |
| `read_audio`       | media         | Äänen transkriptio ja analysointi                            |
| `read_video`       | media         | Videon analysointi                                           |
| `message`          | messaging     | Lähetä viestejä kanaville                                    |
| `tts`              | —             | Teksti puheeksi -synteesi                                    |
| `spawn`            | —             | Käynnistä aliagentti                                         |
| `subagents`        | sessions      | Hallitse käynnissä olevia aliagentteja                       |
| `team_tasks`       | teams         | Jaettu tehtävätaulu (listaa, luo, varaa, viimeistele, etsi)  |
| `team_message`     | teams         | Tiimin postilaatikko (lähetä, lähetä kaikille, lue)          |
| `sessions_list`    | sessions      | Listaa aktiiviset sessiot                                    |
| `sessions_history` | sessions      | Tarkastele session historiaa                                 |
| `sessions_send`    | sessions      | Lähetä viesti sessiolle                                      |
| `sessions_spawn`   | sessions      | Käynnistä uusi sessio                                        |
| `session_status`   | sessions      | Tarkista session tila                                        |
| `cron`             | automation    | Ajoita ja hallinnoi cron-töitä                               |
| `gateway`          | automation    | Yhdyskäytävän hallinta                                       |
| `browser`          | ui            | Selaimen automaatio (navigoi, klikkaa, kirjoita, kuvakaappaus) |
| `announce_queue`   | automation    | Asynkroninen tulosten ilmoittaminen (asynkronisille delegoinneille) |

## Dokumentaatio

Täydellinen dokumentaatio osoitteessa **[docs.goclaw.sh](https://docs.goclaw.sh)** — tai selaa lähdekoodia [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)-kansiossa.

| Osio | Aiheet |
|------|--------|
| [Aloittaminen](https://docs.goclaw.sh/#what-is-goclaw) | Asennus, Pikaopas, Konfigurointi, Web-koontinäytön esittely |
| [Peruskäsitteet](https://docs.goclaw.sh/#how-goclaw-works) | Agenttisilmukka, Sessiot, Työkalut, Muisti, Monivuokraajuus |
| [Agentit](https://docs.goclaw.sh/#creating-agents) | Agenttien luominen, Kontekstitiedostot, Persoonallisuus, Jakaminen ja käyttöoikeudet |
| [Tarjoajat](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 muuta |
| [Kanavat](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Agenttiryhmät](https://docs.goclaw.sh/#teams-what-are-teams) | Tiimit, Tehtävätaulu, Viestintä, Delegointi ja luovutus |
| [Edistyneet](https://docs.goclaw.sh/#custom-tools) | Mukautetut työkalut, MCP, Taidot, Cron, Hiekkalaatikko, Koukut, RBAC |
| [Käyttöönotto](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Tietokanta, Turvallisuus, Observoitavuus, Tailscale |
| [Viite](https://docs.goclaw.sh/#cli-commands) | CLI-komennot, REST API, WebSocket-protokolla, Ympäristömuuttujat |

## Testaus

```bash
go test ./...                                    # Yksikkötestit
go test -v ./tests/integration/ -timeout 120s    # Integraatiotestit (vaatii käynnissä olevan yhdyskäytävän)
```

## Projektin tila

Katso [CHANGELOG.md](CHANGELOG.md) yksityiskohtainen ominaisuuksien tila, mukaan lukien mitä on testattu tuotannossa ja mitä on vielä kesken.

## Tunnustukset

GoClaw on rakennettu alkuperäisen [OpenClaw](https://github.com/openclaw/openclaw)-projektin pohjalta. Olemme kiitollisia arkkitehtuurista ja visiosta, joka inspiroi tätä Go-porttia.

## Lisenssi

MIT
