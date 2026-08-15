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
  <a href="https://docs.goclaw.sh">Dokumentasi</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Mulai Cepat</a> •
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

**GoClaw** adalah gateway AI multi-agen yang menghubungkan LLM ke alat, saluran, dan data Anda — dideploy sebagai satu binary Go tanpa dependensi runtime. GoClaw mengorkestasi tim agen dan delegasi antar-agen ke lebih dari 20 penyedia LLM dengan isolasi multi-tenant penuh.

Merupakan port Go dari [OpenClaw](https://github.com/openclaw/openclaw) dengan keamanan yang ditingkatkan, PostgreSQL multi-tenant, dan observabilitas kelas produksi.

🌐 **Bahasa:**
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

## Apa yang Membuatnya Berbeda

- **Tim Agen & Orkestrasi** — Tim dengan papan tugas bersama, delegasi antar-agen (sinkron/asinkron), dan penemuan agen hybrid
- **PostgreSQL Multi-Tenant** — Ruang kerja per-pengguna, file konteks per-pengguna, kunci API terenkripsi (AES-256-GCM), sesi terisolasi
- **Satu Binary** — Binary Go statis ~25 MB, tanpa runtime Node.js, startup <1 detik, berjalan di VPS $5
- **Keamanan Produksi** — Sistem izin 5 lapisan (autentikasi gateway → kebijakan alat global → per-agen → per-saluran → hanya-pemilik) ditambah pembatasan laju, deteksi injeksi prompt, perlindungan SSRF, pola penolakan shell, dan enkripsi AES-256-GCM
- **20+ Penyedia LLM** — Anthropic (HTTP+SSE native dengan prompt caching), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP, dan endpoint kompatibel OpenAI lainnya
- **7 Saluran Pesan** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Mode berpikir per-penyedia (token anggaran Anthropic, upaya penalaran OpenAI, anggaran berpikir DashScope) dengan dukungan streaming
- **Heartbeat** — Pemeriksaan berkala agen melalui daftar periksa HEARTBEAT.md dengan suppress-on-OK, jam aktif, logika percobaan ulang, dan pengiriman saluran
- **Penjadwalan & Cron** — Ekspresi `at`, `every`, dan cron untuk tugas agen otomatis dengan konkurensi berbasis jalur
- **Observabilitas** — Pelacakan panggilan LLM bawaan dengan span dan metrik cache prompt, ekspor OpenTelemetry OTLP opsional

## Ekosistem Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Bahasa          | TypeScript      | Rust     | Go       | **Go**                                  |
| Ukuran binary   | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (dasar) / **~36 MB** (+ OTel) |
| Image Docker    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (idle)      | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Startup         | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Target hardware | Mac Mini $599+  | edge $10 | edge $10 | **VPS $5+**                             |

| Fitur                      | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| Integrasi MCP              | — (menggunakan ACP)                  | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Tim agen                   | —                                    | —                                            | —                                     | ✅ Papan tugas + kotak surat   |
| Penguatan keamanan         | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Dasar (workspace restrict, exec deny) | ✅ Pertahanan 5 lapisan        |
| Observabilitas OTel        | ✅ (ekstensi opsional)               | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (build tag opsional)   |
| Prompt caching             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Graf pengetahuan           | —                                    | —                                            | —                                     | ✅ Ekstraksi LLM + traversal   |
| Sistem skill               | ✅ Embeddings/semantik               | ✅ SKILL.md + TOML                           | ✅ Dasar                              | ✅ BM25 + pgvector hybrid      |
| Penjadwal berbasis jalur   | ✅                                   | Konkurensi terbatas                          | —                                     | ✅ (main/subagent/team/cron)   |
| Saluran pesan              | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Aplikasi pendamping        | macOS, iOS, Android                  | Python SDK                                   | —                                     | Dasbor Web                     |
| Live Canvas / Suara        | ✅ (A2UI + TTS/STT)                  | —                                            | Transkripsi suara                     | TTS (4 penyedia)               |
| Penyedia LLM               | 10+                                  | 8 native + 29 compat                         | 13+                                   | **20+**                        |
| Ruang kerja per-pengguna   | ✅ (berbasis file)                   | —                                            | —                                     | ✅ (PostgreSQL)                |
| Rahasia terenkripsi        | — (hanya env vars)                   | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM di DB           |

## Arsitektur

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Mulai Cepat

**Prasyarat:** Go 1.26+, PostgreSQL 18 dengan pgvector, Docker (opsional)

### Dari Kode Sumber

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Wizard pengaturan interaktif
source .env.local && ./goclaw
```

### Dengan Docker

```bash
# Buat .env dengan rahasia yang di-generate otomatis
chmod +x prepare-env.sh && ./prepare-env.sh

# Tambahkan minimal satu GOCLAW_*_API_KEY ke .env, lalu:
make up

# Dasbor Web di http://localhost:18790
# Pemeriksaan kesehatan: curl http://localhost:18790/health
```

Ketika variabel lingkungan `GOCLAW_*_API_KEY` diatur, gateway akan melakukan onboard otomatis tanpa prompt interaktif — mendeteksi penyedia, menjalankan migrasi, dan menyemai data default.

> Untuk varian build (OTel, Tailscale, Redis), tag image Docker, dan overlay compose, lihat [Panduan Deployment](https://docs.goclaw.sh/#deploy-docker-compose).

## Orkestrasi Multi-Agen

GoClaw mendukung tim agen dan delegasi antar-agen — setiap agen berjalan dengan identitas, alat, penyedia LLM, dan file konteks miliknya sendiri.

### Delegasi Agen

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Mode | Cara kerjanya | Terbaik untuk |
|------|-------------|----------|
| **Sinkron** | Agen A bertanya ke Agen B dan **menunggu** jawabannya | Pencarian cepat, pengecekan fakta |
| **Asinkron** | Agen A bertanya ke Agen B dan **melanjutkan**. B mengumumkan nanti | Tugas panjang, laporan, analisis mendalam |

Agen berkomunikasi melalui **tautan izin** eksplisit dengan kontrol arah (`outbound`, `inbound`, `bidirectional`) dan batas konkurensi di tingkat per-tautan maupun per-agen.

### Tim Agen

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Papan tugas bersama** — Buat, klaim, selesaikan, cari tugas dengan dependensi `blocked_by`
- **Kotak surat tim** — Pesan langsung antar-sesama dan siaran
- **Alat**: `team_tasks` untuk manajemen tugas, `team_message` untuk kotak surat

> Untuk detail delegasi, tautan izin, dan kontrol konkurensi, lihat [dokumentasi Tim Agen](https://docs.goclaw.sh/#teams-what-are-teams).

## Alat Bawaan

| Alat               | Grup          | Deskripsi                                                    |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Membaca isi file (dengan routing FS virtual)                 |
| `write_file`       | fs            | Menulis/membuat file                                         |
| `edit_file`        | fs            | Menerapkan pengeditan terarah pada file yang ada             |
| `list_files`       | fs            | Menampilkan isi direktori                                    |
| `search`           | fs            | Mencari isi file berdasarkan pola                            |
| `glob`             | fs            | Menemukan file berdasarkan pola glob                         |
| `exec`             | runtime       | Menjalankan perintah shell (dengan alur persetujuan)         |
| `web_search`       | web           | Mencari di web (Brave, DuckDuckGo)                           |
| `web_fetch`        | web           | Mengambil dan memparse konten web                            |
| `memory_search`    | memory        | Mencari memori jangka panjang (FTS + vector)                 |
| `memory_get`       | memory        | Mengambil entri memori                                       |
| `skill_search`     | —             | Mencari skill (BM25 + embedding hybrid)                      |
| `knowledge_graph_search` | memory  | Mencari entitas dan menelusuri relasi graf pengetahuan       |
| `create_image`     | media         | Pembuatan gambar (DashScope, MiniMax)                        |
| `create_audio`     | media         | Pembuatan audio (OpenAI, ElevenLabs, MiniMax, Suno)          |
| `create_video`     | media         | Pembuatan video (MiniMax, Veo)                               |
| `read_document`    | media         | Pembacaan dokumen (Gemini File API, rantai penyedia)         |
| `read_image`       | media         | Analisis gambar                                              |
| `read_audio`       | media         | Transkripsi dan analisis audio                               |
| `read_video`       | media         | Analisis video                                               |
| `message`          | messaging     | Mengirim pesan ke saluran                                    |
| `tts`              | —             | Sintesis Text-to-Speech                                      |
| `spawn`            | —             | Menjalankan subagen                                          |
| `subagents`        | sessions      | Mengendalikan subagen yang berjalan                          |
| `team_tasks`       | teams         | Papan tugas bersama (list, buat, klaim, selesaikan, cari)    |
| `team_message`     | teams         | Kotak surat tim (kirim, siaran, baca)                        |
| `sessions_list`    | sessions      | Menampilkan sesi aktif                                       |
| `sessions_history` | sessions      | Melihat riwayat sesi                                         |
| `sessions_send`    | sessions      | Mengirim pesan ke sesi                                       |
| `sessions_spawn`   | sessions      | Menjalankan sesi baru                                        |
| `session_status`   | sessions      | Memeriksa status sesi                                        |
| `cron`             | automation    | Menjadwalkan dan mengelola cron job                          |
| `gateway`          | automation    | Administrasi gateway                                         |
| `browser`          | ui            | Otomasi browser (navigasi, klik, ketik, screenshot)          |
| `announce_queue`   | automation    | Pengumuman hasil asinkron (untuk delegasi asinkron)          |

## Dokumentasi

Dokumentasi lengkap di **[docs.goclaw.sh](https://docs.goclaw.sh)** — atau jelajahi sumbernya di [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Bagian | Topik |
|---------|--------|
| [Memulai](https://docs.goclaw.sh/#what-is-goclaw) | Instalasi, Mulai Cepat, Konfigurasi, Tur Dasbor Web |
| [Konsep Inti](https://docs.goclaw.sh/#how-goclaw-works) | Loop Agen, Sesi, Alat, Memori, Multi-Tenancy |
| [Agen](https://docs.goclaw.sh/#creating-agents) | Membuat Agen, File Konteks, Kepribadian, Berbagi & Akses |
| [Penyedia](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 lainnya |
| [Saluran](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Tim Agen](https://docs.goclaw.sh/#teams-what-are-teams) | Tim, Papan Tugas, Pesan, Delegasi & Handoff |
| [Lanjutan](https://docs.goclaw.sh/#custom-tools) | Alat Kustom, MCP, Skill, Cron, Sandbox, Hook, RBAC |
| [Deployment](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Database, Keamanan, Observabilitas, Tailscale |
| [Referensi](https://docs.goclaw.sh/#cli-commands) | Perintah CLI, REST API, Protokol WebSocket, Variabel Lingkungan |

## Pengujian

```bash
go test ./...                                    # Tes unit
go test -v ./tests/integration/ -timeout 120s    # Tes integrasi (memerlukan gateway yang berjalan)
```

## Status Proyek

Lihat [CHANGELOG.md](CHANGELOG.md) untuk status fitur terperinci termasuk apa yang telah diuji di produksi dan apa yang masih dalam proses.

## Ucapan Terima Kasih

GoClaw dibangun di atas proyek [OpenClaw](https://github.com/openclaw/openclaw) yang asli. Kami berterima kasih atas arsitektur dan visi yang menginspirasi port Go ini.

## Lisensi

MIT
