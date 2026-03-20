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
  <a href="https://docs.goclaw.sh">Belgelendirme</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Hızlı Başlangıç</a> •
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

**GoClaw**, LLM'leri araçlarınıza, kanallarınıza ve verilerinize bağlayan çok ajanlı bir yapay zeka ağ geçididir — sıfır çalışma zamanı bağımlılığıyla tek bir Go ikili dosyası olarak dağıtılır. 20'den fazla LLM sağlayıcısında tam çok kiracılı izolasyonla ajan ekiplerini ve ajanlar arası devri yönetir.

[OpenClaw](https://github.com/openclaw/openclaw) projesinin, geliştirilmiş güvenlik, çok kiracılı PostgreSQL ve üretim düzeyinde gözlemlenebilirlik ile yazılmış bir Go portudur.

🌐 **Diller:**
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

## Farkı Yaratan Özellikler

- **Ajan Ekipleri ve Orkestrasyon** — Paylaşılan görev panoları, ajanlar arası devir (senkron/asenkron) ve hibrit ajan keşifiyle ekipler
- **Çok Kiracılı PostgreSQL** — Kullanıcı başına çalışma alanları, kullanıcı başına bağlam dosyaları, şifreli API anahtarları (AES-256-GCM), izole oturumlar
- **Tek İkili Dosya** — ~25 MB statik Go ikili dosyası, Node.js çalışma zamanı yok, <1 saniye başlatma, 5 dolarlık bir VPS üzerinde çalışır
- **Üretim Güvenliği** — 5 katmanlı izin sistemi (ağ geçidi kimlik doğrulaması → genel araç politikası → ajan başına → kanal başına → yalnızca sahip) ile hız sınırlama, komut enjeksiyonu tespiti, SSRF koruması, kabuk reddetme desenleri ve AES-256-GCM şifreleme
- **20'den Fazla LLM Sağlayıcısı** — Anthropic (istem önbellekleme ile yerel HTTP+SSE), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP ve OpenAI uyumlu herhangi bir uç nokta
- **7 Mesajlaşma Kanalı** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Akış desteğiyle sağlayıcı başına düşünme modu (Anthropic bütçe tokeni, OpenAI muhakeme çabası, DashScope düşünme bütçesi)
- **Heartbeat** — Sessizlik-başarı modunda, aktif saatler, yeniden deneme mantığı ve kanal teslimiyle HEARTBEAT.md kontrol listeleri üzerinden periyodik ajan bildirimleri
- **Zamanlama ve Cron** — Şerit tabanlı eşzamanlılıkla otomatikleştirilmiş ajan görevleri için `at`, `every` ve cron ifadeleri
- **Gözlemlenebilirlik** — Aralıklar ve istem önbellek metrikleriyle yerleşik LLM çağrı izleme, isteğe bağlı OpenTelemetry OTLP dışa aktarma

## Claw Ekosistemi

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Dil             | TypeScript      | Rust     | Go       | **Go**                                  |
| İkili dosya boyutu | 28 MB + Node.js | 3.4 MB | ~8 MB    | **~25 MB** (temel) / **~36 MB** (+ OTel) |
| Docker imajı    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (boşta)     | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Başlatma        | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Hedef donanım   | $599+ Mac Mini  | $10 uç   | $10 uç   | **$5 VPS+**                             |

| Özellik                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Çok kiracılı (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| MCP entegrasyonu           | — (ACP kullanır)                     | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Ajan ekipleri              | —                                    | —                                            | —                                     | ✅ Görev panosu + posta kutusu |
| Güvenlik sertleştirme      | ✅ (SSRF, yol geçişi, enjeksiyon)   | ✅ (kum havuzu, hız sınırı, enjeksiyon, eşleştirme) | Temel (çalışma alanı kısıtla, exec reddet) | ✅ 5 katmanlı savunma      |
| OTel gözlemlenebilirlik    | ✅ (isteğe bağlı uzantı)             | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (isteğe bağlı derleme etiketi) |
| İstem önbellekleme         | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI uyumlu   |
| Bilgi grafiği              | —                                    | —                                            | —                                     | ✅ LLM çıkarımı + geçiş       |
| Yetenek sistemi            | ✅ Gömme/anlamsal                    | ✅ SKILL.md + TOML                           | ✅ Temel                              | ✅ BM25 + pgvector hibrit      |
| Şerit tabanlı zamanlayıcı  | ✅                                   | Sınırlı eşzamanlılık                         | —                                     | ✅ (main/subagent/team/cron)   |
| Mesajlaşma kanalları       | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Yardımcı uygulamalar       | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web panosu                     |
| Canlı Tuval / Ses          | ✅ (A2UI + TTS/STT)                  | —                                            | Ses transkripsiyonu                   | TTS (4 sağlayıcı)              |
| LLM sağlayıcıları          | 10+                                  | 8 yerel + 29 uyumlu                          | 13+                                   | **20+**                        |
| Kullanıcı başına çalışma alanları | ✅ (dosya tabanlı)             | —                                            | —                                     | ✅ (PostgreSQL)                |
| Şifreli gizli bilgiler     | — (yalnızca ortam değişkenleri)      | ✅ ChaCha20-Poly1305                         | — (düz metin JSON)                    | ✅ DB'de AES-256-GCM           |

## Mimari

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Hızlı Başlangıç

**Önkoşullar:** Go 1.26+, pgvector ile PostgreSQL 18, Docker (isteğe bağlı)

### Kaynaktan

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Etkileşimli kurulum sihirbazı
source .env.local && ./goclaw
```

### Docker ile

```bash
# Otomatik oluşturulan gizli bilgilerle .env oluştur
chmod +x prepare-env.sh && ./prepare-env.sh

# .env dosyasına en az bir GOCLAW_*_API_KEY ekleyin, ardından:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Web Panosu: http://localhost:3000
# Sağlık kontrolü: curl http://localhost:18790/health
```

`GOCLAW_*_API_KEY` ortam değişkenleri ayarlandığında, ağ geçidi etkileşimli istem olmadan otomatik olarak katılır — sağlayıcıyı algılar, migrasyonları çalıştırır ve varsayılan verileri yerleştirir.

> Derleme varyantları (OTel, Tailscale, Redis), Docker imaj etiketleri ve compose katmanları için [Dağıtım Kılavuzu](https://docs.goclaw.sh/#deploy-docker-compose) sayfasına bakın.

## Çok Ajanlı Orkestrasyon

GoClaw, ajan ekiplerini ve ajanlar arası devri destekler — her ajan kendi kimliği, araçları, LLM sağlayıcısı ve bağlam dosyalarıyla çalışır.

### Ajan Devri

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Mod | Nasıl çalışır | En iyi kullanım |
|------|-------------|----------|
| **Senkron** | A Ajanı, B Ajanına sorar ve yanıtı **bekler** | Hızlı sorgular, gerçek doğrulama |
| **Asenkron** | A Ajanı, B Ajanına sorar ve **devam eder**. B daha sonra bildirir | Uzun görevler, raporlar, derin analiz |

Ajanlar, yön kontrolü (`outbound`, `inbound`, `bidirectional`) ve hem bağlantı başına hem de ajan başına eşzamanlılık sınırlarıyla açık **izin bağlantıları** üzerinden iletişim kurar.

### Ajan Ekipleri

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Paylaşılan görev panosu** — `blocked_by` bağımlılıklarıyla görev oluşturma, talep etme, tamamlama ve arama
- **Ekip posta kutusu** — Doğrudan eşler arası mesajlaşma ve yayınlar
- **Araçlar**: Görev yönetimi için `team_tasks`, posta kutusu için `team_message`

> Devir ayrıntıları, izin bağlantıları ve eşzamanlılık kontrolü için [Ajan Ekipleri belgelerine](https://docs.goclaw.sh/#teams-what-are-teams) bakın.

## Yerleşik Araçlar

| Araç               | Grup          | Açıklama                                                     |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Dosya içeriğini oku (sanal FS yönlendirmesiyle)              |
| `write_file`       | fs            | Dosya yaz/oluştur                                            |
| `edit_file`        | fs            | Mevcut dosyalara hedefli düzenlemeler uygula                 |
| `list_files`       | fs            | Dizin içeriğini listele                                      |
| `search`           | fs            | Dosya içeriğini desene göre ara                              |
| `glob`             | fs            | Glob deseniyle dosya bul                                     |
| `exec`             | runtime       | Kabuk komutları çalıştır (onay iş akışıyla)                  |
| `web_search`       | web           | Web'de ara (Brave, DuckDuckGo)                               |
| `web_fetch`        | web           | Web içeriğini getir ve ayrıştır                              |
| `memory_search`    | memory        | Uzun süreli bellekte ara (FTS + vektör)                      |
| `memory_get`       | memory        | Bellek girdilerini al                                        |
| `skill_search`     | —             | Yetenekleri ara (BM25 + gömme hibrit)                        |
| `knowledge_graph_search` | memory  | Varlıkları ara ve bilgi grafiği ilişkilerinde gezin          |
| `create_image`     | media         | Görüntü oluşturma (DashScope, MiniMax)                       |
| `create_audio`     | media         | Ses oluşturma (OpenAI, ElevenLabs, MiniMax, Suno)            |
| `create_video`     | media         | Video oluşturma (MiniMax, Veo)                               |
| `read_document`    | media         | Belge okuma (Gemini File API, sağlayıcı zinciri)             |
| `read_image`       | media         | Görüntü analizi                                              |
| `read_audio`       | media         | Ses transkripsiyonu ve analizi                               |
| `read_video`       | media         | Video analizi                                                |
| `message`          | messaging     | Kanallara mesaj gönder                                       |
| `tts`              | —             | Metinden Konuşmaya sentezi                                   |
| `spawn`            | —             | Bir alt ajan başlat                                          |
| `subagents`        | sessions      | Çalışan alt ajanları yönet                                   |
| `team_tasks`       | teams         | Paylaşılan görev panosu (listele, oluştur, talep et, tamamla, ara) |
| `team_message`     | teams         | Ekip posta kutusu (gönder, yayınla, oku)                     |
| `sessions_list`    | sessions      | Aktif oturumları listele                                     |
| `sessions_history` | sessions      | Oturum geçmişini görüntüle                                   |
| `sessions_send`    | sessions      | Bir oturuma mesaj gönder                                     |
| `sessions_spawn`   | sessions      | Yeni bir oturum başlat                                       |
| `session_status`   | sessions      | Oturum durumunu kontrol et                                   |
| `cron`             | automation    | Cron işlerini zamanla ve yönet                               |
| `gateway`          | automation    | Ağ geçidi yönetimi                                           |
| `browser`          | ui            | Tarayıcı otomasyonu (gezin, tıkla, yaz, ekran görüntüsü al)  |
| `announce_queue`   | automation    | Asenkron sonuç bildirimi (asenkron devirler için)            |

## Belgelendirme

Tam belgelendirme **[docs.goclaw.sh](https://docs.goclaw.sh)** adresinde — veya kaynağa [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs) üzerinden göz atın.

| Bölüm | Konular |
|---------|--------|
| [Başlarken](https://docs.goclaw.sh/#what-is-goclaw) | Kurulum, Hızlı Başlangıç, Yapılandırma, Web Panosu Turu |
| [Temel Kavramlar](https://docs.goclaw.sh/#how-goclaw-works) | Ajan Döngüsü, Oturumlar, Araçlar, Bellek, Çok Kiracılılık |
| [Ajanlar](https://docs.goclaw.sh/#creating-agents) | Ajan Oluşturma, Bağlam Dosyaları, Kişilik, Paylaşım ve Erişim |
| [Sağlayıcılar](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 daha |
| [Kanallar](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Ajan Ekipleri](https://docs.goclaw.sh/#teams-what-are-teams) | Ekipler, Görev Panosu, Mesajlaşma, Devir ve Teslim |
| [Gelişmiş](https://docs.goclaw.sh/#custom-tools) | Özel Araçlar, MCP, Yetenekler, Cron, Kum Havuzu, Hook'lar, RBAC |
| [Dağıtım](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Veritabanı, Güvenlik, Gözlemlenebilirlik, Tailscale |
| [Referans](https://docs.goclaw.sh/#cli-commands) | CLI Komutları, REST API, WebSocket Protokolü, Ortam Değişkenleri |

## Test

```bash
go test ./...                                    # Birim testleri
go test -v ./tests/integration/ -timeout 120s    # Entegrasyon testleri (çalışan ağ geçidi gerektirir)
```

## Proje Durumu

Üretimde test edilenler ve hâlâ devam edenler dahil ayrıntılı özellik durumu için [CHANGELOG.md](CHANGELOG.md) dosyasına bakın.

## Teşekkürler

GoClaw, orijinal [OpenClaw](https://github.com/openclaw/openclaw) projesi üzerine inşa edilmiştir. Bu Go portuna ilham veren mimari ve vizyona minnettarız.

## Lisans

MIT
