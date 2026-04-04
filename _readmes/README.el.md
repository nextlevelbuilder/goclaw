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
  <a href="https://docs.goclaw.sh">Τεκμηρίωση</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Γρήγορη Εκκίνηση</a> •
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

Το **GoClaw** είναι μια πύλη AI πολλαπλών πρακτόρων που συνδέει τα LLM με τα εργαλεία, τα κανάλια και τα δεδομένα σας — αναπτύσσεται ως ένα μονό δυαδικό αρχείο Go χωρίς εξαρτήσεις χρόνου εκτέλεσης. Ενορχηστρώνει ομάδες πρακτόρων και ανάθεση μεταξύ πρακτόρων σε 20+ παρόχους LLM με πλήρη απομόνωση πολλαπλών μισθωτών.

Μια μεταφορά σε Go του [OpenClaw](https://github.com/openclaw/openclaw) με ενισχυμένη ασφάλεια, PostgreSQL πολλαπλών μισθωτών και παρατηρησιμότητα παραγωγικού επιπέδου.

🌐 **Γλώσσες:**
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

## Τι το Κάνει Διαφορετικό

- **Ομάδες Πρακτόρων & Ενορχήστρωση** — Ομάδες με κοινόχρηστους πίνακες εργασιών, ανάθεση μεταξύ πρακτόρων (σύγχρονη/ασύγχρονη), και υβριδική ανακάλυψη πρακτόρων
- **PostgreSQL Πολλαπλών Μισθωτών** — Χώροι εργασίας ανά χρήστη, αρχεία περιβάλλοντος ανά χρήστη, κρυπτογραφημένα κλειδιά API (AES-256-GCM), απομονωμένες συνεδρίες
- **Μονό Δυαδικό Αρχείο** — ~25 MB στατικό δυαδικό αρχείο Go, χωρίς χρόνο εκτέλεσης Node.js, εκκίνηση <1s, τρέχει σε VPS $5
- **Ασφάλεια Παραγωγικού Επιπέδου** — Σύστημα αδειών 5 επιπέδων (πιστοποίηση πύλης → παγκόσμια πολιτική εργαλείων → ανά πράκτορα → ανά κανάλι → μόνο ιδιοκτήτης) συν περιορισμό ρυθμού, ανίχνευση έγχυσης εντολών, προστασία SSRF, μοτίβα απόρριψης κελύφους, και κρυπτογράφηση AES-256-GCM
- **20+ Πάροχοι LLM** — Anthropic (εγγενές HTTP+SSE με προσωρινή αποθήκευση εντολών), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP, και οποιοδήποτε συμβατό σημείο τελικό OpenAI
- **7 Κανάλια Ανταλλαγής Μηνυμάτων** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Λειτουργία σκέψης ανά πάροχο (Anthropic budget tokens, OpenAI reasoning effort, DashScope thinking budget) με υποστήριξη ροής
- **Heartbeat** — Περιοδικές ενημερώσεις πρακτόρων μέσω λιστών ελέγχου HEARTBEAT.md με αναστολή-σε-OK, ενεργές ώρες, λογική επανάληψης, και παράδοση στο κανάλι
- **Χρονοδρομολόγηση & Cron** — Εκφράσεις `at`, `every`, και cron για αυτοματοποιημένες εργασίες πρακτόρων με ταυτόχρονη εκτέλεση βάσει λωρίδων
- **Παρατηρησιμότητα** — Ενσωματωμένη ανίχνευση κλήσεων LLM με χρονικά διαστήματα και μετρικές κρυφής μνήμης εντολών, προαιρετική εξαγωγή OpenTelemetry OTLP

## Οικοσύστημα Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Γλώσσα          | TypeScript      | Rust     | Go       | **Go**                                  |
| Μέγεθος δυαδικού | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (βάση) / **~36 MB** (+ OTel) |
| Docker image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (αδρανές)   | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Εκκίνηση        | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Στοχευόμενο υλικό | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| Χαρακτηριστικό             | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Πολλαπλοί μισθωτές (PostgreSQL) | —                               | —                                            | —                                     | ✅                             |
| Ενσωμάτωση MCP             | — (χρησιμοποιεί ACP)                 | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Ομάδες πρακτόρων           | —                                    | —                                            | —                                     | ✅ Task board + mailbox        |
| Ενίσχυση ασφάλειας         | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Βασική (workspace restrict, exec deny) | ✅ Άμυνα 5 επιπέδων           |
| Παρατηρησιμότητα OTel      | ✅ (προαιρετική επέκταση)            | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (προαιρετική ετικέτα κατασκευής) |
| Κρυφή μνήμη εντολών        | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Γράφος γνώσης              | —                                    | —                                            | —                                     | ✅ Εξαγωγή LLM + διάσχιση     |
| Σύστημα δεξιοτήτων         | ✅ Embeddings/σημασιολογικό          | ✅ SKILL.md + TOML                           | ✅ Βασικό                             | ✅ BM25 + pgvector υβριδικό   |
| Χρονοδρομολογητής βάσει λωρίδων | ✅                              | Περιορισμένη ταυτόχρονη εκτέλεση             | —                                     | ✅ (main/subagent/team/cron)   |
| Κανάλια ανταλλαγής μηνυμάτων | 37+                               | 15+                                          | 10+                                   | 7+                             |
| Συνοδευτικές εφαρμογές     | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web dashboard                  |
| Live Canvas / Φωνή         | ✅ (A2UI + TTS/STT)                  | —                                            | Μεταγραφή φωνής                       | TTS (4 πάροχοι)                |
| Πάροχοι LLM                | 10+                                  | 8 εγγενείς + 29 συμβατοί                     | 13+                                   | **20+**                        |
| Χώροι εργασίας ανά χρήστη  | ✅ (βάσει αρχείων)                   | —                                            | —                                     | ✅ (PostgreSQL)                |
| Κρυπτογραφημένα μυστικά    | — (μόνο μεταβλητές περιβάλλοντος)   | ✅ ChaCha20-Poly1305                         | — (απλό κείμενο JSON)                 | ✅ AES-256-GCM σε DB           |

## Αρχιτεκτονική

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Γρήγορη Εκκίνηση

**Προαπαιτούμενα:** Go 1.26+, PostgreSQL 18 με pgvector, Docker (προαιρετικά)

### Από Πηγαίο Κώδικα

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interactive setup wizard
source .env.local && ./goclaw
```

### Με Docker

```bash
# Generate .env with auto-generated secrets
chmod +x prepare-env.sh && ./prepare-env.sh

# Add at least one GOCLAW_*_API_KEY to .env, then:
make up

# Web Dashboard at http://localhost:18790
# Health check: curl http://localhost:18790/health
```

Όταν ορίζονται μεταβλητές περιβάλλοντος `GOCLAW_*_API_KEY`, η πύλη ενσωματώνεται αυτόματα χωρίς διαδραστικές ερωτήσεις — ανιχνεύει τον πάροχο, εκτελεί μεταναστεύσεις, και εισάγει προεπιλεγμένα δεδομένα.

> Για παραλλαγές κατασκευής (OTel, Tailscale, Redis), ετικέτες Docker image, και επικαλύψεις compose, δείτε τον [Οδηγό Ανάπτυξης](https://docs.goclaw.sh/#deploy-docker-compose).

## Ενορχήστρωση Πολλαπλών Πρακτόρων

Το GoClaw υποστηρίζει ομάδες πρακτόρων και ανάθεση μεταξύ πρακτόρων — κάθε πράκτορας εκτελείται με τη δική του ταυτότητα, εργαλεία, πάροχο LLM, και αρχεία περιβάλλοντος.

### Ανάθεση Πράκτορα

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Λειτουργία | Πώς λειτουργεί | Καλύτερο για |
|------------|---------------|--------------|
| **Σύγχρονη** | Ο Πράκτορας Α ρωτά τον Πράκτορα Β και **αναμένει** την απάντηση | Γρήγορες αναζητήσεις, επαλήθευση γεγονότων |
| **Ασύγχρονη** | Ο Πράκτορας Α ρωτά τον Πράκτορα Β και **συνεχίζει**. Ο Β ανακοινώνει αργότερα | Μακρές εργασίες, αναφορές, βαθιά ανάλυση |

Οι πράκτορες επικοινωνούν μέσω ρητών **συνδέσμων αδειών** με έλεγχο κατεύθυνσης (`outbound`, `inbound`, `bidirectional`) και ορίων ταυτόχρονης εκτέλεσης τόσο σε επίπεδο ανά σύνδεσμο όσο και ανά πράκτορα.

### Ομάδες Πρακτόρων

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Κοινόχρηστος πίνακας εργασιών** — Δημιουργία, ανάληψη, ολοκλήρωση, αναζήτηση εργασιών με εξαρτήσεις `blocked_by`
- **Ταχυδρομείο ομάδας** — Άμεση ανταλλαγή μηνυμάτων μεταξύ ομότιμων και εκπομπές
- **Εργαλεία**: `team_tasks` για διαχείριση εργασιών, `team_message` για ταχυδρομείο

> Για λεπτομέρειες ανάθεσης, συνδέσμους αδειών, και έλεγχο ταυτόχρονης εκτέλεσης, δείτε την [τεκμηρίωση Ομάδων Πρακτόρων](https://docs.goclaw.sh/#teams-what-are-teams).

## Ενσωματωμένα Εργαλεία

| Εργαλείο           | Ομάδα         | Περιγραφή                                                    |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Ανάγνωση περιεχομένου αρχείου (με δρομολόγηση εικονικού FS) |
| `write_file`       | fs            | Εγγραφή/δημιουργία αρχείων                                  |
| `edit_file`        | fs            | Εφαρμογή στοχευμένων επεξεργασιών σε υπάρχοντα αρχεία       |
| `list_files`       | fs            | Λίστα περιεχομένων καταλόγου                                 |
| `search`           | fs            | Αναζήτηση περιεχομένου αρχείων με μοτίβο                    |
| `glob`             | fs            | Εύρεση αρχείων με μοτίβο glob                               |
| `exec`             | runtime       | Εκτέλεση εντολών κελύφους (με ροή εγκρίσεων)                |
| `web_search`       | web           | Αναζήτηση στον ιστό (Brave, DuckDuckGo)                     |
| `web_fetch`        | web           | Ανάκτηση και ανάλυση περιεχομένου ιστού                     |
| `memory_search`    | memory        | Αναζήτηση μακροπρόθεσμης μνήμης (FTS + vector)              |
| `memory_get`       | memory        | Ανάκτηση καταχωρήσεων μνήμης                                |
| `skill_search`     | —             | Αναζήτηση δεξιοτήτων (υβριδικό BM25 + embedding)            |
| `knowledge_graph_search` | memory  | Αναζήτηση οντοτήτων και διάσχιση σχέσεων γράφου γνώσης      |
| `create_image`     | media         | Δημιουργία εικόνας (DashScope, MiniMax)                      |
| `create_audio`     | media         | Δημιουργία ήχου (OpenAI, ElevenLabs, MiniMax, Suno)          |
| `create_video`     | media         | Δημιουργία βίντεο (MiniMax, Veo)                             |
| `read_document`    | media         | Ανάγνωση εγγράφου (Gemini File API, αλυσίδα παρόχων)        |
| `read_image`       | media         | Ανάλυση εικόνας                                              |
| `read_audio`       | media         | Μεταγραφή και ανάλυση ήχου                                   |
| `read_video`       | media         | Ανάλυση βίντεο                                               |
| `message`          | messaging     | Αποστολή μηνυμάτων σε κανάλια                                |
| `tts`              | —             | Σύνθεση κειμένου-σε-ομιλία                                   |
| `spawn`            | —             | Δημιουργία υποπράκτορα                                       |
| `subagents`        | sessions      | Έλεγχος τρεχόντων υποπρακτόρων                              |
| `team_tasks`       | teams         | Κοινόχρηστος πίνακας εργασιών (λίστα, δημιουργία, ανάληψη, ολοκλήρωση, αναζήτηση) |
| `team_message`     | teams         | Ταχυδρομείο ομάδας (αποστολή, εκπομπή, ανάγνωση)            |
| `sessions_list`    | sessions      | Λίστα ενεργών συνεδριών                                      |
| `sessions_history` | sessions      | Προβολή ιστορικού συνεδριών                                  |
| `sessions_send`    | sessions      | Αποστολή μηνύματος σε συνεδρία                               |
| `sessions_spawn`   | sessions      | Δημιουργία νέας συνεδρίας                                    |
| `session_status`   | sessions      | Έλεγχος κατάστασης συνεδρίας                                 |
| `cron`             | automation    | Χρονοδρομολόγηση και διαχείριση εργασιών cron                |
| `gateway`          | automation    | Διαχείριση πύλης                                             |
| `browser`          | ui            | Αυτοματισμός προγράμματος περιήγησης (πλοήγηση, κλικ, πληκτρολόγηση, στιγμιότυπο) |
| `announce_queue`   | automation    | Ασύγχρονη ανακοίνωση αποτελεσμάτων (για ασύγχρονες αναθέσεις) |

## Τεκμηρίωση

Πλήρης τεκμηρίωση στο **[docs.goclaw.sh](https://docs.goclaw.sh)** — ή περιηγηθείτε στην πηγή στο [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Ενότητα | Θέματα |
|---------|--------|
| [Ξεκινώντας](https://docs.goclaw.sh/#what-is-goclaw) | Εγκατάσταση, Γρήγορη Εκκίνηση, Διαμόρφωση, Περιήγηση Web Dashboard |
| [Βασικές Έννοιες](https://docs.goclaw.sh/#how-goclaw-works) | Βρόχος Πράκτορα, Συνεδρίες, Εργαλεία, Μνήμη, Πολλαπλή Ενοικίαση |
| [Πράκτορες](https://docs.goclaw.sh/#creating-agents) | Δημιουργία Πρακτόρων, Αρχεία Περιβάλλοντος, Προσωπικότητα, Κοινοποίηση & Πρόσβαση |
| [Πάροχοι](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 ακόμα |
| [Κανάλια](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Ομάδες Πρακτόρων](https://docs.goclaw.sh/#teams-what-are-teams) | Ομάδες, Πίνακας Εργασιών, Ανταλλαγή Μηνυμάτων, Ανάθεση & Παράδοση |
| [Προχωρημένα](https://docs.goclaw.sh/#custom-tools) | Προσαρμοσμένα Εργαλεία, MCP, Δεξιότητες, Cron, Sandbox, Hooks, RBAC |
| [Ανάπτυξη](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Βάση Δεδομένων, Ασφάλεια, Παρατηρησιμότητα, Tailscale |
| [Αναφορά](https://docs.goclaw.sh/#cli-commands) | Εντολές CLI, REST API, WebSocket Protocol, Μεταβλητές Περιβάλλοντος |

## Δοκιμές

```bash
go test ./...                                    # Unit tests
go test -v ./tests/integration/ -timeout 120s    # Integration tests (requires running gateway)
```

## Κατάσταση Έργου

Δείτε το [CHANGELOG.md](CHANGELOG.md) για λεπτομερή κατάσταση χαρακτηριστικών, συμπεριλαμβανομένου του τι έχει δοκιμαστεί σε παραγωγή και τι βρίσκεται ακόμα σε εξέλιξη.

## Αναγνωρίσεις

Το GoClaw κατασκευάστηκε πάνω στο αρχικό έργο [OpenClaw](https://github.com/openclaw/openclaw). Είμαστε ευγνώμονες για την αρχιτεκτονική και το όραμα που ενέπνευσε αυτή τη μεταφορά σε Go.

## Άδεια Χρήσης

MIT
