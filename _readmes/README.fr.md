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
  <a href="https://docs.goclaw.sh">Documentation</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Démarrage rapide</a> •
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

**GoClaw** est une passerelle IA multi-agents qui connecte les LLMs à vos outils, canaux et données — déployée comme un binaire Go unique sans dépendances d'exécution. Elle orchestre des équipes d'agents et la délégation inter-agents auprès de plus de 20 fournisseurs LLM avec une isolation multi-tenant complète.

Un portage Go de [OpenClaw](https://github.com/openclaw/openclaw) avec une sécurité renforcée, PostgreSQL multi-tenant, et une observabilité de niveau production.

🌐 **Langues :**
[🇺🇸 English](../README.md) ·
[🇨🇳 简体中文](README.zh-CN.md) ·
[🇯🇵 日本語](README.ja.md) ·
[🇰🇷 한국어](README.ko.md) ·
[🇻🇳 Tiếng Việt](README.vi.md) ·
[🇪🇸 Español](README.es.md) ·
[🇧🇷 Português](README.pt.md) ·
[🇫🇷 Français](README.fr.md) ·
[🇩🇪 Deutsch](README.de.md) ·
[🇷🇺 Русский](README.ru.md)
## Ce qui le différencie

- **Équipes d'agents et orchestration** — Équipes avec tableaux de tâches partagés, délégation inter-agents (sync/async), et découverte hybride d'agents
- **PostgreSQL multi-tenant** — Espaces de travail par utilisateur, fichiers de contexte par utilisateur, clés API chiffrées (AES-256-GCM), sessions isolées
- **Binaire unique** — Binaire Go statique de ~25 Mo, sans runtime Node.js, démarrage en <1 s, fonctionne sur un VPS à 5 $
- **Sécurité de niveau production** — Système de permissions à 5 couches (auth passerelle → politique d'outils globale → par agent → par canal → propriétaire uniquement) plus limitation de débit, détection d'injection de prompt, protection SSRF, motifs de refus shell, et chiffrement AES-256-GCM
- **Plus de 20 fournisseurs LLM** — Anthropic (HTTP+SSE natif avec mise en cache des prompts), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP, et tout endpoint compatible OpenAI
- **7 canaux de messagerie** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Mode de réflexion par fournisseur (jetons budget Anthropic, effort de raisonnement OpenAI, budget de réflexion DashScope) avec support du streaming
- **Heartbeat** — Vérifications périodiques des agents via des listes de contrôle HEARTBEAT.md avec suppression en cas de succès, heures actives, logique de réessai, et livraison par canal
- **Planification et Cron** — Expressions `at`, `every`, et cron pour les tâches d'agents automatisées avec concurrence par voie
- **Observabilité** — Traçage intégré des appels LLM avec spans et métriques de cache de prompts, export OpenTelemetry OTLP optionnel

## L'écosystème Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Langage        | TypeScript      | Rust     | Go       | **Go**                                  |
| Taille du binaire     | 28 Mo + Node.js | 3,4 Mo   | ~8 Mo    | **~25 Mo** (base) / **~36 Mo** (+ OTel) |
| Image Docker    | —               | —        | —        | **~50 Mo** (Alpine)                     |
| RAM (inactif)      | > 1 Go          | < 5 Mo   | < 10 Mo  | **~35 Mo**                              |
| Démarrage         | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Matériel cible | Mac Mini à 599 $+ | Edge à 10 $ | Edge à 10 $ | **VPS à 5 $+**                             |

| Fonctionnalité                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| Intégration MCP            | — (utilise ACP)                         | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Équipes d'agents                | —                                    | —                                            | —                                     | ✅ Tableau de tâches + boîte aux lettres        |
| Renforcement de la sécurité         | ✅ (SSRF, traversée de chemin, injection) | ✅ (sandbox, limite de débit, injection, appairage) | Basique (restriction d'espace de travail, refus exec) | ✅ Défense à 5 couches             |
| Observabilité OTel         | ✅ (extension opt-in)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (balise de build opt-in)     |
| Mise en cache des prompts             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Graphe de connaissances            | —                                    | —                                            | —                                     | ✅ Extraction LLM + traversal  |
| Système de compétences               | ✅ Embeddings/sémantique               | ✅ SKILL.md + TOML                           | ✅ Basique                              | ✅ BM25 + pgvector hybride      |
| Planificateur par voie       | ✅                                   | Concurrence bornée                          | —                                     | ✅ (main/subagent/team/cron)   |
| Canaux de messagerie         | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Applications compagnons             | macOS, iOS, Android                  | SDK Python                                   | —                                     | Tableau de bord web                  |
| Canvas en direct / Voix        | ✅ (A2UI + TTS/STT)                  | —                                            | Transcription vocale                   | TTS (4 fournisseurs)              |
| Fournisseurs LLM              | 10+                                  | 8 natifs + 29 compat                         | 13+                                   | **20+**                        |
| Espaces de travail par utilisateur        | ✅ (basé sur fichiers)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| Secrets chiffrés          | — (variables d'env uniquement)                    | ✅ ChaCha20-Poly1305                         | — (JSON en clair)                    | ✅ AES-256-GCM en base de données           |

## Architecture

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Démarrage rapide

**Prérequis :** Go 1.26+, PostgreSQL 18 avec pgvector, Docker (optionnel)

### Depuis les sources

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Assistant de configuration interactif
source .env.local && ./goclaw
```

### Avec Docker

```bash
# Générer .env avec des secrets auto-générés
chmod +x prepare-env.sh && ./prepare-env.sh

# Ajouter au moins une GOCLAW_*_API_KEY dans .env, puis :
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Tableau de bord web sur http://localhost:3000
# Vérification de santé : curl http://localhost:18790/health
```

Lorsque les variables d'environnement `GOCLAW_*_API_KEY` sont définies, la passerelle s'auto-configure sans invites interactives — détecte le fournisseur, exécute les migrations, et initialise les données par défaut.

> Pour les variantes de build (OTel, Tailscale, Redis), les tags d'images Docker, et les overlays compose, voir le [Guide de déploiement](https://docs.goclaw.sh/#deploy-docker-compose).

## Orchestration multi-agents

GoClaw prend en charge les équipes d'agents et la délégation inter-agents — chaque agent s'exécute avec sa propre identité, ses outils, son fournisseur LLM, et ses fichiers de contexte.

### Délégation d'agents

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Mode | Fonctionnement | Idéal pour |
|------|-------------|----------|
| **Sync** | L'agent A demande à l'agent B et **attend** la réponse | Recherches rapides, vérifications de faits |
| **Async** | L'agent A demande à l'agent B et **continue**. B annonce plus tard | Tâches longues, rapports, analyses approfondies |

Les agents communiquent via des **liens de permission** explicites avec contrôle de direction (`outbound`, `inbound`, `bidirectional`) et limites de concurrence au niveau de chaque lien et de chaque agent.

### Équipes d'agents

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Tableau de tâches partagé** — Créer, revendiquer, compléter, rechercher des tâches avec des dépendances `blocked_by`
- **Boîte aux lettres d'équipe** — Messagerie directe entre pairs et diffusions
- **Outils** : `team_tasks` pour la gestion des tâches, `team_message` pour la boîte aux lettres

> Pour les détails sur la délégation, les liens de permission, et le contrôle de concurrence, voir la [documentation des équipes d'agents](https://docs.goclaw.sh/#teams-what-are-teams).

## Outils intégrés

| Outil               | Groupe         | Description                                                  |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Lire le contenu des fichiers (avec routage FS virtuel)                 |
| `write_file`       | fs            | Écrire/créer des fichiers                                           |
| `edit_file`        | fs            | Appliquer des modifications ciblées aux fichiers existants                       |
| `list_files`       | fs            | Lister le contenu d'un répertoire                                      |
| `search`           | fs            | Rechercher le contenu des fichiers par motif                              |
| `glob`             | fs            | Trouver des fichiers par motif glob                                   |
| `exec`             | runtime       | Exécuter des commandes shell (avec flux d'approbation)              |
| `web_search`       | web           | Rechercher sur le web (Brave, DuckDuckGo)                           |
| `web_fetch`        | web           | Récupérer et analyser le contenu web                                  |
| `memory_search`    | memory        | Rechercher dans la mémoire à long terme (FTS + vecteur)                       |
| `memory_get`       | memory        | Récupérer des entrées de mémoire                                      |
| `skill_search`     | —             | Rechercher des compétences (BM25 + hybride d'embeddings)                      |
| `knowledge_graph_search` | memory  | Rechercher des entités et traverser les relations du graphe de connaissances   |
| `create_image`     | media         | Génération d'images (DashScope, MiniMax)                        |
| `create_audio`     | media         | Génération audio (OpenAI, ElevenLabs, MiniMax, Suno)         |
| `create_video`     | media         | Génération vidéo (MiniMax, Veo)                              |
| `read_document`    | media         | Lecture de documents (API Fichiers Gemini, chaîne de fournisseurs)           |
| `read_image`       | media         | Analyse d'images                                               |
| `read_audio`       | media         | Transcription et analyse audio                             |
| `read_video`       | media         | Analyse vidéo                                               |
| `message`          | messaging     | Envoyer des messages aux canaux                                    |
| `tts`              | —             | Synthèse texte-parole                                             |
| `spawn`            | —             | Lancer un sous-agent                                             |
| `subagents`        | sessions      | Contrôler les sous-agents en cours d'exécution                                    |
| `team_tasks`       | teams         | Tableau de tâches partagé (lister, créer, revendiquer, compléter, rechercher)    |
| `team_message`     | teams         | Boîte aux lettres d'équipe (envoyer, diffuser, lire)                         |
| `sessions_list`    | sessions      | Lister les sessions actives                                         |
| `sessions_history` | sessions      | Afficher l'historique des sessions                                         |
| `sessions_send`    | sessions      | Envoyer un message à une session                                    |
| `sessions_spawn`   | sessions      | Lancer une nouvelle session                                          |
| `session_status`   | sessions      | Vérifier le statut d'une session                                         |
| `cron`             | automation    | Planifier et gérer les tâches cron                                |
| `gateway`          | automation    | Administration de la passerelle                                           |
| `browser`          | ui            | Automatisation du navigateur (naviguer, cliquer, saisir, capture d'écran)       |
| `announce_queue`   | automation    | Annonce de résultats asynchrones (pour les délégations asynchrones)            |

## Documentation

Documentation complète sur **[docs.goclaw.sh](https://docs.goclaw.sh)** — ou consultez les sources dans [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Section | Sujets |
|---------|--------|
| [Premiers pas](https://docs.goclaw.sh/#what-is-goclaw) | Installation, Démarrage rapide, Configuration, Visite du tableau de bord web |
| [Concepts fondamentaux](https://docs.goclaw.sh/#how-goclaw-works) | Boucle d'agent, Sessions, Outils, Mémoire, Multi-tenant |
| [Agents](https://docs.goclaw.sh/#creating-agents) | Créer des agents, Fichiers de contexte, Personnalité, Partage et accès |
| [Fournisseurs](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 autres |
| [Canaux](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Équipes d'agents](https://docs.goclaw.sh/#teams-what-are-teams) | Équipes, Tableau de tâches, Messagerie, Délégation et transfert |
| [Avancé](https://docs.goclaw.sh/#custom-tools) | Outils personnalisés, MCP, Compétences, Cron, Sandbox, Hooks, RBAC |
| [Déploiement](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Base de données, Sécurité, Observabilité, Tailscale |
| [Référence](https://docs.goclaw.sh/#cli-commands) | Commandes CLI, API REST, Protocole WebSocket, Variables d'environnement |

## Tests

```bash
go test ./...                                    # Tests unitaires
go test -v ./tests/integration/ -timeout 120s    # Tests d'intégration (nécessite une passerelle en cours d'exécution)
```

## Statut du projet

Voir [CHANGELOG.md](CHANGELOG.md) pour le statut détaillé des fonctionnalités, y compris ce qui a été testé en production et ce qui est encore en cours.

## Remerciements

GoClaw est construit sur le projet original [OpenClaw](https://github.com/openclaw/openclaw). Nous sommes reconnaissants pour l'architecture et la vision qui ont inspiré ce portage Go.

## Licence

MIT
