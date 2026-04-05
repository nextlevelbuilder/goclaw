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
  <a href="https://docs.goclaw.sh">Dokumentacja</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Szybki Start</a> •
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

**GoClaw** to wieloagentowa bramka AI łącząca LLM z Twoimi narzędziami, kanałami i danymi — wdrażana jako pojedynczy binarny plik Go bez żadnych zależności w czasie wykonywania. Orkiestruje zespoły agentów i delegowanie między agentami przez 20+ dostawców LLM z pełną izolacją wielodostępną.

Port GoClaw w języku Go projektu [OpenClaw](https://github.com/openclaw/openclaw) z rozszerzonymi zabezpieczeniami, wielodostępnym PostgreSQL i obserwowalnością na poziomie produkcyjnym.

🌐 **Języki:**
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

## Co Go Wyróżnia

- **Zespoły Agentów i Orkiestracja** — Zespoły ze wspólnymi tablicami zadań, delegowaniem między agentami (synchroniczne/asynchroniczne) i hybrydowym odkrywaniem agentów
- **Wielodostępny PostgreSQL** — Oddzielne obszary robocze dla każdego użytkownika, pliki kontekstu per użytkownik, szyfrowane klucze API (AES-256-GCM), izolowane sesje
- **Pojedynczy Plik Binarny** — ~25 MB statyczny plik binarny Go, bez środowiska uruchomieniowego Node.js, uruchomienie <1s, działa na serwerze za $5
- **Bezpieczeństwo Produkcyjne** — 5-warstwowy system uprawnień (uwierzytelnianie bramki → globalna polityka narzędzi → per-agent → per-kanał → tylko właściciel) oraz ograniczanie szybkości, wykrywanie wstrzyknięć do promptów, ochrona SSRF, wzorce blokowania powłoki i szyfrowanie AES-256-GCM
- **20+ Dostawców LLM** — Anthropic (natywny HTTP+SSE z buforowaniem promptów), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP i dowolny punkt końcowy kompatybilny z OpenAI
- **7 Kanałów Komunikacji** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Tryb myślenia per dostawca (tokeny budżetowe Anthropic, wysiłek wnioskowania OpenAI, budżet myślenia DashScope) ze wsparciem strumieniowania
- **Heartbeat** — Okresowe meldowanie agentów przez listy kontrolne HEARTBEAT.md z wyciszaniem przy OK, aktywnymi godzinami, logiką ponawiania prób i dostarczaniem przez kanały
- **Planowanie i Cron** — Wyrażenia `at`, `every` i cron do zautomatyzowanych zadań agentów ze współbieżnością opartą na torach
- **Obserwowalność** — Wbudowane śledzenie wywołań LLM z rozpiętościami i metrykami buforu promptów, opcjonalny eksport OpenTelemetry OTLP

## Ekosystem Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Język           | TypeScript      | Rust     | Go       | **Go**                                  |
| Rozmiar binarny | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (bazowy) / **~36 MB** (+ OTel) |
| Obraz Docker    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (bezczynny) | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Uruchomienie    | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Docelowy sprzęt | Mac Mini $599+  | Edge $10 | Edge $10 | **VPS $5+**                             |

| Funkcja                        | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| ------------------------------ | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Wielodostęp (PostgreSQL)       | —                                    | —                                            | —                                     | ✅                             |
| Integracja MCP                 | — (używa ACP)                        | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Zespoły agentów                | —                                    | —                                            | —                                     | ✅ Tablica zadań + skrzynka    |
| Utwardzanie bezpieczeństwa     | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Podstawowe (ograniczenie workspace, blokowanie exec) | ✅ 5-warstwowa obrona |
| Obserwowalność OTel            | ✅ (opcjonalne rozszerzenie)          | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opcjonalny tag build) |
| Buforowanie promptów           | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Graf wiedzy                    | —                                    | —                                            | —                                     | ✅ Ekstrakcja LLM + przechodzenie |
| System umiejętności            | ✅ Embeddingi/semantyczne            | ✅ SKILL.md + TOML                           | ✅ Podstawowy                         | ✅ Hybrydowy BM25 + pgvector   |
| Harmonogram oparty na torach   | ✅                                   | Ograniczona współbieżność                    | —                                     | ✅ (main/subagent/team/cron)   |
| Kanały komunikacji             | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Aplikacje towarzyszące         | macOS, iOS, Android                  | Python SDK                                   | —                                     | Panel webowy                   |
| Live Canvas / Głos             | ✅ (A2UI + TTS/STT)                  | —                                            | Transkrypcja głosu                    | TTS (4 dostawców)              |
| Dostawcy LLM                   | 10+                                  | 8 natywnych + 29 compat                      | 13+                                   | **20+**                        |
| Obszary robocze per użytkownik | ✅ (oparte na plikach)               | —                                            | —                                     | ✅ (PostgreSQL)                |
| Szyfrowane sekrety             | — (tylko zmienne env)                | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM w DB            |

## Architektura

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Szybki Start

**Wymagania wstępne:** Go 1.26+, PostgreSQL 18 z pgvector, Docker (opcjonalnie)

### Ze Źródła

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interaktywny kreator konfiguracji
source .env.local && ./goclaw
```

### Z Docker

```bash
# Wygeneruj .env z automatycznie generowanymi sekretami
chmod +x prepare-env.sh && ./prepare-env.sh

# Dodaj co najmniej jeden GOCLAW_*_API_KEY do .env, a następnie:
make up

# Panel webowy pod adresem http://localhost:18790
# Sprawdzenie stanu: curl http://localhost:18790/health
```

Gdy ustawione są zmienne środowiskowe `GOCLAW_*_API_KEY`, bramka automatycznie konfiguruje się bez interaktywnych pytań — wykrywa dostawcę, uruchamia migracje i wypełnia domyślnymi danymi.

> Aby zapoznać się z wariantami budowania (OTel, Tailscale, Redis), tagami obrazów Docker i nakładkami compose, zapoznaj się z [Przewodnikiem Wdrażania](https://docs.goclaw.sh/#deploy-docker-compose).

## Wieloagentowa Orkiestracja

GoClaw obsługuje zespoły agentów i delegowanie między agentami — każdy agent działa z własną tożsamością, narzędziami, dostawcą LLM i plikami kontekstu.

### Delegowanie Agentów

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Tryb | Jak działa | Najlepsze zastosowanie |
|------|-----------|------------------------|
| **Synchroniczny** | Agent A pyta Agenta B i **czeka** na odpowiedź | Szybkie wyszukiwania, weryfikacja faktów |
| **Asynchroniczny** | Agent A pyta Agenta B i **kontynuuje**. B ogłasza wynik później | Długie zadania, raporty, dogłębna analiza |

Agenty komunikują się przez jawne **łącza uprawnień** z kontrolą kierunku (`outbound`, `inbound`, `bidirectional`) i limitami współbieżności zarówno na poziomie łącza, jak i agenta.

### Zespoły Agentów

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Wspólna tablica zadań** — Tworzenie, przejmowanie, ukończenie i wyszukiwanie zadań z zależnościami `blocked_by`
- **Skrzynka zespołu** — Bezpośrednia wymiana wiadomości peer-to-peer i transmisje
- **Narzędzia**: `team_tasks` do zarządzania zadaniami, `team_message` do skrzynki

> Aby zapoznać się ze szczegółami delegowania, łączami uprawnień i kontrolą współbieżności, zapoznaj się z [dokumentacją Zespołów Agentów](https://docs.goclaw.sh/#teams-what-are-teams).

## Wbudowane Narzędzia

| Narzędzie          | Grupa         | Opis                                                         |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Odczyt zawartości pliku (z routingiem wirtualnego FS)        |
| `write_file`       | fs            | Zapis/tworzenie plików                                       |
| `edit_file`        | fs            | Stosowanie ukierunkowanych edycji do istniejących plików     |
| `list_files`       | fs            | Wylistowanie zawartości katalogu                             |
| `search`           | fs            | Wyszukiwanie zawartości pliku wg wzorca                      |
| `glob`             | fs            | Wyszukiwanie plików wg wzorca glob                           |
| `exec`             | runtime       | Wykonywanie poleceń powłoki (z przepływem zatwierdzania)     |
| `web_search`       | web           | Przeszukiwanie internetu (Brave, DuckDuckGo)                 |
| `web_fetch`        | web           | Pobieranie i parsowanie treści webowych                      |
| `memory_search`    | memory        | Przeszukiwanie pamięci długoterminowej (FTS + wektor)        |
| `memory_get`       | memory        | Pobieranie wpisów pamięci                                    |
| `skill_search`     | —             | Wyszukiwanie umiejętności (hybryda BM25 + embedding)         |
| `knowledge_graph_search` | memory  | Wyszukiwanie encji i przechodzenie relacji grafu wiedzy      |
| `create_image`     | media         | Generowanie obrazów (DashScope, MiniMax)                     |
| `create_audio`     | media         | Generowanie dźwięku (OpenAI, ElevenLabs, MiniMax, Suno)      |
| `create_video`     | media         | Generowanie wideo (MiniMax, Veo)                             |
| `read_document`    | media         | Odczyt dokumentów (Gemini File API, łańcuch dostawców)       |
| `read_image`       | media         | Analiza obrazów                                              |
| `read_audio`       | media         | Transkrypcja i analiza dźwięku                               |
| `read_video`       | media         | Analiza wideo                                                |
| `message`          | messaging     | Wysyłanie wiadomości do kanałów                              |
| `tts`              | —             | Synteza tekstu na mowę                                       |
| `spawn`            | —             | Uruchamianie subagenta                                       |
| `subagents`        | sessions      | Sterowanie działającymi subagentami                          |
| `team_tasks`       | teams         | Wspólna tablica zadań (list, create, claim, complete, search) |
| `team_message`     | teams         | Skrzynka zespołu (send, broadcast, read)                     |
| `sessions_list`    | sessions      | Lista aktywnych sesji                                        |
| `sessions_history` | sessions      | Przeglądanie historii sesji                                  |
| `sessions_send`    | sessions      | Wysyłanie wiadomości do sesji                                |
| `sessions_spawn`   | sessions      | Uruchamianie nowej sesji                                     |
| `session_status`   | sessions      | Sprawdzanie stanu sesji                                      |
| `cron`             | automation    | Planowanie i zarządzanie zadaniami cron                      |
| `gateway`          | automation    | Administracja bramką                                         |
| `browser`          | ui            | Automatyzacja przeglądarki (nawigacja, klikanie, wpisywanie, zrzut ekranu) |
| `announce_queue`   | automation    | Ogłaszanie wyników asynchronicznych (dla delegowań asynchronicznych) |

## Dokumentacja

Pełna dokumentacja dostępna na **[docs.goclaw.sh](https://docs.goclaw.sh)** — lub przejrzyj źródło w [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Sekcja | Tematy |
|--------|--------|
| [Pierwsze Kroki](https://docs.goclaw.sh/#what-is-goclaw) | Instalacja, Szybki Start, Konfiguracja, Przewodnik po Panelu Webowym |
| [Podstawowe Koncepcje](https://docs.goclaw.sh/#how-goclaw-works) | Pętla Agenta, Sesje, Narzędzia, Pamięć, Wielodostępność |
| [Agenty](https://docs.goclaw.sh/#creating-agents) | Tworzenie Agentów, Pliki Kontekstu, Osobowość, Udostępnianie i Dostęp |
| [Dostawcy](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 innych |
| [Kanały](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Zespoły Agentów](https://docs.goclaw.sh/#teams-what-are-teams) | Zespoły, Tablica Zadań, Wiadomości, Delegowanie i Przekazywanie |
| [Zaawansowane](https://docs.goclaw.sh/#custom-tools) | Własne Narzędzia, MCP, Umiejętności, Cron, Sandbox, Hooki, RBAC |
| [Wdrażanie](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Baza Danych, Bezpieczeństwo, Obserwowalność, Tailscale |
| [Dokumentacja Referencyjna](https://docs.goclaw.sh/#cli-commands) | Polecenia CLI, REST API, Protokół WebSocket, Zmienne Środowiskowe |

## Testowanie

```bash
go test ./...                                    # Testy jednostkowe
go test -v ./tests/integration/ -timeout 120s    # Testy integracyjne (wymaga działającej bramki)
```

## Stan Projektu

Zobacz [CHANGELOG.md](CHANGELOG.md), aby uzyskać szczegółowy status funkcji, w tym co zostało przetestowane w środowisku produkcyjnym i co jest jeszcze w trakcie pracy.

## Podziękowania

GoClaw jest zbudowany na podstawie oryginalnego projektu [OpenClaw](https://github.com/openclaw/openclaw). Jesteśmy wdzięczni za architekturę i wizję, która zainspirowała ten port w języku Go.

## Licencja

MIT
