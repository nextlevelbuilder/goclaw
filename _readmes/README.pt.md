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
  <a href="https://docs.goclaw.sh">Documentação</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Início Rápido</a> •
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

**GoClaw** é um gateway de IA multi-agente que conecta LLMs às suas ferramentas, canais e dados — implantado como um único binário Go sem dependências de tempo de execução. Ele orquestra equipes de agentes e delegação entre agentes em mais de 20 provedores de LLM com isolamento multi-tenant completo.

Um port em Go do [OpenClaw](https://github.com/openclaw/openclaw) com segurança aprimorada, PostgreSQL multi-tenant e observabilidade de nível de produção.

🌐 **Idiomas:**
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
## O Que o Torna Diferente

- **Equipes de Agentes e Orquestração** — Equipes com quadros de tarefas compartilhados, delegação entre agentes (síncrona/assíncrona) e descoberta híbrida de agentes
- **PostgreSQL Multi-Tenant** — Workspaces por usuário, arquivos de contexto por usuário, chaves de API criptografadas (AES-256-GCM), sessões isoladas
- **Binário Único** — Binário Go estático de ~25 MB, sem runtime Node.js, inicialização em <1s, roda em um VPS de $5
- **Segurança de Produção** — Sistema de permissões de 5 camadas (autenticação do gateway → política global de ferramentas → por agente → por canal → somente proprietário) mais limitação de taxa, detecção de injeção de prompt, proteção SSRF, padrões de negação de shell e criptografia AES-256-GCM
- **20+ Provedores de LLM** — Anthropic (HTTP+SSE nativo com cache de prompt), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP e qualquer endpoint compatível com OpenAI
- **7 Canais de Mensagens** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Modo de raciocínio por provedor (tokens de orçamento Anthropic, esforço de raciocínio OpenAI, orçamento de raciocínio DashScope) com suporte a streaming
- **Heartbeat** — Check-ins periódicos de agentes via listas de verificação HEARTBEAT.md com supressão quando OK, horários ativos, lógica de repetição e entrega por canal
- **Agendamento e Cron** — Expressões `at`, `every` e cron para tarefas automatizadas de agentes com concorrência baseada em lanes
- **Observabilidade** — Rastreamento integrado de chamadas LLM com spans e métricas de cache de prompt, exportação opcional OpenTelemetry OTLP

## Ecossistema Claw

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Linguagem        | TypeScript      | Rust     | Go       | **Go**                                  |
| Tamanho do binário     | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Imagem Docker    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (inativo)      | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Inicialização         | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Hardware alvo | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| Funcionalidade                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| Integração MCP            | — (usa ACP)                         | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Equipes de agentes                | —                                    | —                                            | —                                     | ✅ Quadro de tarefas + caixa de entrada        |
| Segurança reforçada         | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Básica (restrição de workspace, negação de exec) | ✅ Defesa em 5 camadas             |
| Observabilidade OTel         | ✅ (extensão opcional)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (build tag opcional)     |
| Cache de prompt             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Grafo de conhecimento            | —                                    | —                                            | —                                     | ✅ Extração LLM + travessia  |
| Sistema de skills               | ✅ Embeddings/semântico               | ✅ SKILL.md + TOML                           | ✅ Básico                              | ✅ BM25 + pgvector híbrido      |
| Agendador baseado em lanes       | ✅                                   | Concorrência limitada                          | —                                     | ✅ (main/subagent/team/cron)   |
| Canais de mensagens         | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Aplicativos complementares             | macOS, iOS, Android                  | Python SDK                                   | —                                     | Painel web                  |
| Live Canvas / Voz        | ✅ (A2UI + TTS/STT)                  | —                                            | Transcrição de voz                   | TTS (4 provedores)              |
| Provedores de LLM              | 10+                                  | 8 nativos + 29 compat                         | 13+                                   | **20+**                        |
| Workspaces por usuário        | ✅ (baseado em arquivos)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| Segredos criptografados          | — (somente variáveis de ambiente)                    | ✅ ChaCha20-Poly1305                         | — (JSON em texto simples)                    | ✅ AES-256-GCM no banco de dados           |

## Arquitetura

<p align="center">
  <img src="../_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

## Início Rápido

**Pré-requisitos:** Go 1.26+, PostgreSQL 18 com pgvector, Docker (opcional)

### A Partir do Código-Fonte

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Assistente de configuração interativo
source .env.local && ./goclaw
```

### Com Docker

```bash
# Gerar .env com segredos gerados automaticamente
chmod +x prepare-env.sh && ./prepare-env.sh

# Adicione pelo menos uma GOCLAW_*_API_KEY ao .env, depois:
make up

# Painel Web em http://localhost:18790
# Verificação de saúde: curl http://localhost:18790/health
```

Quando as variáveis de ambiente `GOCLAW_*_API_KEY` estão definidas, o gateway é configurado automaticamente sem prompts interativos — detecta o provedor, executa migrações e popula os dados padrão.

> Para variantes de build (OTel, Tailscale, Redis), tags de imagem Docker e sobreposições de compose, consulte o [Guia de Implantação](https://docs.goclaw.sh/#deploy-docker-compose).

## Orquestração Multi-Agente

GoClaw suporta equipes de agentes e delegação entre agentes — cada agente roda com sua própria identidade, ferramentas, provedor de LLM e arquivos de contexto.

### Delegação de Agentes

<p align="center">
  <img src="../_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Modo | Como funciona | Melhor para |
|------|-------------|----------|
| **Síncrono** | O Agente A pergunta ao Agente B e **aguarda** a resposta | Consultas rápidas, verificação de fatos |
| **Assíncrono** | O Agente A pergunta ao Agente B e **segue em frente**. B anuncia depois | Tarefas longas, relatórios, análises aprofundadas |

Os agentes se comunicam por meio de **links de permissão** explícitos com controle de direção (`outbound`, `inbound`, `bidirectional`) e limites de concorrência tanto no nível por link quanto por agente.

### Equipes de Agentes

<p align="center">
  <img src="../_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Quadro de tarefas compartilhado** — Criar, reivindicar, concluir e pesquisar tarefas com dependências `blocked_by`
- **Caixa de entrada da equipe** — Mensagens diretas entre pares e transmissões
- **Ferramentas**: `team_tasks` para gerenciamento de tarefas, `team_message` para caixa de entrada

> Para detalhes de delegação, links de permissão e controle de concorrência, consulte a [documentação de Equipes de Agentes](https://docs.goclaw.sh/#teams-what-are-teams).

## Ferramentas Integradas

| Ferramenta               | Grupo         | Descrição                                                  |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Ler conteúdo de arquivos (com roteamento de FS virtual)                 |
| `write_file`       | fs            | Escrever/criar arquivos                                           |
| `edit_file`        | fs            | Aplicar edições pontuais em arquivos existentes                       |
| `list_files`       | fs            | Listar conteúdo de diretórios                                      |
| `search`           | fs            | Pesquisar conteúdo de arquivos por padrão                              |
| `glob`             | fs            | Encontrar arquivos por padrão glob                                   |
| `exec`             | runtime       | Executar comandos shell (com fluxo de aprovação)              |
| `web_search`       | web           | Pesquisar na web (Brave, DuckDuckGo)                           |
| `web_fetch`        | web           | Buscar e analisar conteúdo da web                                  |
| `memory_search`    | memory        | Pesquisar memória de longo prazo (FTS + vector)                       |
| `memory_get`       | memory        | Recuperar entradas de memória                                      |
| `skill_search`     | —             | Pesquisar skills (BM25 + embedding híbrido)                      |
| `knowledge_graph_search` | memory  | Pesquisar entidades e percorrer relações do grafo de conhecimento   |
| `create_image`     | media         | Geração de imagens (DashScope, MiniMax)                        |
| `create_audio`     | media         | Geração de áudio (OpenAI, ElevenLabs, MiniMax, Suno)         |
| `create_video`     | media         | Geração de vídeo (MiniMax, Veo)                              |
| `read_document`    | media         | Leitura de documentos (Gemini File API, cadeia de provedores)           |
| `read_image`       | media         | Análise de imagens                                               |
| `read_audio`       | media         | Transcrição e análise de áudio                             |
| `read_video`       | media         | Análise de vídeo                                               |
| `message`          | messaging     | Enviar mensagens para canais                                    |
| `tts`              | —             | Síntese de texto para fala (Text-to-Speech)                                     |
| `spawn`            | —             | Iniciar um subagente                                             |
| `subagents`        | sessions      | Controlar subagentes em execução                                |
| `team_tasks`       | teams         | Quadro de tarefas compartilhado (listar, criar, reivindicar, concluir, pesquisar)    |
| `team_message`     | teams         | Caixa de entrada da equipe (enviar, transmitir, ler)                         |
| `sessions_list`    | sessions      | Listar sessões ativas                                         |
| `sessions_history` | sessions      | Ver histórico de sessões                                         |
| `sessions_send`    | sessions      | Enviar mensagem para uma sessão                                    |
| `sessions_spawn`   | sessions      | Iniciar uma nova sessão                                          |
| `session_status`   | sessions      | Verificar status da sessão                                         |
| `cron`             | automation    | Agendar e gerenciar jobs cron                                |
| `gateway`          | automation    | Administração do gateway                                       |
| `browser`          | ui            | Automação de navegador (navegar, clicar, digitar, capturar tela)       |
| `announce_queue`   | automation    | Anúncio assíncrono de resultados (para delegações assíncronas)            |

## Documentação

Documentação completa em **[docs.goclaw.sh](https://docs.goclaw.sh)** — ou navegue pelo código-fonte em [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Seção | Tópicos |
|---------|--------|
| [Primeiros Passos](https://docs.goclaw.sh/#what-is-goclaw) | Instalação, Início Rápido, Configuração, Tour do Painel Web |
| [Conceitos Fundamentais](https://docs.goclaw.sh/#how-goclaw-works) | Loop do Agente, Sessões, Ferramentas, Memória, Multi-Tenancy |
| [Agentes](https://docs.goclaw.sh/#creating-agents) | Criação de Agentes, Arquivos de Contexto, Personalidade, Compartilhamento e Acesso |
| [Provedores](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 mais |
| [Canais](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Equipes de Agentes](https://docs.goclaw.sh/#teams-what-are-teams) | Equipes, Quadro de Tarefas, Mensagens, Delegação e Transferência |
| [Avançado](https://docs.goclaw.sh/#custom-tools) | Ferramentas Personalizadas, MCP, Skills, Cron, Sandbox, Hooks, RBAC |
| [Implantação](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Banco de Dados, Segurança, Observabilidade, Tailscale |
| [Referência](https://docs.goclaw.sh/#cli-commands) | Comandos CLI, REST API, Protocolo WebSocket, Variáveis de Ambiente |

## Testes

```bash
go test ./...                                    # Testes unitários
go test -v ./tests/integration/ -timeout 120s    # Testes de integração (requer gateway em execução)
```

## Status do Projeto

Consulte [CHANGELOG.md](CHANGELOG.md) para o status detalhado das funcionalidades, incluindo o que foi testado em produção e o que ainda está em andamento.

## Agradecimentos

GoClaw foi construído sobre o projeto original [OpenClaw](https://github.com/openclaw/openclaw). Somos gratos pela arquitetura e visão que inspiraram este port em Go.

## Licença

MIT
